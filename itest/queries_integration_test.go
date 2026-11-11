//go:build integration

package itest

import (
	"strconv"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	"github.com/brickKit/be-sdk-go/v3/ids"
	"github.com/brickKit/be-sdk-go/v3/recoverypb"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/status"
)

// queries: the cost layers with their sources, the FIFO valuation's layers in stock, the cost facts with their
// triggers (docs/design.md, 命令与查询); runs in Beijing (FIFO).
func (fx *fixture) queries() {
	t := fx.t
	q := ids.New().String()
	l1 := fx.basis(fx.le2, q, "10", "2026-11-02")
	r1 := fx.movement(fx.le2, q, "receipt", "3", "1.1", 1, fx.receiptRef(l1))
	r2 := fx.movement(fx.le2, q, "receipt", "2", "1.1", 2, fx.receiptRef(fx.basis(fx.le2, q, "12", "2026-11-02")))
	fx.record(r1)
	fx.record(r2)
	fx.scopeAt(fx.le2, q, 3)
	c := fx.confirm(r1, "1", "11", "CNY", "2026-11-07")
	fx.waitFact(core.FactOf(r1["fact_id"].(string), c, core.FactReplaced))
	i3 := fx.movement(fx.le2, q, "issue", "2", "1.1", 3, nil)
	fx.record(i3)
	fx.scopeAt(fx.le2, q, 4)

	// a layer: its units in stock at their own prices, its sources in the order they came
	l := fx.get("/api/costing/layers/" + r1["fact_id"].(string))
	assert.Equal(t, fx.le2, l["legal_entity_id"])
	assert.Equal(t, q, l["product_id"])
	assert.Equal(t, l1, l["receipt_line_id"])
	assert.Equal(t, "3", l["quantity"])
	assert.Equal(t, "2", l["issued_quantity"], "IS01: the first unit at 11, the second at 10")
	assert.Equal(t, "10", l["value_in_stock"])
	assert.Regexp(t, `^10\.333`, l["unit_cost"])
	assert.Equal(t, "invoice", l["source"])
	require.Len(t, l["sources"], 2)
	assert.Equal(t, map[string]any{"kind": "basis", "fact_id": l1, "quantity": "3", "unit_price": "10", "state": "active"},
		without(at(l, "sources", 0).(map[string]any), "arrival"))
	assert.Equal(t, map[string]any{"kind": "invoice", "fact_id": c, "quantity": "1", "unit_price": "11", "state": "active"},
		without(at(l, "sources", 1).(map[string]any), "arrival"))
	assert.Less(t, at(l, "sources", 0, "arrival").(float64), at(l, "sources", 1, "arrival").(float64))
	code, _ := fx.alice.do("GET", "/api/costing/layers/"+ids.New().String(), nil)
	assert.Equal(t, 404, code)
	code, _ = fx.alice.do("GET", "/api/costing/scopes/"+fx.le2+"/"+ids.New().String(), nil)
	assert.Equal(t, 404, code, "a scope not seen yet")

	// the FIFO valuation: its layers in stock, oldest first
	s := fx.scope(fx.le2, q)
	assert.Equal(t, "34.00", s["value"])
	require.Len(t, s["layers_in_stock"], 2)
	assert.Equal(t, r1["fact_id"], at(s, "layers_in_stock", 0, "movement_id"))
	assert.Equal(t, "24", at(s, "layers_in_stock", 1, "value_in_stock"))
	assert.Empty(t, fx.scope(fx.le, fx.anyScope(fx.le))["layers_in_stock"], "moving average: no layers")

	// the layer list by product, source and stock
	list := fx.get("/api/costing/layers?legal_entity_id=" + fx.le2 + "&product_id=" + q)
	assert.Len(t, list["layers"], 2)
	list = fx.get("/api/costing/layers?legal_entity_id=" + fx.le2 + "&product_id=" + q + "&source=invoice")
	require.Len(t, list["layers"], 1)
	assert.Equal(t, r1["fact_id"], at(list, "layers", 0, "movement_id"))
	list = fx.get("/api/costing/layers?legal_entity_id=" + fx.le2 + "&product_id=" + q + "&in_stock=true&limit=1")
	require.Len(t, list["layers"], 1)
	require.NotNil(t, list["next_cursor"])
	list = fx.get("/api/costing/layers?legal_entity_id=" + fx.le2 + "&product_id=" + q + "&in_stock=true&limit=1&cursor=" +
		list["next_cursor"].(string))
	require.Len(t, list["layers"], 1)

	// the cost facts of a movement, with what made each
	facts := fx.get("/api/costing/facts?movement_id=" + r1["fact_id"].(string))
	require.Len(t, facts["facts"], 2)
	byRule := map[string]map[string]any{}
	for _, f := range facts["facts"].([]any) {
		byRule[at(f, "rule_key").(string)] = f.(map[string]any)
	}
	assert.Equal(t, map[string]any{"kind": "movement", "fact_id": r1["fact_id"]}, byRule["inventory_receipt"]["trigger"])
	assert.Equal(t, map[string]any{"inventory": "30.00", "grni": "30.00"}, byRule["inventory_receipt"]["amounts"])
	assert.Equal(t, map[string]any{"kind": "invoice", "fact_id": c}, byRule["cost_adjustment_up"]["trigger"])
	assert.Equal(t, q, byRule["cost_adjustment_up"]["product_id"])
	assert.Equal(t, "2026-11-07", byRule["cost_adjustment_up"]["business_date"])
	assert.Equal(t, "CNY", byRule["cost_adjustment_up"]["currency"])
	facts = fx.get("/api/costing/facts?legal_entity_id=" + fx.le2 + "&product_id=" + q + "&rule_key=cost_of_sales")
	require.Len(t, facts["facts"], 1)
	assert.Equal(t, i3["fact_id"], at(facts, "facts", 0, "movement_id"))
	assert.Equal(t, map[string]any{"cost_of_sales": "21.00", "inventory": "21.00"}, at(facts, "facts", 0, "amounts"))
	facts = fx.get("/api/costing/facts?legal_entity_id=" + fx.le2 + "&rule_key=cost_of_sales_return")
	assert.Len(t, facts["facts"], 1, "IS02's levelling in the fifo step")

	// a layer issued to its end is no longer in stock
	fx.record(fx.movement(fx.le2, q, "issue", "1", "1.1", 4, nil))
	fx.scopeAt(fx.le2, q, 5)
	list = fx.get("/api/costing/layers?legal_entity_id=" + fx.le2 + "&product_id=" + q + "&in_stock=true")
	require.Len(t, list["layers"], 1)
	assert.Equal(t, r2["fact_id"], at(list, "layers", 0, "movement_id"))
	list = fx.get("/api/costing/layers?legal_entity_id=" + fx.le2 + "&product_id=" + q + "&in_stock=false")
	require.Len(t, list["layers"], 1)
	assert.Equal(t, r1["fact_id"], at(list, "layers", 0, "movement_id"))
}

// recoveryChecks: a full recovery asks this member, as the recovery tool only, whether the policies its audit names
// are still there at their versions (be-protocol chapter 14), and runs its checks: the scopes stopped, below zero,
// or waiting more than an hour (docs/design.md, 整套恢复之后).
func (fx *fixture) recoveryChecks() {
	t := fx.t
	dial := func(as string) *grpc.ClientConn {
		conn, err := grpc.NewClient(itSystem, grpc.WithTransportCredentials(credentials.NewTLS(fx.w.TLS(t, as).ClientConfig(core.ID))))
		require.NoError(t, err)
		t.Cleanup(func() { conn.Close() })
		return conn
	}
	_, err := recoverypb.NewCheckServiceClient(dial(core.Finance)).Check(fx.ctx, &recoverypb.CheckRequest{Recovery: "r"})
	assert.Equal(t, codes.PermissionDenied, status.Code(err), "the recovery tool only: %v", err)
	cs := recoverypb.NewCheckServiceClient(dial("ops/recover"))

	var version int64
	require.NoError(t, fx.admin.QueryRow(fx.ctx, "SELECT version FROM "+itSchema+".policy WHERE legal_entity_id = $1", fx.le2).Scan(&version))
	present, err := cs.Present(fx.ctx, &recoverypb.PresentRequest{Recovery: "r", Objects: []*recoverypb.ObjectRef{
		{Type: core.Policies, Id: fx.le2, Version: strconv.FormatInt(version, 10)},
		{Type: core.Policies, Id: fx.le2, Version: strconv.FormatInt(version+1, 10)},
		{Type: core.Policies, Id: ids.New().String(), Version: "1"}}})
	require.NoError(t, err)
	var answers []string
	for _, a := range present.GetAnswers() {
		answers = append(answers, a.GetAnswer())
	}
	assert.Equal(t, []string{"present", "older", "absent"}, answers)

	// a scope below zero and one waiting for two hours
	neg := ids.New().String()
	fx.record(fx.movement(fx.le, neg, "issue", "1", "1.1", 1, nil))
	fx.scopeAt(fx.le, neg, 2)
	wait := ids.New().String()
	fx.record(fx.movement(fx.le, wait, "receipt", "1", "1.1", 1, nil))
	fx.scopeAt(fx.le, wait, 2)
	_, err = fx.admin.Exec(fx.ctx, "UPDATE "+itSchema+".scope SET state = 'waiting_gap', waiting_since = now() - interval '2 hours' "+
		"WHERE product_id = $1", wait)
	require.NoError(t, err)

	res, err := cs.Check(fx.ctx, &recoverypb.CheckRequest{Recovery: "r"})
	require.NoError(t, err)
	results := map[string]*recoverypb.CheckResult{}
	for _, r := range res.GetResults() {
		results[r.GetCheck()] = r
	}
	for name, want := range map[string]int{"erp.costing.stopped_scopes": 2, "erp.costing.negative_stock": 1, "erp.costing.waiting": 1} {
		r := results[name]
		require.NotNil(t, r, "%s is registered", name)
		assert.Equal(t, "diff", r.GetOutcome(), name)
		assert.Len(t, r.GetDifferences(), want, "%s: %v", name, r.GetDifferences())
		assert.NotEmpty(t, r.GetDisposal(), name)
	}
	assert.Equal(t, fx.scopeID(fx.le, neg), results["erp.costing.negative_stock"].GetDifferences()[0].GetObjectId())
	assert.Equal(t, fx.scopeID(fx.le, wait), results["erp.costing.waiting"].GetDifferences()[0].GetObjectId())
	assert.Contains(t, results["erp.costing.stopped_scopes"].GetDifferences()[0].GetDetail(), "not valued by this version")
}

// without is a copy of m without the key.
func without(m map[string]any, key string) map[string]any {
	out := map[string]any{}
	for k, v := range m {
		if k != key {
			out[k] = v
		}
	}
	return out
}

// anyScope is a product of a valued scope of the legal entity.
func (fx *fixture) anyScope(le string) string {
	var p string
	require.NoError(fx.t, fx.admin.QueryRow(fx.ctx, "SELECT product_id::text FROM "+itSchema+".scope WHERE legal_entity_id = $1 "+
		"AND state = 'running' LIMIT 1", le).Scan(&p))
	return p
}

func (fx *fixture) scopeID(le, product string) string {
	var id string
	require.NoError(fx.t, fx.admin.QueryRow(fx.ctx, "SELECT id::text FROM "+itSchema+".scope WHERE legal_entity_id = $1 AND "+
		"product_id = $2", le, product).Scan(&id))
	return id
}
