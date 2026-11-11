package layers

import (
	"context"
	"encoding/json"

	core "github.com/brickKit/erp-costing/v3/internal/core"
	facts "github.com/brickKit/erp-costing/v3/internal/facts"
	methods "github.com/brickKit/erp-costing/v3/internal/methods"

	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/brickKit/be-sdk-go/v3/events"
	"github.com/brickKit/be-sdk-go/v3/money"
	"github.com/jackc/pgx/v5"
)

// The higher sources of the layers (LY02-LY05): what came is kept here in the consumer's transaction; it acts on the
// layers once its price is in the base currency (the sources module converts it outside a transaction, then calls
// the Apply functions in a transaction that holds the scope).

// Basis is a kept valuation basis.
type Basis struct {
	Line          string
	LegalEntityID string
	ProductID     string
	UnitPrice     string
	Currency      string
	Date          string
	BasePrice     *string
}

// BasisByLine reads a kept basis.
func BasisByLine(ctx context.Context, tx db.Tx, line string) (Basis, error) {
	b := Basis{Line: line}
	err := tx.QueryRow(ctx, `SELECT legal_entity_id::text, product_id::text, unit_price::text, currency,
		business_date::text, base_price::text FROM basis WHERE receipt_line_id = $1`, line).Scan(&b.LegalEntityID,
		&b.ProductID, &b.UnitPrice, &b.Currency, &b.Date, &b.BasePrice)
	return b, err
}

// RegisterWithdrawal keeps a withdrawal of a basis (LY05): the basis is no longer used for a new layer; a layer that
// used it keeps it, warned (the two sides do not agree: someone looks).
func RegisterWithdrawal(m *core.Base, ctx context.Context, tx db.Tx, e events.Received) error {
	var w struct {
		FactID      string `json:"fact_id"`
		BasisFactID string `json:"basis_fact_id"`
	}
	if json.Unmarshal(e.Data, &w) != nil || !core.UUIDForm.MatchString(w.FactID) || !core.UUIDForm.MatchString(w.BasisFactID) {
		m.RT.Logger.Error("a withdrawal that is not one is dropped", "event", e.ID)
		return nil
	}
	tag, err := tx.Exec(ctx, `INSERT INTO basis_withdrawal (receipt_line_id, fact_id, data) VALUES ($1, $2, $3)
		ON CONFLICT DO NOTHING`, w.BasisFactID, w.FactID, e.Data)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT l.movement_id::text FROM cost_layer l JOIN layer_source s ON s.movement_id = l.movement_id
		AND s.kind = 'basis' AND s.source_id = $1 AND NOT s.withdrawn`, w.BasisFactID)
	if err != nil {
		return err
	}
	used, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, mv := range used {
		l, err := load(ctx, tx, mv)
		if err != nil {
			return err
		}
		if Lifecycle.Decide(l.state(), OnWithdraw).Effect == core.Proceed {
			m.RT.Logger.Warn("a valuation basis a layer uses was withdrawn: the layer keeps it; erp/purchase and erp/costing "+
				"do not agree", "event", "basis_withdrawn_in_use", "receipt_line_id", w.BasisFactID, "movement_id", mv)
		}
	}
	return nil
}

// ConvertBasis records a basis's price in the base currency and the rate it took; false when it was done before.
func ConvertBasis(ctx context.Context, tx db.Tx, line, basePrice string, rate any) (bool, error) {
	tag, err := tx.Exec(ctx, `UPDATE basis SET base_price = $2, rate = $3 WHERE receipt_line_id = $1 AND base_price IS NULL`,
		line, basePrice, rate)
	return err == nil && tag.RowsAffected() == 1, err
}

// ApplyBasis acts on the layers of the basis's receipt line in the scope (LY02): a layer at the policy price takes
// the basis, every unit no invoice covers; the difference is an adjustment (AD01).
func ApplyBasis(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, b Basis) error {
	rows, err := tx.Query(ctx, `SELECT movement_id::text FROM cost_layer WHERE scope_id = $1 AND receipt_line_id = $2`, s.ID, b.Line)
	if err != nil {
		return err
	}
	movements, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, mv := range movements {
		l, err := load(ctx, tx, mv)
		if err != nil {
			return err
		}
		if Lifecycle.Decide(l.state(), OnBasis).Effect == core.Ignore || l.Base.Kind == SourceBasis {
			continue // the basis is in already
		}
		diff := core.Minus(*b.BasePrice, l.Base.Price)
		stretches := l.uncovered()
		if err := l.setBase(ctx, tx, SourceBasis, &b.Line, *b.BasePrice); err != nil {
			return err
		}
		if err := adjust(m, ctx, tx, s, l, stretches, diff, core.FactOf(mv, b.Line, core.FactReplaced), b.Date, nil,
			facts.Trigger{Kind: facts.TriggerBasis, FactID: b.Line}); err != nil {
			return err
		}
	}
	return nil
}

// Confirmation is a kept price confirmation (LY03).
type Confirmation struct {
	FactID        string
	LegalEntityID string
	SupplierID    *string
	UnitPrice     string
	Currency      string
	Date          string
	BasePrice     *string
	Reversed      bool
}

type confirmationIn struct {
	FactID        string  `json:"fact_id"`
	LegalEntityID string  `json:"legal_entity_id"`
	SupplierID    *string `json:"supplier_id"`
	Movements     []struct {
		MovementID   string `json:"movement_id"`
		BaseQuantity string `json:"base_quantity"`
	} `json:"movements"`
	UnitPrice    string `json:"unit_price"`
	Currency     string `json:"currency"`
	BusinessDate string `json:"business_date"`
	RecordedAt   string `json:"recorded_at"`
}

// RegisterConfirmation keeps a price confirmation and its matches, in the order they came (LY03); one reversed before
// it came is kept reversed (LY04). ok is false when it was kept before or is not one.
func RegisterConfirmation(m *core.Base, ctx context.Context, tx db.Tx, e events.Received) (bool, error) {
	var c confirmationIn
	if json.Unmarshal(e.Data, &c) != nil || !core.UUIDForm.MatchString(c.FactID) || !core.UUIDForm.MatchString(c.LegalEntityID) ||
		len(c.Movements) == 0 {
		m.RT.Logger.Error("a price confirmation that is not one is dropped", "event", e.ID)
		return false, nil
	}
	if _, err := money.Parse(c.UnitPrice); err != nil {
		m.RT.Logger.Error("a price confirmation without a price is dropped", "event", e.ID, "fact_id", c.FactID)
		return false, nil
	}
	date := factDate(c.BusinessDate, c.RecordedAt)
	tag, err := tx.Exec(ctx, `INSERT INTO confirmation (fact_id, legal_entity_id, supplier_id, unit_price, currency,
		business_date, reversed, data) VALUES ($1, $2, $3, $4, $5, $6, EXISTS (SELECT 1 FROM reversal WHERE confirmed_fact_id = $1), $7)
		ON CONFLICT DO NOTHING`, c.FactID, c.LegalEntityID, c.SupplierID, c.UnitPrice, c.Currency, date, e.Data)
	if err != nil || tag.RowsAffected() == 0 {
		return false, err
	}
	for _, mv := range c.Movements {
		if !core.UUIDForm.MatchString(mv.MovementID) || core.Cmp(mv.BaseQuantity, "0") <= 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `INSERT INTO confirmation_match (confirmation_id, movement_id, quantity, state)
			VALUES ($1, $2, $3, CASE WHEN (SELECT reversed FROM confirmation WHERE fact_id = $1) THEN 'void' ELSE 'pending' END)
			ON CONFLICT DO NOTHING`, c.FactID, mv.MovementID, mv.BaseQuantity); err != nil {
			return false, err
		}
	}
	return true, nil
}

// factDate is a source fact's business date, or the day it was recorded (facts published before contract alpha.4).
func factDate(date, recorded string) string {
	if core.DateForm.MatchString(date) {
		return date
	}
	if len(recorded) >= 10 && core.DateForm.MatchString(recorded[:10]) {
		return recorded[:10]
	}
	return "1970-01-01"
}

// RegisterReversal keeps a reversal of a confirmation (LY04); ok is false when kept before or not one.
func RegisterReversal(m *core.Base, ctx context.Context, tx db.Tx, e events.Received) (string, bool, error) {
	var r struct {
		FactID          string `json:"fact_id"`
		ConfirmedFactID string `json:"confirmed_fact_id"`
		BusinessDate    string `json:"business_date"`
		RecordedAt      string `json:"recorded_at"`
	}
	if json.Unmarshal(e.Data, &r) != nil || !core.UUIDForm.MatchString(r.FactID) || !core.UUIDForm.MatchString(r.ConfirmedFactID) {
		m.RT.Logger.Error("a price reversal that is not one is dropped", "event", e.ID)
		return "", false, nil
	}
	tag, err := tx.Exec(ctx, `INSERT INTO reversal (confirmed_fact_id, fact_id, business_date, data) VALUES ($1, $2, $3, $4)
		ON CONFLICT DO NOTHING`, r.ConfirmedFactID, r.FactID, factDate(r.BusinessDate, r.RecordedAt), e.Data)
	return r.ConfirmedFactID, err == nil && tag.RowsAffected() == 1, err
}

// LockConfirmation reads a kept confirmation FOR UPDATE.
func LockConfirmation(ctx context.Context, tx db.Tx, id string) (Confirmation, error) {
	c := Confirmation{FactID: id}
	err := tx.QueryRow(ctx, `SELECT legal_entity_id::text, supplier_id::text, unit_price::text, currency, business_date::text,
		base_price::text, reversed FROM confirmation WHERE fact_id = $1 FOR UPDATE`, id).Scan(&c.LegalEntityID, &c.SupplierID,
		&c.UnitPrice, &c.Currency, &c.Date, &c.BasePrice, &c.Reversed)
	return c, err
}

// ConvertConfirmation records a confirmation's price in the base currency and the rate it took.
func ConvertConfirmation(ctx context.Context, tx db.Tx, c *Confirmation, basePrice string, rate any) error {
	c.BasePrice = &basePrice
	_, err := tx.Exec(ctx, `UPDATE confirmation SET base_price = $2, rate = $3 WHERE fact_id = $1`, c.FactID, basePrice, rate)
	return err
}

// Match is a confirmation's match whose layer exists: the scope the layer is in.
type Match struct {
	MovementID string
	Quantity   string
	ScopeID    string
	State      string
}

// MatchesOf are a confirmation's matches of valued receipts (pending or applied), the layer's scope with each.
func MatchesOf(ctx context.Context, tx db.Tx, confirmation string) ([]Match, error) {
	rows, err := tx.Query(ctx, `SELECT m.movement_id::text, m.quantity::text, l.scope_id::text, m.state FROM confirmation_match m
		JOIN cost_layer l ON l.movement_id = m.movement_id WHERE m.confirmation_id = $1 AND m.state <> 'void'
		ORDER BY l.scope_id, m.movement_id`, confirmation)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Match, error) {
		var x Match
		return x, r.Scan(&x.MovementID, &x.Quantity, &x.ScopeID, &x.State)
	})
}

// ApplyConfirm replaces the price of the match's units with the invoice's (LY03): the next units no invoice covers,
// in the order the confirmations came; the difference is an adjustment (AD01) with the supplier.
func ApplyConfirm(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, c Confirmation, x Match) error {
	tag, err := tx.Exec(ctx, `UPDATE confirmation_match SET state = 'applied' WHERE confirmation_id = $1 AND movement_id = $2
		AND state = 'pending'`, c.FactID, x.MovementID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	l, err := load(ctx, tx, x.MovementID)
	if err != nil {
		return err
	}
	if Lifecycle.Decide(l.state(), OnConfirm).Effect == core.Ignore {
		return nil
	}
	stretches := take(l.uncovered(), x.Quantity)
	short(m, l, stretches, x.Quantity, c.FactID)
	if err := l.addInvoice(ctx, tx, c.FactID, stretches, *c.BasePrice); err != nil {
		return err
	}
	return adjust(m, ctx, tx, s, l, stretches, core.Minus(*c.BasePrice, l.Base.Price),
		core.FactOf(x.MovementID, c.FactID, core.FactReplaced), c.Date, c.SupplierID,
		facts.Trigger{Kind: facts.TriggerInvoice, FactID: c.FactID})
}

// short warns when a confirmation matches more units than its layer has without an invoice.
func short(m *core.Base, l *layer, got []methods.Range, want, confirmation string) {
	n := "0"
	for _, r := range got {
		n = core.Sum(n, r.Quantity)
	}
	if core.Cmp(n, want) < 0 {
		m.RT.Logger.Warn("a price confirmation matches more units than its layer has without an invoice: the rest is "+
			"ignored", "event", "confirmation_over_layer", "movement_id", l.MovementID, "confirmation", confirmation,
			"matched", want, "covered", n)
	}
}

// Reversal is a kept reversal.
type Reversal struct {
	FactID    string
	Confirmed string
	Date      string
}

// ReversalOf reads the reversal of a confirmation.
func ReversalOf(ctx context.Context, tx db.Tx, confirmed string) (Reversal, error) {
	r := Reversal{Confirmed: confirmed}
	return r, tx.QueryRow(ctx, `SELECT fact_id::text, business_date::text FROM reversal WHERE confirmed_fact_id = $1`,
		confirmed).Scan(&r.FactID, &r.Date)
}

// MarkReversed marks a confirmation reversed and voids its matches not applied yet (LY04).
func MarkReversed(ctx context.Context, tx db.Tx, confirmation string) error {
	if _, err := tx.Exec(ctx, `UPDATE confirmation SET reversed = true WHERE fact_id = $1`, confirmation); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE confirmation_match SET state = 'void' WHERE confirmation_id = $1 AND state = 'pending'`, confirmation)
	return err
}

// ApplyReverse takes an invoice's contribution off the match's layer (LY04): its units go back to the base source's
// price; the difference is an adjustment (AD01, AD02) dated by the reversal, with the supplier.
func ApplyReverse(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, c Confirmation, r Reversal, x Match) error {
	tag, err := tx.Exec(ctx, `UPDATE confirmation_match SET state = 'void' WHERE confirmation_id = $1 AND movement_id = $2
		AND state = 'applied'`, c.FactID, x.MovementID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	l, err := load(ctx, tx, x.MovementID)
	if err != nil {
		return err
	}
	if Lifecycle.Decide(l.state(), OnReverse).Effect == core.Ignore {
		return nil
	}
	var stretches []methods.Range
	kept := l.Invoices[:0]
	diff := ""
	for _, inv := range l.Invoices {
		if inv.SourceID == nil || *inv.SourceID != c.FactID {
			kept = append(kept, inv)
			continue
		}
		stretches = append(stretches, methods.Range{Start: inv.Start, Quantity: inv.Quantity})
		diff = core.Minus(l.Base.Price, inv.Price)
		if _, err := tx.Exec(ctx, `UPDATE layer_source SET withdrawn = true WHERE id = $1`, inv.ID); err != nil {
			return err
		}
	}
	l.Invoices = kept
	if len(stretches) == 0 {
		return nil
	}
	return adjust(m, ctx, tx, s, l, stretches, diff, core.FactOf(x.MovementID, c.FactID, core.FactReversed), r.Date, c.SupplierID,
		facts.Trigger{Kind: facts.TriggerReversal, FactID: r.FactID})
}

// applyPending lets a new layer take the confirmations kept for its movement (LY03): at the invoice's price from the
// start, no adjustment; the ones not yet in the base currency are left to the sources module, which adjusts.
func applyPending(ctx context.Context, tx db.Tx, l *layer) error {
	rows, err := tx.Query(ctx, `UPDATE confirmation_match m SET state = 'applied' FROM confirmation c
		WHERE m.confirmation_id = c.fact_id AND m.movement_id = $1 AND m.state = 'pending' AND c.base_price IS NOT NULL
		AND NOT c.reversed RETURNING c.fact_id::text, m.quantity::text, c.base_price::text, c.arrival`, l.MovementID)
	if err != nil {
		return err
	}
	type pending struct {
		id, qty, price string
		arrival        int64
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (pending, error) {
		var p pending
		return p, r.Scan(&p.id, &p.qty, &p.price, &p.arrival)
	})
	if err != nil {
		return err
	}
	for i := range all { // in the order the confirmations came
		for j := i + 1; j < len(all); j++ {
			if all[j].arrival < all[i].arrival {
				all[i], all[j] = all[j], all[i]
			}
		}
	}
	for _, p := range all {
		if err := l.addInvoice(ctx, tx, p.id, take(l.uncovered(), p.qty), p.price); err != nil {
			return err
		}
	}
	return nil
}

// adjust publishes an adjustment of diff a unit over the stretches of the layer (AD01): split by the scope's method
// (AD02), the inventory part into the scope's value; up or down, every amount positive; none when it is zero.
func adjust(m *core.Base, ctx context.Context, tx db.Tx, s *core.Scope, l *layer, stretches []methods.Range, diff, factID,
	date string, partner *string, trigger facts.Trigger) error {
	if err := l.save(ctx, tx); err != nil {
		return err
	}
	total, inventory, variance := methods.Split(s.Method, s.Quantity, l.Issued, stretches, diff, s.Scale)
	s.Value = core.Sum(s.Value, inventory)
	rule := facts.RuleAdjustUp
	if core.Cmp(total, "0") < 0 {
		rule = facts.RuleAdjustDown
		total, inventory, variance = core.Minus("0", total), core.Minus("0", inventory), core.Minus("0", variance)
	}
	return facts.Publish(m, ctx, tx, facts.Cost{FactID: factID, LegalEntityID: s.LegalEntityID, ProductID: s.ProductID,
		MovementID: l.MovementID, Trigger: trigger,
		Date: date, Rule: rule, Currency: s.Currency, PartnerID: partner, Amounts: map[string]string{
			facts.RoleInventory: core.Amount(inventory, s.Scale), facts.RoleVariance: core.Amount(variance, s.Scale),
			facts.RoleGRNI: core.Amount(total, s.Scale)}})
}
