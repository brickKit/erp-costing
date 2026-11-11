package core

import (
	"bytes"
	"context"
	"encoding/json"
	"regexp"

	"github.com/brickKit/be-sdk-go/v3/audit"
	sdkauthz "github.com/brickKit/be-sdk-go/v3/authz"
	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/brickKit/be-sdk-go/v3/errs"
	"github.com/brickKit/be-sdk-go/v3/events"
	"github.com/brickKit/be-sdk-go/v3/httpx"
	"github.com/brickKit/be-sdk-go/v3/identity"
	apd "github.com/cockroachdb/apd/v3"
)

// The component's own reasons (the SDK's catalog has the shared ones).
var (
	ErrVersion = errs.Define(ID, "VERSION_CONFLICT", errs.Aborted, 409, errs.RetryNo, "The record changed since that version")
	ErrState   = errs.Define(ID, "STATE_CONFLICT", errs.FailedPrecondition, 409, errs.RetryNo,
		"The record's state does not allow this")
	ErrLegalEntity = errs.Define(ID, "LEGAL_ENTITY_NOT_USABLE", errs.FailedPrecondition, 409, errs.RetryNo,
		"The legal entity does not exist or is not active")
	// LY02, LY03: no spot rate to convert a source's price to the base currency
	ErrCurrency = errs.Define(ID, "CURRENCY_NOT_USABLE", errs.FailedPrecondition, 409, errs.RetryNo,
		"No rate converts the currency to the legal entity's base currency")
	// PO01: a legal entity with a valued movement keeps its costing method
	ErrPolicyInUse = errs.Define(ID, "POLICY_IN_USE", errs.FailedPrecondition, 409, errs.RetryNo,
		"A movement of the legal entity is valued: the costing method no longer changes")
)

var (
	UUIDForm = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
	DateForm = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
)

// EntityFacts are the facts of a record with the legal entity dimension.
func EntityFacts(legalEntity string) sdkauthz.Facts {
	return sdkauthz.Facts{DimLegalEntity: sdkauthz.Value(legalEntity)}
}

// BindOptional decodes a body the contract makes optional; no body leaves v as it is.
func BindOptional(c *httpx.Context, v any) error {
	body, err := c.Body()
	if err != nil || len(bytes.TrimSpace(body)) == 0 {
		return err
	}
	return c.BindJSON(v)
}

// Check decides op on a record with the given facts and returns the context to work in and the acting principal.
func Check(c *httpx.Context, op string, facts sdkauthz.Facts) (context.Context, string, error) {
	s, _ := sdkauthz.From(c.Ctx())
	ctx, _, err := s.Check(c.Ctx(), op, facts)
	if err != nil {
		return ctx, "", err
	}
	u, _ := identity.UserFrom(ctx)
	return ctx, u.Subject, nil
}

func Invalid(field, why string) error {
	return errs.New(errs.ReasonRequestInvalid, why).WithViolations(errs.Violation{Field: field, Reason: "INVALID", Description: why})
}

func NotFound(what string) error { return errs.New(errs.ReasonNotFound, what+" not found") }

// Change records the business change with the record's new state (protocol 03), in the transaction that made it.
func Change(m *Base, ctx context.Context, tx db.Tx, actor, op, typ, id string, after any) error {
	_, err := m.RT.Audit.Write(ctx, tx, audit.Record{Category: audit.BusinessChange, Operation: op, Outcome: audit.Succeeded,
		Actor: actor, Target: audit.TargetOf(typ, id, after), Changes: map[string]any{"after": after}})
	return err
}

// Emit writes an event into the outbox, in the transaction that changed the record (protocol 02, S2).
func Emit(m *Base, ctx context.Context, tx db.Tx, typ, id string, version int64, legalEntity string, data any) error {
	_, err := m.RT.Events.Emit(ctx, tx, Stream, events.Event{Type: typ, RecordID: id, Version: version,
		Projection: events.Full, LegalEntity: legalEntity, Data: data})
	return err
}

// Fact publishes an incremental fact (protocol 05, common rules) in the transaction that made it, and keeps it for
// FactService.Read: its record is the fact's ID.
func Fact(m *Base, ctx context.Context, tx db.Tx, typ, factID, legalEntity string, data any) error {
	b, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO costing_fact (fact_id, type, data) VALUES ($1, $2, $3)`, factID, typ, b); err != nil {
		return err
	}
	return Emit(m, ctx, tx, typ, factID, 1, legalEntity, json.RawMessage(b))
}

var exact = apd.BaseContext.WithPrecision(80)

func dec(s string) *apd.Decimal {
	d, _, _ := apd.NewFromString(s)
	return d
}

func text(d *apd.Decimal) string {
	d.Reduce(d)
	return d.Text('f')
}

// Sum is the exact sum of decimals.
func Sum(xs ...string) string {
	var r apd.Decimal
	for _, x := range xs {
		_, _ = exact.Add(&r, &r, dec(x))
	}
	return text(&r)
}

// Minus is a − b, exact.
func Minus(a, b string) string {
	var r apd.Decimal
	_, _ = exact.Sub(&r, dec(a), dec(b))
	return text(&r)
}

// Cmp compares decimals: -1, 0 or 1.
func Cmp(a, b string) int { return dec(a).Cmp(dec(b)) }
