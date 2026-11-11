package sequence

import (
	"context"

	core "github.com/brickKit/erp-costing/v3/internal/core"
	layers "github.com/brickKit/erp-costing/v3/internal/layers"

	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/brickKit/be-sdk-go/v3/events"
	"github.com/brickKit/be-sdk-go/v3/ids"
)

// JobProcess values a scope's movements in their sequence (VS02); one pending at a time per scope (scope.queued),
// besides the ones due when a wait ends.
const JobProcess = "process-scope"

type processArgs struct {
	ScopeID string `json:"scope_id"`
}

// OnMovement receives an erp/inventory movement (VS01): the scope of its product and legal entity, created at the
// movement's epoch the first time; the movement kept in movement_in by its number; the scope's processing queued.
// A movement already valued, or of an older epoch than the scope follows (VS04, warned), is dropped.
func OnMovement(m *core.Base, ctx context.Context, tx db.Tx, e events.Received) error {
	mv, ok := core.ParseMovement(e.Data)
	if !ok {
		m.RT.Logger.Error("a movement this component cannot place in a valuation sequence is dropped", "event", e.ID)
		return nil
	}
	return keep(m, ctx, tx, mv, e.Data, true)
}

// keep puts a movement into its scope's movement_in, from the bus (wakeUp: queue the processing) or read back by the
// processing itself (VS03, VS04).
func keep(m *core.Base, ctx context.Context, tx db.Tx, mv core.Movement, data []byte, wakeUp bool) error {
	seq := mv.ValuationSeq
	if _, err := tx.Exec(ctx, `INSERT INTO scope (id, legal_entity_id, product_id, epoch) VALUES ($1, $2, $3, $4)
		ON CONFLICT (legal_entity_id, product_id) DO NOTHING`, ids.New().String(), mv.LegalEntityID, mv.ProductID, seq.Epoch); err != nil {
		return err
	}
	var id, epoch string
	var next int64
	if err := tx.QueryRow(ctx, `SELECT id::text, epoch, next_seq FROM scope WHERE legal_entity_id = $1 AND product_id = $2
		FOR UPDATE`, mv.LegalEntityID, mv.ProductID).Scan(&id, &epoch, &next); err != nil {
		return err
	}
	switch c := core.CmpEpoch(seq.Epoch, epoch); {
	case c < 0:
		m.RT.Logger.Warn("a movement of an older epoch than its scope follows is dropped", "event", "older_epoch",
			"fact_id", mv.FactID, "epoch", seq.Epoch, "scope_epoch", epoch)
		return nil
	case c == 0 && seq.Seq < next:
		return nil // valued already
	}
	if _, err := tx.Exec(ctx, `INSERT INTO movement_in (scope_id, epoch, seq, fact_id, data) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT DO NOTHING`, id, seq.Epoch, seq.Seq, mv.FactID, data); err != nil || !wakeUp {
		return err
	}
	return wake(m, ctx, tx, id)
}

// OnBasis receives a valuation basis (LY01): kept by its receipt line; a scope waiting for a basis is woken.
func OnBasis(m *core.Base, ctx context.Context, tx db.Tx, e events.Received) error {
	le, product, ok, err := layers.RegisterBasis(m, ctx, tx, e)
	if err != nil || !ok {
		return err
	}
	var id string
	err = tx.QueryRow(ctx, `SELECT coalesce((SELECT id::text FROM scope WHERE legal_entity_id = $1 AND product_id = $2
		AND state = $3), '')`, le, product, WaitingBasis).Scan(&id)
	if err != nil || id == "" {
		return err
	}
	return wake(m, ctx, tx, id)
}

// wake queues the scope's processing unless one is pending.
func wake(m *core.Base, ctx context.Context, tx db.Tx, scopeID string) error {
	tag, err := tx.Exec(ctx, `UPDATE scope SET queued = true WHERE id = $1 AND NOT queued`, scopeID)
	if err != nil || tag.RowsAffected() == 0 {
		return err
	}
	_, err = m.RT.Work.Enqueue(ctx, tx, JobProcess, processArgs{ScopeID: scopeID})
	return err
}
