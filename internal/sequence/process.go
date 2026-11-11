package sequence

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	core "github.com/brickKit/erp-costing/v3/internal/core"
	issues "github.com/brickKit/erp-costing/v3/internal/issues"
	layers "github.com/brickKit/erp-costing/v3/internal/layers"
	masterdata "github.com/brickKit/erp-costing/v3/internal/masterdata"
	policies "github.com/brickKit/erp-costing/v3/internal/policies"

	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/brickKit/be-sdk-go/v3/work"
	"github.com/jackc/pgx/v5"
)

// stepsPerRun bounds one run of JobProcess (its deadline); a scope with more to value queues the next run.
const stepsPerRun = 500

// next is what a step leaves for the run to do outside a transaction.
type next int

const (
	idle       next = iota // nothing to do now: the scope's queue flag is cleared
	again                  // take the next step
	refetch                // the gap wait is over: read the missing numbers back (VS03)
	closeEpoch             // a newer epoch's movement is here: read the old epoch to its end first (VS04)
)

// state is the scope as one step reads it, locked.
type state struct {
	core.Scope
	waitingSince *time.Time
	reason       *string
	gapDue       bool   // waiting_gap, and the gap wait is over
	basisDue     bool   // waiting_basis, and the basis wait is over
	newer        string // the next newer epoch a kept movement has, "" none
}

// Process values a scope's movements one by one in their sequence, one transaction each (VS02); it reads a gap or the
// end of an old epoch back from erp/inventory between the transactions (VS03, VS04), never inside one.
func Process(m *core.Base, ctx context.Context, r *work.Run) error {
	var a processArgs
	if err := json.Unmarshal(r.Args, &a); err != nil {
		return work.Permanent(err)
	}
	var le, product string
	err := m.RT.DB.BackgroundTx(ctx, func(ctx context.Context, tx db.Tx) error {
		return tx.QueryRow(ctx, `SELECT legal_entity_id::text, product_id::text FROM scope WHERE id = $1`, a.ScopeID).Scan(&le, &product)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return work.Permanent(errors.New("sequence: no scope " + a.ScopeID))
	}
	if err != nil {
		return err
	}
	entity, err := masterdata.LegalEntityOf(m, ctx, le)
	if err != nil {
		return err
	}
	scale, err := masterdata.CurrencyScale(m, ctx, entity.BaseCurrency)
	if err != nil {
		return err
	}
	for range stepsPerRun {
		var s state
		n, err := step(m, ctx, a.ScopeID, entity.BaseCurrency, scale, &s)
		if err != nil {
			return err
		}
		switch n {
		case idle:
			return nil
		case refetch:
			err = readGap(m, ctx, &s)
		case closeEpoch:
			err = readToEnd(m, ctx, &s)
		}
		if err != nil {
			return err
		}
	}
	return m.RT.DB.BackgroundTx(ctx, func(ctx context.Context, tx db.Tx) error { // more to do: the next run
		_, err := m.RT.Work.Enqueue(ctx, tx, JobProcess, a)
		return err
	})
}

// step looks at the scope's next number once and does what the lifecycle says (docs/design.md, the valuation progress
// table), in one transaction.
func step(m *core.Base, ctx context.Context, id, currency string, scale int32, s *state) (n next, err error) {
	err = m.RT.DB.BackgroundTx(ctx, func(ctx context.Context, tx db.Tx) error {
		if err := lock(m, ctx, tx, id, s); err != nil {
			return err
		}
		s.Currency, s.Scale = currency, scale
		if s.State == Stopped { // VS05: kept, not valued
			n = idle
			return save(ctx, tx, s, false)
		}
		var factID string
		var data []byte
		err := tx.QueryRow(ctx, `SELECT fact_id::text, data FROM movement_in WHERE scope_id = $1 AND epoch = $2 AND seq = $3`,
			s.ID, s.Epoch, s.NextSeq).Scan(&factID, &data)
		switch {
		case err == nil:
			n, err = valueNext(m, ctx, tx, s, factID, data)
		case errors.Is(err, pgx.ErrNoRows):
			n, err = missing(m, ctx, tx, s)
		}
		return err
	})
	return n, err
}

// lock reads the scope FOR UPDATE, drops the kept movements it is past (an older epoch: VS04, warned), and notes
// whether a wait is over and which newer epoch is waiting.
func lock(m *core.Base, ctx context.Context, tx db.Tx, id string, s *state) error {
	if err := tx.QueryRow(ctx, `SELECT id::text, legal_entity_id::text, product_id::text, epoch, next_seq, state,
		quantity::text, value::text, waiting_since, stopped_reason,
		coalesce(state = 'waiting_gap' AND waiting_since + make_interval(secs => $2) <= now(), false),
		coalesce(state = 'waiting_basis' AND waiting_since + make_interval(secs => $3) <= now(), false)
		FROM scope WHERE id = $1 FOR UPDATE`, id, m.GapWait.Seconds(), m.BasisWait.Seconds()).Scan(&s.ID, &s.LegalEntityID,
		&s.ProductID, &s.Epoch, &s.NextSeq, &s.State, &s.Quantity, &s.Value, &s.waitingSince, &s.reason, &s.gapDue,
		&s.basisDue); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM movement_in WHERE scope_id = $1 AND epoch = $2 AND seq < $3`, s.ID, s.Epoch,
		s.NextSeq); err != nil {
		return err
	}
	rows, err := tx.Query(ctx, `SELECT DISTINCT epoch FROM movement_in WHERE scope_id = $1 AND epoch <> $2`, s.ID, s.Epoch)
	if err != nil {
		return err
	}
	epochs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, e := range epochs {
		if core.CmpEpoch(e, s.Epoch) < 0 {
			m.RT.Logger.Warn("the movements of an older epoch than the scope follows are dropped", "event", "older_epoch",
				"scope_id", s.ID, "epoch", e, "scope_epoch", s.Epoch)
			if _, err := tx.Exec(ctx, `DELETE FROM movement_in WHERE scope_id = $1 AND epoch = $2`, s.ID, e); err != nil {
				return err
			}
		} else if s.newer == "" || core.CmpEpoch(e, s.newer) < 0 {
			s.newer = e
		}
	}
	return nil
}

// valueNext values the kept movement of the next number (VS02), or skips it when it was valued in an older epoch
// (VS04), or stops the scope on a kind this version does not value; a purchase receipt waits for its basis (LY01).
func valueNext(m *core.Base, ctx context.Context, tx db.Tx, s *state, factID string, data []byte) (next, error) {
	var valued bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM valued_movement WHERE fact_id = $1)`, factID).Scan(&valued); err != nil {
		return idle, err
	}
	if valued { // VS04: numbered again in a new epoch, valued once
		return again, advance(ctx, tx, s)
	}
	mv, _ := core.ParseMovement(data)
	method, err := policies.MethodOf(ctx, tx, s.LegalEntityID)
	if err != nil {
		return idle, err
	}
	s.Method = method
	command := Value
	var cost string
	switch mv.Kind {
	case core.KindReceipt:
		var source string
		var wait bool
		before, beforeValue := s.Quantity, s.Value
		if cost, source, wait, err = layers.Receive(m, ctx, tx, &s.Scope, mv, s.basisDue); err != nil {
			return idle, err
		}
		if wait {
			return idle, waitFor(m, ctx, tx, s, AwaitBasis, m.BasisWait)
		}
		if err := issues.Level(m, ctx, tx, &s.Scope, mv, before, beforeValue); err != nil { // IS02
			return idle, err
		}
		if s.State == WaitingBasis {
			command = BasisDue
			if source == layers.SourceBasis {
				command = Basis
			}
		}
	case core.KindIssue:
		cost, err = issues.Issue(m, ctx, tx, &s.Scope, mv)
	case core.KindPurchaseReturn:
		cost, err = issues.Return(m, ctx, tx, &s.Scope, mv)
	default:
		reason := fmt.Sprintf("movement %s (number %d of epoch %s) is a %q: not valued by this version", mv.FactID,
			s.NextSeq, s.Epoch, mv.Kind)
		m.RT.Logger.Error("a valuation scope stopped: "+reason, "event", "scope_stopped", "scope_id", s.ID)
		return idle, stop(ctx, tx, s, UnknownKind, reason)
	}
	if err != nil {
		return idle, err
	}
	if d := Lifecycle.Decide(s.State, command); d.Effect == core.Move {
		s.State = d.To
	}
	s.waitingSince = nil
	ref, _ := json.Marshal(mv.Reference)
	if _, err := tx.Exec(ctx, `INSERT INTO valued_movement (fact_id, scope_id, legal_entity_id, product_id, epoch, seq, kind,
		quantity, business_date, cost, reference) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`, mv.FactID, s.ID,
		s.LegalEntityID, s.ProductID, s.Epoch, s.NextSeq, mv.Kind, mv.Quantity, mv.BusinessDate, cost, ref); err != nil {
		return idle, err
	}
	return again, advance(ctx, tx, s)
}

// advance drops the next number's kept movement and moves on to the one after it.
func advance(ctx context.Context, tx db.Tx, s *state) error {
	if _, err := tx.Exec(ctx, `DELETE FROM movement_in WHERE scope_id = $1 AND epoch = $2 AND seq = $3`, s.ID, s.Epoch,
		s.NextSeq); err != nil {
		return err
	}
	s.NextSeq++
	return save(ctx, tx, s, true)
}

// missing: the next number has not come. A newer epoch's movement means the old epoch is to be read to its end
// (VS04); a later number of this epoch is a gap (VS03); nothing at all is nothing to do.
func missing(m *core.Base, ctx context.Context, tx db.Tx, s *state) (next, error) {
	if s.newer != "" {
		return closeEpoch, nil
	}
	var later bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM movement_in WHERE scope_id = $1 AND epoch = $2 AND seq > $3)`,
		s.ID, s.Epoch, s.NextSeq).Scan(&later); err != nil {
		return idle, err
	}
	switch {
	case !later:
		return idle, save(ctx, tx, s, false)
	case s.State == WaitingGap && s.gapDue:
		return refetch, nil
	case s.State == WaitingGap: // a run due at the end of the wait is queued
		return idle, save(ctx, tx, s, false)
	}
	return idle, waitFor(m, ctx, tx, s, AwaitGap, m.GapWait)
}

// waitFor moves the scope into a wait by the lifecycle and queues a run due when the wait ends, by the database's
// clock (the run is never early).
func waitFor(m *core.Base, ctx context.Context, tx db.Tx, s *state, command string, wait time.Duration) error {
	if d := Lifecycle.Decide(s.State, command); d.Effect == core.Move {
		s.State = d.To
		since, err := restartWait(m, ctx, tx, s.ID, wait)
		if err != nil {
			return err
		}
		s.waitingSince = &since
	}
	return save(ctx, tx, s, false)
}

// restartWait starts the scope's wait now and queues the run due at its end.
func restartWait(m *core.Base, ctx context.Context, tx db.Tx, scopeID string, wait time.Duration) (time.Time, error) {
	var since time.Time
	if err := tx.QueryRow(ctx, `SELECT now()`).Scan(&since); err != nil {
		return since, err
	}
	_, err := m.RT.Work.EnqueueAt(ctx, tx, JobProcess, processArgs{ScopeID: scopeID}, since.Add(wait))
	return since, err
}

// stop stops the scope by the lifecycle (VS02, VS04), with its reason (VS05).
func stop(ctx context.Context, tx db.Tx, s *state, command, reason string) error {
	if d := Lifecycle.Decide(s.State, command); d.Effect == core.Move {
		s.State, s.reason, s.waitingSince = d.To, &reason, nil
	}
	return save(ctx, tx, s, false)
}

// save writes the scope back; queued false says this run found nothing more to do.
func save(ctx context.Context, tx db.Tx, s *state, queued bool) error {
	q := `UPDATE scope SET epoch = $2, next_seq = $3, state = $4, quantity = $5, value = $6, currency = $7,
		waiting_since = $8, stopped_reason = $9, updated_at = now()`
	if !queued {
		q += `, queued = false`
	}
	_, err := tx.Exec(ctx, q+` WHERE id = $1`, s.ID, s.Epoch, s.NextSeq, s.State, core.Plain(s.Quantity),
		core.Amount(s.Value, s.Scale), s.Currency, s.waitingSince, s.reason)
	return err
}

// readGap reads the gap back from erp/inventory (VS03), a batch after another while they come full; when the next
// number is still not there, the scope waits another period, warned.
func readGap(m *core.Base, ctx context.Context, s *state) error {
	got, _, err := readBack(m, ctx, s)
	if err != nil || got {
		return err
	}
	m.RT.Logger.Warn("a gap in a valuation sequence is not in erp/inventory yet: waiting another period", "event",
		"gap_unfilled", "scope_id", s.ID, "epoch", s.Epoch, "seq", s.NextSeq)
	return m.RT.DB.BackgroundTx(ctx, func(ctx context.Context, tx db.Tx) error {
		var cur state
		if err := lock(m, ctx, tx, s.ID, &cur); err != nil {
			return err
		}
		if cur.State != WaitingGap || cur.Epoch != s.Epoch || cur.NextSeq != s.NextSeq {
			return nil // moved on meanwhile
		}
		cur.Currency, cur.Scale = s.Currency, s.Scale
		since, err := restartWait(m, ctx, tx, cur.ID, m.GapWait)
		cur.waitingSince = &since
		if err != nil {
			return err
		}
		return save(ctx, tx, &cur, false)
	})
}

// readToEnd reads the old epoch to its end (VS04); when erp/inventory has no more of it, the scope follows the newer
// epoch from 1, unless this component valued past what erp/inventory numbered in the old one: then it stops.
func readToEnd(m *core.Base, ctx context.Context, s *state) error {
	got, last, err := readBack(m, ctx, s)
	if err != nil || got {
		return err
	}
	return m.RT.DB.BackgroundTx(ctx, func(ctx context.Context, tx db.Tx) error {
		var cur state
		if err := lock(m, ctx, tx, s.ID, &cur); err != nil {
			return err
		}
		if cur.Epoch != s.Epoch || cur.NextSeq != s.NextSeq || cur.newer == "" {
			return nil // moved on meanwhile
		}
		cur.Currency, cur.Scale = s.Currency, s.Scale
		if cur.NextSeq-1 > last {
			reason := fmt.Sprintf("epoch %s: valued up to %d, erp/inventory numbered up to %d before epoch %s; "+
				"the movements past %d are to be aligned by identity", cur.Epoch, cur.NextSeq-1, last, cur.newer, last)
			m.RT.Logger.Error("a valuation scope stopped: "+reason, "event", "scope_stopped", "scope_id", cur.ID)
			return stop(ctx, tx, &cur, EpochAhead, reason)
		}
		d := Lifecycle.Decide(cur.State, NewEpoch)
		if d.Effect == core.Ignore {
			return nil
		}
		if d.Effect == core.Move {
			cur.State = d.To
		}
		cur.Epoch, cur.NextSeq, cur.waitingSince = cur.newer, 1, nil
		return save(ctx, tx, &cur, true)
	})
}

// readBack reads the scope's epoch back from its next number on and keeps what comes; got says the next number came,
// numbered is the highest number erp/inventory has given in the epoch.
func readBack(m *core.Base, ctx context.Context, s *state) (got bool, numbered int64, err error) {
	epoch, from := s.Epoch, s.NextSeq
	for {
		moves, lastSeq, err := masterdata.Movements(m, ctx, s.LegalEntityID, s.ProductID, epoch, from)
		if err != nil {
			return false, 0, err
		}
		numbered = lastSeq
		last := from - 1
		err = m.RT.DB.BackgroundTx(ctx, func(ctx context.Context, tx db.Tx) error {
			for _, b := range moves {
				mv, ok := core.ParseMovement(b)
				if !ok || mv.LegalEntityID != s.LegalEntityID || mv.ProductID != s.ProductID || mv.ValuationSeq.Epoch != epoch {
					m.RT.Logger.Error("erp/inventory answered a movement of another sequence; it is dropped", "scope_id", s.ID)
					continue
				}
				got = got || mv.ValuationSeq.Seq == s.NextSeq
				last = max(last, mv.ValuationSeq.Seq)
				if err := keep(m, ctx, tx, mv, b, false); err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil || len(moves) < core.MaxRefetch {
			return got, numbered, err
		}
		from = last + 1
	}
}
