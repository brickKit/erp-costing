package facts

import (
	"encoding/json"
	"time"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	sdkauthz "github.com/brickKit/be-sdk-go/v3/authz"
	"github.com/brickKit/be-sdk-go/v3/canon"
	"github.com/brickKit/be-sdk-go/v3/httpx"
	"github.com/brickKit/be-sdk-go/v3/list"
	"github.com/jackc/pgx/v5"
)

// Listed is a cost fact as the user surface lists it, with the input that made it.
type Listed struct {
	FactID        string            `json:"fact_id"`
	RuleKey       string            `json:"rule_key"`
	LegalEntityID string            `json:"legal_entity_id"`
	ProductID     string            `json:"product_id"`
	MovementID    string            `json:"movement_id"`
	BusinessDate  string            `json:"business_date"`
	Currency      string            `json:"currency"`
	Amounts       map[string]string `json:"amounts"`
	Trigger       Trigger           `json:"trigger"`
	RecordedAt    string            `json:"recorded_at"`
}

const listCols = `f.fact_id::text, f.rule_key, f.legal_entity_id::text, f.product_id::text, f.movement_id::text,
	f.business_date::text, f.data->>'currency', f.data->'amounts', f.trigger_kind, f.trigger_id::text, f.recorded_at`

func scanListed(row pgx.Row) (Listed, error) {
	var f Listed
	var amounts []byte
	var at time.Time
	err := row.Scan(&f.FactID, &f.RuleKey, &f.LegalEntityID, &f.ProductID, &f.MovementID, &f.BusinessDate, &f.Currency,
		&amounts, &f.Trigger.Kind, &f.Trigger.FactID, &at)
	if err == nil {
		err = json.Unmarshal(amounts, &f.Amounts)
	}
	f.RecordedAt = canon.Time(at)
	return f, err
}

var factList = list.Spec{Operation: core.OpValuationRead, Alias: "f", IDColumn: "fact_id", IDKind: list.UUID,
	Columns: []list.Column{
		{Field: "recorded_at", Column: "recorded_at", Kind: list.Time, Sortable: true},
		{Field: "legal_entity_id", Column: "legal_entity_id", Kind: list.UUID, Filterable: true},
		{Field: "product_id", Column: "product_id", Kind: list.UUID, Filterable: true},
		{Field: "movement_id", Column: "movement_id", Kind: list.UUID, Filterable: true},
		{Field: "rule_key", Column: "rule_key", Kind: list.Text, Filterable: true}}}

// ListFacts is one page of the cost facts published, newest first.
func ListFacts(m *core.Base, c *httpx.Context) error {
	var f []list.Filter
	for _, k := range []string{"legal_entity_id", "product_id", "movement_id", "rule_key"} {
		if v := c.Query(k); v != "" {
			f = append(f, list.Filter{Field: k, Op: "eq", Values: []string{v}})
		}
	}
	limit, err := list.ParseLimit(c.Query("limit"))
	if err != nil {
		return err
	}
	s, _ := sdkauthz.From(c.Ctx())
	ctx, q, err := list.Build(c.Ctx(), s, factList, list.Request{Filters: f, Limit: limit, Cursor: c.Query("cursor"),
		Sort: []list.Order{{Field: "recorded_at", Desc: true}}})
	if err != nil {
		return err
	}
	out, next, err := list.Page(ctx, m.RT.DB, q, "costing_fact", listCols, scanListed,
		func(f Listed) ([]string, string) { return []string{f.RecordedAt}, f.FactID })
	if err != nil {
		return err
	}
	return c.JSON(200, map[string]any{"facts": out, "next_cursor": next})
}
