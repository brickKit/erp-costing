// Package sources takes in the higher price sources of the layers (docs/design.md LY02-LY05): erp/purchase's valuation
// bases and their withdrawals, erp/finance's price confirmations and their reversals. Each is kept in the consumer's
// transaction; a job converts its price to the legal entity's base currency outside any transaction (mdm/currency),
// then acts on the layers in a transaction that holds their scopes.
package sources

import (
	"context"
	"encoding/json"
	"errors"

	core "github.com/brickKit/erp-costing/v3/internal/core"
	layers "github.com/brickKit/erp-costing/v3/internal/layers"
	masterdata "github.com/brickKit/erp-costing/v3/internal/masterdata"
	policies "github.com/brickKit/erp-costing/v3/internal/policies"
	sequence "github.com/brickKit/erp-costing/v3/internal/sequence"

	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/brickKit/be-sdk-go/v3/errs"
	"github.com/brickKit/be-sdk-go/v3/events"
	"github.com/brickKit/be-sdk-go/v3/work"
	"github.com/jackc/pgx/v5"
)

// JobApply acts on the layers with one kept source.
const JobApply = "apply-source"

// The kinds of source a JobApply item carries.
const (
	kindBasis   = "basis"
	kindConfirm = "confirm"
	kindReverse = "reverse"
)

type applyArgs struct {
	Kind string `json:"kind"`
	ID   string `json:"id"` // the receipt line, or the confirmation
}

// OnBasis keeps a valuation basis (LY01, LY02) and queues its conversion.
func OnBasis(m *core.Base, ctx context.Context, tx db.Tx, e events.Received) error {
	line, ok, err := layers.RegisterBasis(m, ctx, tx, e)
	if err != nil || !ok {
		return err
	}
	_, err = m.RT.Work.Enqueue(ctx, tx, JobApply, applyArgs{Kind: kindBasis, ID: line})
	return err
}

// OnWithdrawn keeps a withdrawal of a basis (LY05).
func OnWithdrawn(m *core.Base, ctx context.Context, tx db.Tx, e events.Received) error {
	return layers.RegisterWithdrawal(m, ctx, tx, e)
}

// OnConfirmed keeps a price confirmation (LY03) and queues its conversion.
func OnConfirmed(m *core.Base, ctx context.Context, tx db.Tx, e events.Received) error {
	var head struct {
		FactID string `json:"fact_id"`
	}
	ok, err := layers.RegisterConfirmation(m, ctx, tx, e)
	if err != nil || !ok || json.Unmarshal(e.Data, &head) != nil {
		return err
	}
	_, err = m.RT.Work.Enqueue(ctx, tx, JobApply, applyArgs{Kind: kindConfirm, ID: head.FactID})
	return err
}

// OnReversed keeps a reversal (LY04) and queues it.
func OnReversed(m *core.Base, ctx context.Context, tx db.Tx, e events.Received) error {
	confirmed, ok, err := layers.RegisterReversal(m, ctx, tx, e)
	if err != nil || !ok {
		return err
	}
	_, err = m.RT.Work.Enqueue(ctx, tx, JobApply, applyArgs{Kind: kindReverse, ID: confirmed})
	return err
}

// Apply acts on the layers with a kept source.
func Apply(m *core.Base, ctx context.Context, r *work.Run) error {
	var a applyArgs
	if err := json.Unmarshal(r.Args, &a); err != nil {
		return work.Permanent(err)
	}
	var err error
	switch a.Kind {
	case kindBasis:
		err = applyBasis(m, ctx, a.ID)
	case kindConfirm:
		err = applyConfirm(m, ctx, a.ID)
	case kindReverse:
		err = applyReverse(m, ctx, a.ID)
	default:
		return work.Permanent(errors.New("sources: no kind " + a.Kind))
	}
	if errs.Is(err, core.ID, "CURRENCY_NOT_USABLE") {
		return work.Wait(err) // a rate may come
	}
	return err
}

// entity is what a source's conversion needs of its legal entity, read outside a transaction.
type entity struct {
	currency string
	scale    int32
}

func entityOf(m *core.Base, ctx context.Context, le string) (entity, error) {
	l, err := masterdata.LegalEntityOf(m, ctx, le)
	if err != nil {
		return entity{}, err
	}
	scale, err := masterdata.CurrencyScale(m, ctx, l.BaseCurrency)
	return entity{l.BaseCurrency, scale}, err
}

// convert is a price in the base currency on the date (LY02, LY03), with the rate it took (nil: the same currency).
func convert(m *core.Base, ctx context.Context, price, currency, base, date string) (string, *masterdata.Rate, error) {
	rate, err := masterdata.RateOf(m, ctx, currency, base, date)
	if err != nil || rate == nil {
		return price, nil, err
	}
	return core.Mul(price, rate.Rate), rate, nil
}

// applyBasis converts a basis, then lets the layers of its receipt line take it (LY02) and wakes a scope waiting for
// it (LY01).
func applyBasis(m *core.Base, ctx context.Context, line string) error {
	var b layers.Basis
	err := m.RT.DB.BackgroundTx(ctx, func(ctx context.Context, tx db.Tx) error {
		var err error
		b, err = layers.BasisByLine(ctx, tx, line)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return work.Permanent(errors.New("sources: no basis " + line))
	}
	if err != nil || b.BasePrice != nil {
		return err
	}
	en, err := entityOf(m, ctx, b.LegalEntityID)
	if err != nil {
		return err
	}
	price, rate, err := convert(m, ctx, b.UnitPrice, b.Currency, en.currency, b.Date)
	if err != nil {
		return err
	}
	return m.RT.DB.BackgroundTx(ctx, func(ctx context.Context, tx db.Tx) error {
		id, err := sequence.ScopeOf(ctx, tx, b.LegalEntityID, b.ProductID)
		if err != nil {
			return err
		}
		s, found, err := lockScope(ctx, tx, id, en)
		if err != nil {
			return err
		}
		if done, err := layers.ConvertBasis(ctx, tx, line, price, rateJSON(rate)); err != nil || !done {
			return err
		}
		if !found {
			return nil
		}
		b.BasePrice = &price
		if err := layers.ApplyBasis(m, ctx, tx, &s, b); err != nil {
			return err
		}
		if err := sequence.SaveValue(ctx, tx, &s); err != nil {
			return err
		}
		if s.State == sequence.WaitingBasis {
			return sequence.Wake(m, ctx, tx, s.ID)
		}
		return nil
	})
}

// applyConfirm converts a confirmation, then lets each valued layer it matches take it (LY03); the matches of
// receipts not valued yet stay kept for their layers.
func applyConfirm(m *core.Base, ctx context.Context, id string) error {
	var c layers.Confirmation
	err := m.RT.DB.BackgroundTx(ctx, func(ctx context.Context, tx db.Tx) error {
		var err error
		c, err = layers.LockConfirmation(ctx, tx, id)
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return work.Permanent(errors.New("sources: no confirmation " + id))
	}
	if err != nil || c.Reversed {
		return err
	}
	en, err := entityOf(m, ctx, c.LegalEntityID)
	if err != nil {
		return err
	}
	price, rate, err := convert(m, ctx, c.UnitPrice, c.Currency, en.currency, c.Date)
	if err != nil {
		return err
	}
	return m.RT.DB.BackgroundTx(ctx, func(ctx context.Context, tx db.Tx) error {
		c, err := layers.LockConfirmation(ctx, tx, id)
		if err != nil || c.Reversed {
			return err
		}
		if c.BasePrice == nil {
			if err := layers.ConvertConfirmation(ctx, tx, &c, price, rateJSON(rate)); err != nil {
				return err
			}
		}
		return eachScope(m, ctx, tx, id, en, "pending", func(s *core.Scope, x layers.Match) error {
			return layers.ApplyConfirm(m, ctx, tx, s, c, x)
		})
	})
}

// applyReverse takes a confirmation's contribution off its layers (LY04); a confirmation not come yet comes reversed.
func applyReverse(m *core.Base, ctx context.Context, id string) error {
	var le string
	err := m.RT.DB.BackgroundTx(ctx, func(ctx context.Context, tx db.Tx) error {
		return tx.QueryRow(ctx, `SELECT coalesce((SELECT legal_entity_id::text FROM confirmation WHERE fact_id = $1), '')`, id).Scan(&le)
	})
	if err != nil || le == "" {
		return err // not come yet: it comes reversed (layers.RegisterConfirmation)
	}
	en, err := entityOf(m, ctx, le)
	if err != nil {
		return err
	}
	return m.RT.DB.BackgroundTx(ctx, func(ctx context.Context, tx db.Tx) error {
		c, err := layers.LockConfirmation(ctx, tx, id)
		if err != nil || c.Reversed {
			return err
		}
		r, err := layers.ReversalOf(ctx, tx, id)
		if err != nil {
			return err
		}
		if err := layers.MarkReversed(ctx, tx, id); err != nil {
			return err
		}
		return eachScope(m, ctx, tx, id, en, "applied", func(s *core.Scope, x layers.Match) error {
			return layers.ApplyReverse(m, ctx, tx, s, c, r, x)
		})
	})
}

// eachScope holds the scopes of the confirmation's matches in state, one after another by ID (no two jobs lock them in
// opposite orders), and calls do for each match with its scope; the scopes' values are written back.
func eachScope(m *core.Base, ctx context.Context, tx db.Tx, confirmation string, en entity, state string,
	do func(*core.Scope, layers.Match) error) error {
	matches, err := layers.MatchesOf(ctx, tx, confirmation)
	if err != nil {
		return err
	}
	scopes := map[string]*core.Scope{}
	for _, x := range matches { // by scope
		if x.State != state {
			continue
		}
		s, ok := scopes[x.ScopeID]
		if !ok {
			sc, _, err := lockScope(ctx, tx, x.ScopeID, en)
			if err != nil {
				return err
			}
			s = &sc
			scopes[x.ScopeID] = s
		}
		if err := do(s, x); err != nil {
			return err
		}
	}
	for _, s := range scopes {
		if err := sequence.SaveValue(ctx, tx, s); err != nil {
			return err
		}
	}
	return nil
}

// lockScope holds a scope with what an adjustment needs: the currency, its places and the method.
func lockScope(ctx context.Context, tx db.Tx, id string, en entity) (core.Scope, bool, error) {
	if id == "" {
		return core.Scope{}, false, nil
	}
	s, ok, err := sequence.Lock(ctx, tx, id)
	if err != nil || !ok {
		return s, ok, err
	}
	s.Currency, s.Scale = en.currency, en.scale
	s.Method, err = policies.MethodOf(ctx, tx, s.LegalEntityID)
	return s, true, err
}

func rateJSON(r *masterdata.Rate) any {
	if r == nil {
		return nil
	}
	b, _ := json.Marshal(r)
	return b
}
