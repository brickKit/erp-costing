// Package issues is the cost of what leaves the stock (docs/design.md IS01, IS02, RT01): an issue, a purchase return.
package issues

import (
	"context"
	"fmt"

	core "github.com/brickKit/erp-costing/v3/internal/core"
	facts "github.com/brickKit/erp-costing/v3/internal/facts"
	layers "github.com/brickKit/erp-costing/v3/internal/layers"
	policies "github.com/brickKit/erp-costing/v3/internal/policies"

	"github.com/brickKit/be-sdk-go/v3/db"
)

// Issue values an issue (IS01) and publishes its cost of sales fact (CF01).
func Issue(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, mv core.Movement) (string, error) {
	if s.Method != policies.MovingAverage {
		return "", fmt.Errorf("issues: %s is not valued by this version", s.Method)
	}
	cost, err := movingAverage(m, ctx, tx, s, mv.Quantity)
	if err != nil {
		return "", err
	}
	return cost, facts.Publish(m, ctx, tx, facts.Cost{FactID: core.FactOf(mv.FactID, core.FactValued),
		LegalEntityID: s.LegalEntityID, MovementID: mv.FactID, Date: mv.BusinessDate, Rule: facts.RuleCostOfSales,
		Currency: s.Currency, Amounts: map[string]string{facts.RoleCostOfSales: cost, facts.RoleInventory: cost}})
}

// Return values a purchase return (RT01).
func Return(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, mv core.Movement) (string, error) {
	return "", fmt.Errorf("issues: a purchase return is not valued by this version")
}

// movingAverage takes qty out of the scope at its average (IS01): qty ÷ quantity × value, rounded; all of the stock
// takes all of the value, leaving no rounding behind. Past the stock (IS02) the rest is priced by the policy and the
// quantity goes below zero, warned.
func movingAverage(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, qty string) (string, error) {
	var cost string
	switch onHand := s.Quantity; {
	case core.Cmp(qty, onHand) < 0:
		cost = core.Amount(core.Quo(core.Mul(qty, s.Value), onHand), s.Scale)
	case core.Cmp(qty, onHand) == 0:
		cost = s.Value
	default:
		covered, past := "0", qty
		if core.Cmp(onHand, "0") > 0 {
			covered, past = s.Value, core.Minus(qty, onHand)
		}
		price, err := layers.PolicyPrice(m, ctx, tx, s)
		if err != nil {
			return "", err
		}
		cost = core.Sum(covered, core.Amount(core.Mul(past, price), s.Scale))
		m.RT.Logger.Warn("an issue past the valued stock: the rest at the policy price, the stock below zero",
			"event", "negative_stock", "legal_entity_id", s.LegalEntityID, "product_id", s.ProductID, "past", past)
	}
	s.Quantity = core.Minus(s.Quantity, qty)
	s.Value = core.Minus(s.Value, cost)
	return core.Amount(cost, s.Scale), nil
}
