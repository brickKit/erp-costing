package layers

import (
	"context"
	"errors"
	"sort"

	core "github.com/brickKit/erp-costing/v3/internal/core"
	methods "github.com/brickKit/erp-costing/v3/internal/methods"

	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/brickKit/be-sdk-go/v3/ids"
	"github.com/jackc/pgx/v5"
)

// A layer's units have each the price of the highest source covering it (LY03): an invoice covers the stretches it
// took, in the order the confirmations came; the other units take the base source, the basis or else the policy price.
// Issues and purchase returns take units from the front (FIFO).

// source is one of a layer's sources in effect.
type source struct {
	ID       string
	Kind     string
	SourceID *string
	Start    string // an invoice's first unit
	Quantity string
	Price    string
}

// layer is a cost layer with its sources in effect, read in a transaction that holds its scope.
type layer struct {
	MovementID string
	ScopeID    string
	Quantity   string
	Issued     string
	Base       source   // the policy or the basis
	Invoices   []source // by start
}

var errNoLayer = errors.New("layers: no layer")

// load reads a layer; errNoLayer when its movement is not valued yet.
func load(ctx context.Context, tx db.Tx, movement string) (*layer, error) {
	l := &layer{MovementID: movement}
	err := tx.QueryRow(ctx, `SELECT scope_id::text, quantity::text, issued_quantity::text FROM cost_layer
		WHERE movement_id = $1`, movement).Scan(&l.ScopeID, &l.Quantity, &l.Issued)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, errNoLayer
	}
	if err != nil {
		return nil, err
	}
	rows, err := tx.Query(ctx, `SELECT id::text, kind, source_id::text, coalesce(start, 0)::text, quantity::text,
		unit_price::text FROM layer_source WHERE movement_id = $1 AND NOT withdrawn ORDER BY arrival`, movement)
	if err != nil {
		return nil, err
	}
	all, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (source, error) {
		var s source
		return s, r.Scan(&s.ID, &s.Kind, &s.SourceID, &s.Start, &s.Quantity, &s.Price)
	})
	if err != nil {
		return nil, err
	}
	for _, s := range all {
		if s.Kind == SourceInvoice {
			l.Invoices = append(l.Invoices, s)
		} else {
			l.Base = s
		}
	}
	sort.Slice(l.Invoices, func(i, j int) bool { return core.Cmp(l.Invoices[i].Start, l.Invoices[j].Start) < 0 })
	return l, nil
}

// state is the layer's highest source in effect (the lifecycle's phase).
func (l *layer) state() string {
	if len(l.Invoices) > 0 {
		return SourceInvoice
	}
	return l.Base.Kind
}

// uncovered is the stretches of units no invoice covers, from the front.
func (l *layer) uncovered() []methods.Range {
	var out []methods.Range
	at := "0"
	for _, s := range l.Invoices {
		if core.Cmp(s.Start, at) > 0 {
			out = append(out, methods.Range{Start: at, Quantity: core.Minus(s.Start, at)})
		}
		at = core.Sum(s.Start, s.Quantity)
	}
	if core.Cmp(l.Quantity, at) > 0 {
		out = append(out, methods.Range{Start: at, Quantity: core.Minus(l.Quantity, at)})
	}
	return out
}

// take is the first qty units of the stretches; short when they hold fewer.
func take(stretches []methods.Range, qty string) []methods.Range {
	var out []methods.Range
	for _, r := range stretches {
		if core.Cmp(qty, "0") <= 0 {
			break
		}
		n := r.Quantity
		if core.Cmp(n, qty) > 0 {
			n = qty
		}
		out = append(out, methods.Range{Start: r.Start, Quantity: n})
		qty = core.Minus(qty, n)
	}
	return out
}

// value is the layer's units at their prices, exact.
func (l *layer) value() string {
	v, covered := "0", "0"
	for _, s := range l.Invoices {
		v = core.Sum(v, core.Mul(s.Quantity, s.Price))
		covered = core.Sum(covered, s.Quantity)
	}
	return core.Sum(v, core.Mul(core.Minus(l.Quantity, covered), l.Base.Price))
}

// addInvoice covers the stretches with an invoice's price.
func (l *layer) addInvoice(ctx context.Context, tx db.Tx, confirmation string, stretches []methods.Range, price string) error {
	for _, r := range stretches {
		s := source{ID: ids.New().String(), Kind: SourceInvoice, SourceID: &confirmation, Start: r.Start, Quantity: r.Quantity, Price: price}
		if _, err := tx.Exec(ctx, `INSERT INTO layer_source (id, movement_id, kind, source_id, start, quantity, unit_price)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, s.ID, l.MovementID, s.Kind, confirmation, s.Start, s.Quantity, price); err != nil {
			return err
		}
		l.Invoices = append(l.Invoices, s)
	}
	sort.Slice(l.Invoices, func(i, j int) bool { return core.Cmp(l.Invoices[i].Start, l.Invoices[j].Start) < 0 })
	return nil
}

// setBase replaces the base source (the policy by the basis); the replaced one is withdrawn, kept.
func (l *layer) setBase(ctx context.Context, tx db.Tx, kind string, sourceID *string, price string) error {
	if l.Base.ID != "" {
		if _, err := tx.Exec(ctx, `UPDATE layer_source SET withdrawn = true WHERE id = $1`, l.Base.ID); err != nil {
			return err
		}
	}
	l.Base = source{ID: ids.New().String(), Kind: kind, SourceID: sourceID, Quantity: l.Quantity, Price: price}
	_, err := tx.Exec(ctx, `INSERT INTO layer_source (id, movement_id, kind, source_id, quantity, unit_price)
		VALUES ($1, $2, $3, $4, $5, $6)`, l.Base.ID, l.MovementID, kind, sourceID, l.Quantity, price)
	return err
}

// save writes the layer's weighted unit cost and highest source.
func (l *layer) save(ctx context.Context, tx db.Tx) error {
	_, err := tx.Exec(ctx, `UPDATE cost_layer SET unit_cost = $2, source = $3, updated_at = now() WHERE movement_id = $1`,
		l.MovementID, core.Quo(l.value(), l.Quantity), l.state())
	return err
}
