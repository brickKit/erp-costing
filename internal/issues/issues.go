// Package issues is the cost of what leaves the stock (docs/design.md IS01, IS02, RT01): an issue, a purchase return.
package issues

import (
	"context"
	"fmt"

	core "github.com/brickKit/erp-costing/v3/internal/core"
	facts "github.com/brickKit/erp-costing/v3/internal/facts"
	layers "github.com/brickKit/erp-costing/v3/internal/layers"
	methods "github.com/brickKit/erp-costing/v3/internal/methods"

	"github.com/brickKit/be-sdk-go/v3/db"
)

// Issue values an issue (IS01) and publishes its cost of sales fact (CF01).
func Issue(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, mv core.Movement) (string, error) {
	if s.Method != core.MethodMovingAverage {
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
		m.RT.Logger.Warn("an issue past the valued stock: the rest at the policy price, the stock below zero",
			"event", "negative_stock", "legal_entity_id", s.LegalEntityID, "product_id", s.ProductID, "past", past)
	}
	s.Quantity = core.Minus(s.Quantity, qty)
	s.Value = core.Minus(s.Value, cost)
	return cost, nil
}
