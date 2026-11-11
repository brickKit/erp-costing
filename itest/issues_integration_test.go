//go:build integration

package itest

import (
	"context"
	"time"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	"github.com/brickKit/be-sdk-go/v3/ids"
	costingv3 "github.com/brickKit/erp-costing/contract/v3/gen/go/be/erp/costing/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
)

// fifo: Beijing values by FIFO: issues take the oldest units at their own prices (IS01), an invoice's adjustment
// splits by the units already gone (AD02, IC24), a purchase return takes its layer's next units and is limited to them
// (RT01, IC30).
func (fx *fixture) fifo() {
	t := fx.t
	p := fx.get("/api/costing/policies/" + fx.le2)
	code, out := fx.alice.do("PUT", "/api/costing/policies/"+fx.le2, map[string]any{"method": "fifo", "version": p["version"]})
	require.Equal(t, 200, code, out)

	a := ids.New().String()
	r1 := fx.movement(fx.le2, a, "receipt", "10", "1.1", 1, fx.receiptRef(fx.basis(fx.le2, a, "10", "2026-11-02")))
	r2 := fx.movement(fx.le2, a, "receipt", "10", "1.1", 2, fx.receiptRef(fx.basis(fx.le2, a, "12", "2026-11-02")))
	i3 := fx.movement(fx.le2, a, "issue", "12", "1.1", 3, nil)
	for _, mv := range []map[string]any{r1, r2, i3} {
		fx.record(mv)
	}
	s := fx.scopeAt(fx.le2, a, 4)
	assert.Equal(t, "fifo", s["method"])
	assert.Equal(t, "8", s["quantity"])
	assert.Equal(t, "96.00", s["value"])
	assert.Equal(t, map[string]any{"cost_of_sales": "124.00", "inventory": "124.00"},
		fx.fact(core.FactOf(i3["fact_id"].(string), core.FactValued), core.AccountingFact)["amounts"], "IS01: 10 × 10 + 2 × 12")

	// AD02 (IC24): the first layer is all gone, its invoice is all a variance; the second has 2 of 10 gone: of an
	// invoice on its first 4 units, 2 are gone (variance) and 2 in stock (into the value)
	ca := fx.confirm(r1, "6", "12", "CNY", "2026-11-07")
	adj := fx.waitFact(core.FactOf(r1["fact_id"].(string), ca, core.FactReplaced))
	assert.Equal(t, map[string]any{"variance": "12.00", "grni": "12.00"}, adj["amounts"])
	cb := fx.confirm(r2, "4", "13", "CNY", "2026-11-07")
	adj = fx.waitFact(core.FactOf(r2["fact_id"].(string), cb, core.FactReplaced))
	assert.Equal(t, map[string]any{"inventory": "2.00", "variance": "2.00", "grni": "4.00"}, adj["amounts"])
	assert.Equal(t, "98.00", fx.scope(fx.le2, a)["value"])

	// IS01: the next issue takes units 3 to 5 of the second layer: two at the invoice's 13, one at the basis's 12
	i4 := fx.movement(fx.le2, a, "issue", "3", "1.1", 4, nil)
	fx.record(i4)
	s = fx.scopeAt(fx.le2, a, 5)
	assert.Equal(t, "60.00", s["value"])
	assert.Equal(t, "38.00", at(fx.fact(core.FactOf(i4["fact_id"].(string), core.FactValued), core.AccountingFact), "amounts", "inventory"))

	// RT01: erp/purchase asks the limit (the layer's units in stock), then returns 4 of them at their prices
	lim := fx.returnLimit(r2["fact_id"].(string))
	assert.True(t, lim.GetKnown())
	assert.True(t, lim.GetLimited(), "RT01: FIFO limits")
	assert.Equal(t, "5", lim.GetQuantity())
	rt := fx.movement(fx.le2, a, "purchase_return", "4", "1.1", 5, map[string]any{"kind": "purchase_return", "id": ids.New().String()})
	rt["origin_movement_id"] = r2["fact_id"]
	fx.record(rt)
	s = fx.scopeAt(fx.le2, a, 6)
	assert.Equal(t, "1", s["quantity"])
	assert.Equal(t, "12.00", s["value"])
	f := fx.fact(core.FactOf(rt["fact_id"].(string), core.FactValued), core.AccountingFact)
	assert.Equal(t, "inventory_return", f["rule_key"])
	assert.Equal(t, map[string]any{"grni": "48.00", "inventory": "48.00"}, f["amounts"])
	assert.Equal(t, "1", fx.returnLimit(r2["fact_id"].(string)).GetQuantity())
	assert.False(t, fx.returnLimit(ids.New().String()).GetKnown(), "RT01: a receipt not valued is not known")

	// IS01: the last unit is the stock's whole value
	i6 := fx.movement(fx.le2, a, "issue", "1", "1.1", 6, nil)
	fx.record(i6)
	s = fx.scopeAt(fx.le2, a, 7)
	assert.Equal(t, "0.00", s["value"])

	// IS02: past the stock, the rest at the policy price (the latest layer's unit cost, (4 × 13 + 6 × 12) ÷ 10),
	// the quantity below zero
	i7 := fx.movement(fx.le2, a, "issue", "2", "1.1", 7, nil)
	fx.record(i7)
	s = fx.scopeAt(fx.le2, a, 8)
	assert.Equal(t, "-2", s["quantity"])
	assert.Equal(t, "-24.80", s["value"])

	// IS02: the next receipt levels the stock first: its first 2 units are the ones issued at 12.40, their cost is 11;
	// the difference corrects the cost of sales (inventory debit, cost of sales credit)
	r8 := fx.movement(fx.le2, a, "receipt", "5", "1.1", 8, fx.receiptRef(fx.basis(fx.le2, a, "11", "2026-11-09")))
	fx.record(r8)
	s = fx.scopeAt(fx.le2, a, 9)
	assert.Equal(t, "3", s["quantity"])
	assert.Equal(t, "33.00", s["value"], "IS02: 3 units at 11")
	f = fx.fact(core.FactOf(r8["fact_id"].(string), core.FactLevelled), core.AccountingFact)
	assert.Equal(t, "cost_of_sales_return", f["rule_key"])
	assert.Equal(t, map[string]any{"inventory": "2.80", "cost_of_sales": "2.80"}, f["amounts"])
	assert.Equal(t, "2026-11-09", f["business_date"])
	i9 := fx.movement(fx.le2, a, "issue", "3", "1.1", 9, nil)
	fx.record(i9)
	assert.Equal(t, "33.00", at(fx.waitFact(core.FactOf(i9["fact_id"].(string), core.FactValued)), "amounts", "inventory"),
		"IS01: the levelled units are gone")
}

// returns: moving average: a purchase return takes its layer's unit cost, invoices included; nothing limits it (RT01).
func (fx *fixture) returns() {
	t := fx.t
	p := ids.New().String()
	r := fx.movement(fx.le, p, "receipt", "5", "1.1", 1, fx.receiptRef(fx.basis(fx.le, p, "10", "2026-11-02")))
	fx.record(r)
	fx.scopeAt(fx.le, p, 2)
	c := fx.confirm(r, "2", "15", "CNY", "2026-11-07")
	fx.waitFact(core.FactOf(r["fact_id"].(string), c, core.FactReplaced))
	lim := fx.returnLimit(r["fact_id"].(string))
	assert.True(t, lim.GetKnown())
	assert.False(t, lim.GetLimited(), "RT01: moving average does not limit")
	rt := fx.movement(fx.le, p, "purchase_return", "1", "1.1", 2, nil)
	rt["origin_movement_id"] = r["fact_id"]
	fx.record(rt)
	s := fx.scopeAt(fx.le, p, 3)
	assert.Equal(t, "4", s["quantity"])
	assert.Equal(t, "48.00", s["value"], "60 - (2 × 15 + 3 × 10) ÷ 5")
	assert.Equal(t, map[string]any{"grni": "12.00", "inventory": "12.00"},
		fx.fact(core.FactOf(rt["fact_id"].(string), core.FactValued), core.AccountingFact)["amounts"])

	// IS02 (moving average): an issue with nothing valued takes the policy price 0; the receipt that levels it costs
	// 20 more (cost of sales debit)
	q := ids.New().String()
	fx.record(fx.movement(fx.le, q, "issue", "2", "1.1", 1, nil))
	rq := fx.movement(fx.le, q, "receipt", "2", "1.1", 2, fx.receiptRef(fx.basis(fx.le, q, "10", "2026-11-09")))
	fx.record(rq)
	s = fx.scopeAt(fx.le, q, 3)
	assert.Equal(t, "0", s["quantity"])
	assert.Equal(t, "0.00", s["value"])
	f := fx.fact(core.FactOf(rq["fact_id"].(string), core.FactLevelled), core.AccountingFact)
	assert.Equal(t, "cost_of_sales", f["rule_key"])
	assert.Equal(t, map[string]any{"cost_of_sales": "20.00", "inventory": "20.00"}, f["amounts"])

	// only erp/purchase asks
	_, err := fx.layerClient(core.Finance).ReturnLimit(fx.ctx, &costingv3.ReturnLimitRequest{OriginMovementId: r["fact_id"].(string)})
	assert.Error(t, err)
}

func (fx *fixture) layerClient(as string) costingv3.LayerServiceClient {
	conn, err := grpc.NewClient(itSystem, grpc.WithTransportCredentials(credentials.NewTLS(fx.w.TLS(fx.t, as).ClientConfig(core.ID))))
	require.NoError(fx.t, err)
	fx.t.Cleanup(func() { conn.Close() })
	return costingv3.NewLayerServiceClient(conn)
}

func (fx *fixture) returnLimit(origin string) *costingv3.ReturnLimitResponse {
	fx.t.Helper()
	ctx, cancel := context.WithTimeout(fx.ctx, 5*time.Second)
	defer cancel()
	out, err := fx.layerClient(core.Purchase).ReturnLimit(ctx, &costingv3.ReturnLimitRequest{OriginMovementId: origin})
	require.NoError(fx.t, err)
	return out
}
