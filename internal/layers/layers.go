// Package layers is the cost layers and their price sources (docs/design.md LY01-LY05): a receipt's layer, the
// valuation bases of erp/purchase, the policy price.
package layers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	core "github.com/brickKit/erp-costing/v3/internal/core"
	facts "github.com/brickKit/erp-costing/v3/internal/facts"

	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/brickKit/be-sdk-go/v3/events"
	"github.com/brickKit/be-sdk-go/v3/ids"
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

// RegisterBasis keeps a valuation basis by its receipt line (its fact_id), once; ok is false for a payload that is
// not one, and the scope it prices is answered so that a receipt waiting for it moves on (LY01).
func RegisterBasis(m *core.Base, ctx context.Context, tx db.Tx, e events.Received) (le, product string, ok bool, err error) {
	var b basisIn
	if json.Unmarshal(e.Data, &b) != nil || !core.UUIDForm.MatchString(b.FactID) || !core.UUIDForm.MatchString(b.LegalEntityID) ||
		!core.UUIDForm.MatchString(b.ProductID) || !core.DateForm.MatchString(b.BusinessDate) {
		m.RT.Logger.Error("a valuation basis that is not one is dropped", "event", e.ID)
		return "", "", false, nil
	}
	if _, perr := money.Parse(b.UnitPrice); perr != nil {
		m.RT.Logger.Error("a valuation basis without a price is dropped", "event", e.ID, "fact_id", b.FactID)
		return "", "", false, nil
	}
	_, err = tx.Exec(ctx, `INSERT INTO basis (receipt_line_id, legal_entity_id, product_id, unit_price, currency,
		business_date, data) VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING`, b.FactID, b.LegalEntityID,
		b.ProductID, b.UnitPrice, b.Currency, b.BusinessDate, e.Data)
	return b.LegalEntityID, b.ProductID, err == nil, err
}

// basis is a receipt line's valuation basis not withdrawn.
type basis struct {
	UnitPrice string
	Currency  string
}

func basisOf(ctx context.Context, tx db.Tx, line string) (basis, bool, error) {
	var b basis
	err := tx.QueryRow(ctx, `SELECT unit_price::text, currency FROM basis WHERE receipt_line_id = $1 AND withdrawn_at IS NULL`,
		line).Scan(&b.UnitPrice, &b.Currency)
	if errors.Is(err, pgx.ErrNoRows) {
		return b, false, nil
	}
	return b, err == nil, err
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
// at the policy price; the scope's stock grows by it and its receipt fact is published (CF01). A purchase receipt
// whose basis has not come waits unless policy says the wait is over: wait is true and nothing is written. source is
// the layer's price source.
func Receive(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, mv core.Movement, policy bool) (cost, source string, wait bool, err error) {
	var line *string
	price := ""
	source = SourcePolicy
	if r := mv.Reference; r != nil && r.Kind == core.RefPurchaseReceipt && r.LineID != nil && core.UUIDForm.MatchString(*r.LineID) {
		line = r.LineID
		b, ok, err := basisOf(ctx, tx, *line)
		switch {
		case err != nil:
			return "", "", false, err
		case ok && b.Currency != s.Currency:
			return "", "", false, fmt.Errorf("layers: the basis of line %s is in %s, the legal entity's currency is %s",
				*line, b.Currency, s.Currency)
		case ok:
			price, source = b.UnitPrice, SourceBasis
		case !policy:
			return "", "", true, nil
		}
	}
	if price == "" {
		if price, err = PolicyPrice(m, ctx, tx, s); err != nil {
			return "", "", false, err
		}
	}
	cost = core.Amount(core.Mul(mv.Quantity, price), s.Scale)
	if _, err := tx.Exec(ctx, `INSERT INTO cost_layer (movement_id, scope_id, legal_entity_id, product_id, receipt_line_id,
		business_date, quantity, unit_cost, source) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`, mv.FactID, s.ID,
		s.LegalEntityID, s.ProductID, line, mv.BusinessDate, mv.Quantity, price, source); err != nil {
		return "", "", false, err
	}
	var sourceID *string
	if source == SourceBasis {
		sourceID = line
	}
	if _, err := tx.Exec(ctx, `INSERT INTO layer_source (id, movement_id, kind, source_id, quantity, unit_price)
		VALUES ($1, $2, $3, $4, $5, $6)`, ids.New().String(), mv.FactID, source, sourceID, mv.Quantity, price); err != nil {
		return "", "", false, err
	}
	s.Quantity = core.Sum(s.Quantity, mv.Quantity)
	s.Value = core.Sum(s.Value, cost)
	return cost, source, false, facts.Publish(m, ctx, tx, facts.Cost{FactID: core.FactOf(mv.FactID, core.FactValued),
		LegalEntityID: s.LegalEntityID, MovementID: mv.FactID, Date: mv.BusinessDate, Rule: facts.RuleReceipt,
		Currency: s.Currency, Amounts: map[string]string{facts.RoleInventory: cost, facts.RoleGRNI: cost}})
}
