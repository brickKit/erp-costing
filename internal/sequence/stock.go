package sequence

import (
	"context"
	"errors"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/jackc/pgx/v5"
)

// Lock holds a scope for a change of its stock's value from outside its sequence (a source replacing a price, LY02-
// LY04): FOR UPDATE, the same lock a valuation step takes. ok is false when the scope does not exist. The caller sets
// the currency, its places and the method.
func Lock(ctx context.Context, tx db.Tx, id string) (s core.Scope, ok bool, err error) {
	err = tx.QueryRow(ctx, `SELECT id::text, legal_entity_id::text, product_id::text, epoch, next_seq, state, quantity::text,
		value::text FROM scope WHERE id = $1 FOR UPDATE`, id).Scan(&s.ID, &s.LegalEntityID, &s.ProductID, &s.Epoch,
		&s.NextSeq, &s.State, &s.Quantity, &s.Value)
	if errors.Is(err, pgx.ErrNoRows) {
		return s, false, nil
	}
	return s, err == nil, err
}

// ScopeOf is the ID of a product's scope in a legal entity, "" when none.
func ScopeOf(ctx context.Context, tx db.Tx, le, product string) (string, error) {
	var id string
	err := tx.QueryRow(ctx, `SELECT coalesce((SELECT id::text FROM scope WHERE legal_entity_id = $1 AND product_id = $2), '')`,
		le, product).Scan(&id)
	return id, err
}

// SaveValue writes back the value a source changed.
func SaveValue(ctx context.Context, tx db.Tx, s *core.Scope) error {
	_, err := tx.Exec(ctx, `UPDATE scope SET value = $2, updated_at = now() WHERE id = $1`, s.ID, core.Amount(s.Value, s.Scale))
	return err
}
