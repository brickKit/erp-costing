// Package issues is the cost of what leaves the stock (docs/design.md IS01, IS02, RT01): an issue, a purchase return.
package issues

import (
	"context"

	core "github.com/brickKit/erp-costing/v3/internal/core"
	facts "github.com/brickKit/erp-costing/v3/internal/facts"
	layers "github.com/brickKit/erp-costing/v3/internal/layers"
	methods "github.com/brickKit/erp-costing/v3/internal/methods"
	policies "github.com/brickKit/erp-costing/v3/internal/policies"

	"github.com/brickKit/be-sdk-go/v3/db"
	costingv3 "github.com/brickKit/erp-costing/contract/v3/gen/go/be/erp/costing/v3"
)

// Issue values an issue (IS01) and publishes its cost of sales fact (CF01).
func Issue(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, mv core.Movement) (string, error) {
	var cost string
	var err error
	if s.Method == core.MethodFIFO {
		cost, err = fifo(m, ctx, tx, s, mv)
	} else {
		cost, err = movingAverage(m, ctx, tx, s, mv.Quantity)
	}
	if err != nil {
		return "", err
	}
	return cost, facts.Publish(m, ctx, tx, facts.Cost{FactID: core.FactOf(mv.FactID, core.FactValued),
		LegalEntityID: s.LegalEntityID, MovementID: mv.FactID, Date: mv.BusinessDate, Rule: facts.RuleCostOfSales,
		Currency: s.Currency, Amounts: map[string]string{facts.RoleCostOfSales: cost, facts.RoleInventory: cost}})
}

// fifo takes the issue's units from the oldest layers at their own prices (IS01); the rest past them at the policy
// price (IS02).
func fifo(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, mv core.Movement) (string, error) {
	value, taken, err := layers.TakeOldest(ctx, tx, s, mv.FactID, mv.Quantity)
	if err != nil {
		return "", err
	}
	return leave(m, ctx, tx, s, mv.Quantity, value, taken)
}

// leave takes qty out of the scope at value for the taken units (FIFO: from the layers), the rest at the policy price
// (IS02, warned); all of the stock takes all of its value, leaving no rounding behind.
func leave(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, qty, value, taken string) (string, error) {
	past := core.Minus(qty, taken)
	if core.Cmp(past, "0") > 0 {
		price, err := layers.PolicyPrice(m, ctx, tx, s)
		if err != nil {
			return "", err
		}
		value = core.Sum(value, core.Mul(past, price))
		warnPast(m, s, past)
	}
	cost := core.Amount(value, s.Scale)
	if core.Cmp(qty, s.Quantity) == 0 {
		cost = core.Amount(s.Value, s.Scale)
	}
	s.Quantity = core.Minus(s.Quantity, qty)
	s.Value = core.Minus(s.Value, cost)
	return cost, nil
}

func warnPast(m *core.Base, s *core.Scope, past string) {
	m.RT.Logger.Warn("units past the valued stock left: at the policy price, the stock below zero", "event", "negative_stock",
		"legal_entity_id", s.LegalEntityID, "product_id", s.ProductID, "past", past)
}

// Return values a purchase return (RT01) at its receipt layer's current prices: FIFO the layer's next units, moving
// average the layer's unit cost (the units counted gone); units past the layer, or a receipt not valued in this scope,
// at the policy price (IS02, warned). Publishes the inventory return fact (CF01).
func Return(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, mv core.Movement) (string, error) {
	var o layers.Origin
	var err error
	if mv.OriginMovementID != nil && core.UUIDForm.MatchString(*mv.OriginMovementID) {
		if o, err = layers.OriginOf(ctx, tx, *mv.OriginMovementID); err != nil {
			return "", err
		}
	}
	value, taken := "0", "0"
	switch {
	case !o.Found || o.ScopeID != s.ID:
		m.RT.Logger.Warn("a purchase return without its receipt's layer in the scope: at the policy price", "event",
			"return_origin_unknown", "movement_id", mv.FactID, "legal_entity_id", s.LegalEntityID, "product_id", s.ProductID)
	case s.Method == core.MethodFIFO:
		value, taken, err = layers.TakeFromLayer(ctx, tx, *mv.OriginMovementID, mv.FactID, mv.Quantity)
	default:
		value, taken = core.Mul(mv.Quantity, o.UnitCost), mv.Quantity
		err = layers.MarkGone(ctx, tx, *mv.OriginMovementID, mv.Quantity)
	}
	if err != nil {
		return "", err
	}
	cost, err := leave(m, ctx, tx, s, mv.Quantity, value, taken)
	if err != nil {
		return "", err
	}
	return cost, facts.Publish(m, ctx, tx, facts.Cost{FactID: core.FactOf(mv.FactID, core.FactValued),
		LegalEntityID: s.LegalEntityID, MovementID: mv.FactID, Date: mv.BusinessDate, Rule: facts.RuleReturn,
		Currency: s.Currency, Amounts: map[string]string{facts.RoleGRNI: cost, facts.RoleInventory: cost}})
}

// movingAverage takes qty out of the scope at its average (IS01); past the stock (IS02) the rest at the policy price
// and the quantity below zero, warned.
func movingAverage(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, qty string) (string, error) {
	price := "0"
	if core.Cmp(qty, s.Quantity) > 0 {
		var err error
		if price, err = layers.PolicyPrice(m, ctx, tx, s); err != nil {
			return "", err
		}
	}
	cost, past := methods.AverageTake(qty, s.Quantity, s.Value, price, s.Scale)
	if core.Cmp(past, "0") > 0 {
		warnPast(m, s, past)
	}
	s.Quantity = core.Minus(s.Quantity, qty)
	s.Value = core.Minus(s.Value, cost)
	return cost, nil
}

// ReturnLimit answers erp/purchase how much of a receipt may be returned (RT01, IC30): FIFO the layer's units still in
// stock, moving average no limit; a receipt not valued yet is not known, and the caller does not limit.
func ReturnLimit(m *core.Base, ctx context.Context, in *costingv3.ReturnLimitRequest) (*costingv3.ReturnLimitResponse, error) {
	out := &costingv3.ReturnLimitResponse{}
	if !core.UUIDForm.MatchString(in.GetOriginMovementId()) {
		return out, nil
	}
	err := m.RT.DB.Tx(ctx, func(ctx context.Context, tx db.Tx) error {
		o, err := layers.OriginOf(ctx, tx, in.GetOriginMovementId())
		if err != nil || !o.Found {
			return err
		}
		method, err := policies.MethodRead(ctx, tx, o.LegalEntityID)
		if err != nil {
			return err
		}
		out.Known, out.Limited = true, method == core.MethodFIFO
		if out.Limited {
			out.Quantity = core.Plain(o.InStock)
		}
		return nil
	})
	return out, err
}

// Level levels a stock that was below zero with a receipt (IS02): the receipt's first units are the ones issued past
// the stock; their cost by the receipt's prices replaces what the policy price took, the difference correcting the
// cost of sales (cost_of_sales when the receipt cost more, cost_of_sales_return when less). before and beforeValue
// are the scope's quantity and value before the receipt; s is after it.
func Level(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, mv core.Movement, before, beforeValue string) error {
	if core.Cmp(before, "0") >= 0 {
		return nil
	}
	n := core.Minus("0", before)
	if core.Cmp(mv.Quantity, n) < 0 {
		n = mv.Quantity
	}
	took := core.Amount(core.Quo(core.Mul(n, beforeValue), before), s.Scale)
	value, _, err := layers.TakeFromLayer(ctx, tx, mv.FactID, mv.FactID, n)
	if err != nil {
		return err
	}
	diff := core.Amount(core.Minus(core.Amount(value, s.Scale), took), s.Scale)
	s.Value = core.Minus(s.Value, diff)
	rule, amounts := facts.RuleCostOfSales, map[string]string{facts.RoleCostOfSales: diff, facts.RoleInventory: diff}
	if core.Cmp(diff, "0") < 0 {
		neg := core.Amount(core.Minus("0", diff), s.Scale)
		rule, amounts = facts.RuleCostReturn, map[string]string{facts.RoleInventory: neg, facts.RoleCostOfSales: neg}
	}
	return facts.Publish(m, ctx, tx, facts.Cost{FactID: core.FactOf(mv.FactID, core.FactLevelled),
		LegalEntityID: s.LegalEntityID, MovementID: mv.FactID, Date: mv.BusinessDate, Rule: rule, Currency: s.Currency,
		Amounts: amounts})
}
