package layers

import (
	"context"
	"errors"
	"strings"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	sdkauthz "github.com/brickKit/be-sdk-go/v3/authz"
	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/brickKit/be-sdk-go/v3/httpx"
	"github.com/brickKit/be-sdk-go/v3/list"
	"github.com/jackc/pgx/v5"
)

// Layer is a cost layer as the user surface reads it.
type Layer struct {
	MovementID     string      `json:"movement_id"`
	LegalEntityID  string      `json:"legal_entity_id"`
	ProductID      string      `json:"product_id"`
	ReceiptLineID  *string     `json:"receipt_line_id"`
	BusinessDate   string      `json:"business_date"`
	Quantity       string      `json:"quantity"`
	IssuedQuantity string      `json:"issued_quantity"`
	UnitCost       string      `json:"unit_cost"`
	ValueInStock   string      `json:"value_in_stock"`
	Source         string      `json:"source"`
	Sources        []SourceOut `json:"sources"`
}

// SourceOut is one of a layer's price sources: an invoice's stretch, a base source; withdrawn when replaced, reversed
// when its invoice was.
type SourceOut struct {
	Kind      string  `json:"kind"`
	FactID    *string `json:"fact_id"`
	Quantity  string  `json:"quantity"`
	UnitPrice string  `json:"unit_price"`
	Arrival   int64   `json:"arrival"`
	State     string  `json:"state"`
}

const layerCols = `l.movement_id::text, l.legal_entity_id::text, l.product_id::text, l.receipt_line_id::text,
	l.business_date::text, l.quantity::text, l.issued_quantity::text, l.unit_cost::text, l.source`

func scanLayer(row pgx.Row) (Layer, error) {
	var l Layer
	err := row.Scan(&l.MovementID, &l.LegalEntityID, &l.ProductID, &l.ReceiptLineID, &l.BusinessDate, &l.Quantity,
		&l.IssuedQuantity, &l.UnitCost, &l.Source)
	if err != nil {
		return l, err
	}
	l.Quantity, l.IssuedQuantity, l.UnitCost = core.Plain(l.Quantity), core.Plain(l.IssuedQuantity), core.Plain(l.UnitCost)
	return l, nil
}

// complete adds what is not a column: the value of the units in stock at their own prices, and the sources.
func complete(ctx context.Context, tx db.Tx, out *Layer) error {
	l, err := load(ctx, tx, out.MovementID)
	if err != nil {
		return err
	}
	out.ValueInStock = core.Plain(l.rangeValue(l.Issued, core.Minus(l.Quantity, l.Issued)))
	rows, err := tx.Query(ctx, `SELECT kind, source_id::text, quantity::text, unit_price::text, arrival, withdrawn
		FROM layer_source WHERE movement_id = $1 ORDER BY arrival`, out.MovementID)
	if err != nil {
		return err
	}
	out.Sources, err = pgx.CollectRows(rows, func(r pgx.CollectableRow) (SourceOut, error) {
		var s SourceOut
		var withdrawn bool
		if err := r.Scan(&s.Kind, &s.FactID, &s.Quantity, &s.UnitPrice, &s.Arrival, &withdrawn); err != nil {
			return s, err
		}
		s.Quantity, s.UnitPrice, s.State = core.Plain(s.Quantity), core.Plain(s.UnitPrice), "active"
		switch {
		case withdrawn && s.Kind == SourceInvoice:
			s.State = "reversed"
		case withdrawn:
			s.State = "withdrawn"
		}
		return s, nil
	})
	return err
}

// GetLayer answers a layer with its sources.
func GetLayer(m *core.Base, c *httpx.Context) error {
	id := c.Param("movement_id")
	if !core.UUIDForm.MatchString(id) {
		return core.NotFound("cost layer")
	}
	var out Layer
	err := m.RT.DB.Tx(c.Ctx(), func(ctx context.Context, tx db.Tx) error {
		var err error
		if out, err = scanLayer(tx.QueryRow(ctx, `SELECT `+layerCols+` FROM cost_layer l WHERE l.movement_id = $1`, id)); err != nil {
			return err
		}
		return complete(ctx, tx, &out)
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return core.NotFound("cost layer")
	}
	if err != nil {
		return err
	}
	if _, _, err := core.Check(c, core.OpValuationRead, core.EntityFacts(out.LegalEntityID)); err != nil {
		return err
	}
	return c.JSON(200, out)
}

// InStock are a scope's layers with units in stock, oldest first (a FIFO valuation's layers).
func InStock(ctx context.Context, tx db.Tx, scopeID string) ([]Layer, error) {
	rows, err := tx.Query(ctx, `SELECT `+layerCols+` FROM cost_layer l WHERE l.scope_id = $1 AND l.in_stock ORDER BY l.ordinal`, scopeID)
	if err != nil {
		return nil, err
	}
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (Layer, error) { return scanLayer(r) })
	if err != nil {
		return nil, err
	}
	for i := range out {
		if err := complete(ctx, tx, &out[i]); err != nil {
			return nil, err
		}
	}
	return out, nil
}

var layerList = list.Spec{Operation: core.OpValuationRead, Alias: "l", IDColumn: "movement_id", IDKind: list.UUID,
	Columns: []list.Column{
		{Field: "business_date", Column: "business_date", Kind: list.Date, Sortable: true},
		{Field: "legal_entity_id", Column: "legal_entity_id", Kind: list.UUID, Filterable: true},
		{Field: "product_id", Column: "product_id", Kind: list.UUID, Filterable: true},
		{Field: "source", Column: "source", Kind: list.Text, Filterable: true},
		{Field: "in_stock", Column: "in_stock", Kind: list.Boolean, Filterable: true}}}

// ListLayers is one page of layers, the latest business date first.
func ListLayers(m *core.Base, c *httpx.Context) error {
	var f []list.Filter
	for _, k := range []string{"legal_entity_id", "product_id", "source", "in_stock"} {
		if v := c.Query(k); v != "" {
			f = append(f, list.Filter{Field: k, Op: "eq", Values: []string{strings.ToLower(v)}})
		}
	}
	limit, err := list.ParseLimit(c.Query("limit"))
	if err != nil {
		return err
	}
	s, _ := sdkauthz.From(c.Ctx())
	ctx, q, err := list.Build(c.Ctx(), s, layerList, list.Request{Filters: f, Limit: limit, Cursor: c.Query("cursor"),
		Sort: []list.Order{{Field: "business_date", Desc: true}}})
	if err != nil {
		return err
	}
	out, next, err := list.Page(ctx, m.RT.DB, q, "cost_layer", layerCols, scanLayer,
		func(l Layer) ([]string, string) { return []string{l.BusinessDate}, l.MovementID })
	if err != nil {
		return err
	}
	err = m.RT.DB.Tx(ctx, func(ctx context.Context, tx db.Tx) error {
		for i := range out {
			if err := complete(ctx, tx, &out[i]); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return c.JSON(200, map[string]any{"layers": out, "next_cursor": next})
}
