// Package layers is the cost layers and their price sources (docs/design.md LY01-LY05): a receipt's layer, the
// valuation bases of erp/purchase, the policy price.
package layers

import (
	"context"
	"encoding/json"
	"errors"

	core "github.com/brickKit/erp-costing/v3/internal/core"
	facts "github.com/brickKit/erp-costing/v3/internal/facts"

	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/brickKit/be-sdk-go/v3/events"
	"github.com/brickKit/be-sdk-go/v3/money"
	"github.com/jackc/pgx/v5"
)

// The price sources, lowest first (LY01).
const (
	SourcePolicy  = "policy"
	SourceBasis   = "basis"
	SourceInvoice = "invoice"
)

type basisIn struct {
	FactID        string `json:"fact_id"`
	LegalEntityID string `json:"legal_entity_id"`
	ProductID     string `json:"product_id"`
	UnitPrice     string `json:"unit_price"`
	Currency      string `json:"currency"`
	BusinessDate  string `json:"business_date"`
}

// RegisterBasis keeps a valuation basis by its receipt line (its fact_id), once, not yet converted to the base
// currency (LY02); ok is false for one kept before or not one.
func RegisterBasis(m *core.Base, ctx context.Context, tx db.Tx, e events.Received) (line string, ok bool, err error) {
	var b basisIn
	if json.Unmarshal(e.Data, &b) != nil || !core.UUIDForm.MatchString(b.FactID) || !core.UUIDForm.MatchString(b.LegalEntityID) ||
		!core.UUIDForm.MatchString(b.ProductID) || !core.DateForm.MatchString(b.BusinessDate) {
		m.RT.Logger.Error("a valuation basis that is not one is dropped", "event", e.ID)
		return "", false, nil
	}
	if _, perr := money.Parse(b.UnitPrice); perr != nil {
		m.RT.Logger.Error("a valuation basis without a price is dropped", "event", e.ID, "fact_id", b.FactID)
		return "", false, nil
	}
	tag, err := tx.Exec(ctx, `INSERT INTO basis (receipt_line_id, legal_entity_id, product_id, unit_price, currency,
		business_date, data) VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING`, b.FactID, b.LegalEntityID,
		b.ProductID, b.UnitPrice, b.Currency, b.BusinessDate, e.Data)
	return b.FactID, err == nil && tag.RowsAffected() == 1, err
}

// basisOf is a receipt line's valuation basis usable for a new layer: converted to the base currency (LY02), not
// withdrawn (LY05).
func basisOf(ctx context.Context, tx db.Tx, line string) (string, bool, error) {
	var price string
	err := tx.QueryRow(ctx, `SELECT base_price::text FROM basis b WHERE receipt_line_id = $1 AND base_price IS NOT NULL
		AND NOT EXISTS (SELECT 1 FROM basis_withdrawal w WHERE w.receipt_line_id = b.receipt_line_id)`, line).Scan(&price)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", false, nil
	}
	return price, err == nil, err
}

// PolicyPrice is the price of a receipt without a basis (LY01) and of an issue past the stock (IS02): the scope's
// latest layer's unit cost; without layers the moving average; without either 0, warned.
func PolicyPrice(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope) (string, error) {
	var price string
	err := tx.QueryRow(ctx, `SELECT unit_cost::text FROM cost_layer WHERE scope_id = $1 ORDER BY ordinal DESC LIMIT 1`,
		s.ID).Scan(&price)
	switch {
	case err == nil:
		return price, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return "", err
	case core.Cmp(s.Quantity, "0") > 0:
		return core.Quo(s.Value, s.Quantity), nil
	}
	m.RT.Logger.Warn("no price for the policy: valued at 0 (a later basis or invoice replaces it)",
		"event", "policy_price_zero", "legal_entity_id", s.LegalEntityID, "product_id", s.ProductID)
	return "0", nil
}

// Receive values a receipt (LY01): its layer at the basis's price when it is a purchase receipt whose basis came, else
// at the policy price; the confirmations kept for it cover their units at the invoice's price from the start (LY03);
// the scope's stock grows by the layer's value and its receipt fact is published (CF01). A purchase receipt whose
// basis has not come waits unless policy says the wait is over: wait is true and nothing is written. source is the
// layer's base source.
func Receive(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, mv core.Movement, policy bool) (cost, source string, wait bool, err error) {
	var line *string
	price := ""
	source = SourcePolicy
	if r := mv.Reference; r != nil && r.Kind == core.RefPurchaseReceipt && r.LineID != nil && core.UUIDForm.MatchString(*r.LineID) {
		line = r.LineID
		p, ok, err := basisOf(ctx, tx, *line)
		switch {
		case err != nil:
			return "", "", false, err
		case ok:
			price, source = p, SourceBasis
		case !policy:
			return "", "", true, nil
		}
	}
	if price == "" {
		if price, err = PolicyPrice(m, ctx, tx, s); err != nil {
			return "", "", false, err
		}
	}
	if _, err := tx.Exec(ctx, `INSERT INTO cost_layer (movement_id, scope_id, legal_entity_id, product_id, receipt_line_id,
		business_date, quantity, unit_cost, source) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, mv.FactID, s.ID,
		s.LegalEntityID, s.ProductID, line, mv.BusinessDate, mv.Quantity, price, source); err != nil {
		return "", "", false, err
	}
	l := &layer{MovementID: mv.FactID, ScopeID: s.ID, Quantity: mv.Quantity, Issued: "0"}
	var sourceID *string
	if source == SourceBasis {
		sourceID = line
	}
	if err := l.setBase(ctx, tx, source, sourceID, price); err != nil {
		return "", "", false, err
	}
	if err := applyPending(ctx, tx, l); err != nil {
		return "", "", false, err
	}
	if err := l.save(ctx, tx); err != nil {
		return "", "", false, err
	}
	cost = core.Amount(l.value(), s.Scale)
	s.Quantity = core.Sum(s.Quantity, mv.Quantity)
	s.Value = core.Sum(s.Value, cost)
	return cost, source, false, facts.Publish(m, ctx, tx, facts.Cost{FactID: core.FactOf(mv.FactID, core.FactValued),
		LegalEntityID: s.LegalEntityID, ProductID: s.ProductID, MovementID: mv.FactID,
		Trigger: facts.Trigger{Kind: facts.TriggerMovement, FactID: mv.FactID}, Date: mv.BusinessDate, Rule: facts.RuleReceipt,
		Currency: s.Currency, Amounts: map[string]string{facts.RoleInventory: cost, facts.RoleGRNI: cost}})
}
