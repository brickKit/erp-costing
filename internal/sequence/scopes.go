package sequence

import (
	"context"
	"errors"
	"time"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	sdkauthz "github.com/brickKit/be-sdk-go/v3/authz"
	"github.com/brickKit/be-sdk-go/v3/canon"
	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/brickKit/be-sdk-go/v3/httpx"
	"github.com/brickKit/be-sdk-go/v3/list"
	"github.com/jackc/pgx/v5"
)

// Scope is a valuation scope as the user surface reads it.
type Scope struct {
	LegalEntityID string  `json:"legal_entity_id"`
	ProductID     string  `json:"product_id"`
	Method        string  `json:"method"`
	Epoch         string  `json:"epoch"`
	NextSeq       int64   `json:"next_seq"`
	State         string  `json:"state"`
	Quantity      string  `json:"quantity"`
	Value         string  `json:"value"`
	UnitCost      *string `json:"unit_cost"`
	Currency      string  `json:"currency"`
	WaitingSince  *string `json:"waiting_since"`
	StoppedReason *string `json:"stopped_reason"`
	UpdatedAt     string  `json:"updated_at"`

	id string
}

// Valuation is a scope read alone: FIFO adds its layers in stock (the cost layers' module, P3e).
type Valuation struct {
	Scope
	LayersInStock []any `json:"layers_in_stock"`
}

const cols = `s.id::text, s.legal_entity_id::text, s.product_id::text,
	coalesce((SELECT method FROM policy p WHERE p.legal_entity_id = s.legal_entity_id), 'moving_average'),
	s.epoch, s.next_seq, s.state, s.quantity::text, s.value::text, s.currency, s.waiting_since, s.stopped_reason, s.updated_at`

func scan(row pgx.Row) (Scope, error) {
	var s Scope
	var waiting *time.Time
	var at time.Time
	err := row.Scan(&s.id, &s.LegalEntityID, &s.ProductID, &s.Method, &s.Epoch, &s.NextSeq, &s.State, &s.Quantity, &s.Value,
		&s.Currency, &waiting, &s.StoppedReason, &at)
	s.UpdatedAt = canon.Time(at)
	if waiting != nil {
		w := canon.Time(*waiting)
		s.WaitingSince = &w
	}
	if core.Cmp(s.Quantity, "0") > 0 {
		u := core.Quo(s.Value, s.Quantity)
		s.UnitCost = &u
	}
	return s, err
}

// GetScope answers the valuation of a product in a legal entity: its progress, quantity, value and unit cost.
func GetScope(m *core.Base, c *httpx.Context) error {
	le, product := c.Param("legal_entity_id"), c.Param("product_id")
	if !core.UUIDForm.MatchString(le) || !core.UUIDForm.MatchString(product) {
		return core.NotFound("valuation scope")
	}
	ctx, _, err := core.Check(c, core.OpValuationRead, core.EntityFacts(le))
	if err != nil {
		return err
	}
	var s Scope
	err = m.RT.DB.Tx(ctx, func(ctx context.Context, tx db.Tx) error {
		var err error
		s, err = scan(tx.QueryRow(ctx, `SELECT `+cols+` FROM scope s WHERE s.legal_entity_id = $1 AND s.product_id = $2`, le, product))
		return err
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return core.NotFound("valuation scope")
	}
	if err != nil {
		return err
	}
	return c.JSON(200, Valuation{Scope: s, LayersInStock: []any{}})
}

var scopeList = list.Spec{Operation: core.OpValuationRead, Alias: "s", IDColumn: "id", IDKind: list.UUID,
	Columns: []list.Column{
		{Field: "updated_at", Column: "updated_at", Kind: list.Time, Sortable: true},
		{Field: "legal_entity_id", Column: "legal_entity_id", Kind: list.UUID, Filterable: true},
		{Field: "product_id", Column: "product_id", Kind: list.UUID, Filterable: true},
		{Field: "state", Column: "state", Kind: list.Text, Filterable: true}}}

// ListScopes is one page of valuation scopes, the latest changed first.
func ListScopes(m *core.Base, c *httpx.Context) error {
	var f []list.Filter
	for _, k := range []string{"legal_entity_id", "product_id", "state"} {
		if v := c.Query(k); v != "" {
			f = append(f, list.Filter{Field: k, Op: "eq", Values: []string{v}})
		}
	}
	limit, err := list.ParseLimit(c.Query("limit"))
	if err != nil {
		return err
	}
	s, _ := sdkauthz.From(c.Ctx())
	ctx, q, err := list.Build(c.Ctx(), s, scopeList, list.Request{Filters: f, Limit: limit, Cursor: c.Query("cursor"),
		Sort: []list.Order{{Field: "updated_at", Desc: true}}})
	if err != nil {
		return err
	}
	out, next, err := list.Page(ctx, m.RT.DB, q, "scope", cols, scan,
		func(s Scope) ([]string, string) { return []string{s.UpdatedAt}, s.id })
	if err != nil {
		return err
	}
	return c.JSON(200, map[string]any{"scopes": out, "next_cursor": next})
}
