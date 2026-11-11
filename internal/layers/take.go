package layers

import (
	"context"
	"errors"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/jackc/pgx/v5"
)

// Units leave a layer from its front (FIFO issues, purchase returns; LY03): the units after the issued ones, each at
// the price of the source covering it.

// rangeValue is the exact value of qty units of the layer from the start-th on.
func (l *layer) rangeValue(start, qty string) string {
	end := core.Sum(start, qty)
	v, covered := "0", "0"
	for _, s := range l.Invoices {
		from, to := maxDec(start, s.Start), minDec(end, core.Sum(s.Start, s.Quantity))
		if core.Cmp(to, from) > 0 {
			n := core.Minus(to, from)
			v = core.Sum(v, core.Mul(n, s.Price))
			covered = core.Sum(covered, n)
		}
	}
	return core.Sum(v, core.Mul(core.Minus(qty, covered), l.Base.Price))
}

// takeFront takes qty units from the layer's front (at most what is in stock) for the movement, records it in
// issue_layer, and answers the exact value and the quantity taken.
func (l *layer) takeFront(ctx context.Context, tx db.Tx, movement, qty string) (value, taken string, err error) {
	taken = minDec(qty, core.Minus(l.Quantity, l.Issued))
	if core.Cmp(taken, "0") <= 0 {
		return "0", "0", nil
	}
	value = l.rangeValue(l.Issued, taken)
	l.Issued = core.Sum(l.Issued, taken)
	if _, err := tx.Exec(ctx, `UPDATE cost_layer SET issued_quantity = $2, updated_at = now() WHERE movement_id = $1`,
		l.MovementID, l.Issued); err != nil {
		return "", "", err
	}
	_, err = tx.Exec(ctx, `INSERT INTO issue_layer (movement_id, layer_movement_id, quantity, value) VALUES ($1, $2, $3, $4)`,
		movement, l.MovementID, taken, value)
	return value, taken, err
}

// TakeOldest takes qty units for an issue from the scope's layers, oldest first (IS01 FIFO): the exact value of the
// units taken and how many there were (fewer than qty when the layers run out, IS02).
func TakeOldest(ctx context.Context, tx db.Tx, s *core.Scope, movement, qty string) (value, taken string, err error) {
	rows, err := tx.Query(ctx, `SELECT movement_id::text FROM cost_layer WHERE scope_id = $1 AND issued_quantity < quantity
		ORDER BY ordinal`, s.ID)
	if err != nil {
		return "", "", err
	}
	open, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return "", "", err
	}
	value, taken = "0", "0"
	for _, mv := range open {
		left := core.Minus(qty, taken)
		if core.Cmp(left, "0") <= 0 {
			break
		}
		l, err := load(ctx, tx, mv)
		if err != nil {
			return "", "", err
		}
		v, n, err := l.takeFront(ctx, tx, movement, left)
		if err != nil {
			return "", "", err
		}
		value, taken = core.Sum(value, v), core.Sum(taken, n)
	}
	return value, taken, nil
}

// Origin is a purchase return's receipt layer as the return reads it (RT01).
type Origin struct {
	Found         bool
	ScopeID       string
	LegalEntityID string
	UnitCost      string // the layer's weighted unit cost (moving average)
	InStock       string // the layer's units not yet gone (FIFO)
}

// OriginOf reads a receipt movement's layer.
func OriginOf(ctx context.Context, tx db.Tx, movement string) (Origin, error) {
	var o Origin
	err := tx.QueryRow(ctx, `SELECT scope_id::text, legal_entity_id::text, unit_cost::text, (quantity - issued_quantity)::text
		FROM cost_layer WHERE movement_id = $1`, movement).Scan(&o.ScopeID, &o.LegalEntityID, &o.UnitCost, &o.InStock)
	if errors.Is(err, pgx.ErrNoRows) {
		return o, nil
	}
	o.Found = err == nil
	return o, err
}

// TakeFromLayer takes qty units from a layer's front for a purchase return (RT01): the exact value of the units taken
// and how many (fewer when the layer's units ran out).
func TakeFromLayer(ctx context.Context, tx db.Tx, layerMovement, movement, qty string) (value, taken string, err error) {
	l, err := load(ctx, tx, layerMovement)
	if err != nil {
		return "", "", err
	}
	return l.takeFront(ctx, tx, movement, qty)
}

// MarkGone counts qty more of a moving average layer's units as gone (a purchase return, RT01): what FIFO's take does,
// without its prices.
func MarkGone(ctx context.Context, tx db.Tx, layerMovement, qty string) error {
	_, err := tx.Exec(ctx, `UPDATE cost_layer SET issued_quantity = issued_quantity + $2, updated_at = now() WHERE movement_id = $1`,
		layerMovement, qty)
	return err
}

func minDec(a, b string) string {
	if core.Cmp(a, b) <= 0 {
		return a
	}
	return b
}

func maxDec(a, b string) string {
	if core.Cmp(a, b) >= 0 {
		return a
	}
	return b
}
