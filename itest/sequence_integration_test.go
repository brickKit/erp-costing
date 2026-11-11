//go:build integration

package itest

import (
	"fmt"
	"time"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	"github.com/brickKit/be-sdk-go/v3/ids"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sequence: the movements of a product and legal entity are valued in their valuation sequence, whatever order they
// arrive in; a gap is read back from erp/inventory after the wait; a purchase receipt waits for its valuation basis,
// then takes the policy price (docs/design.md VS01-VS03, LY01, IS01 moving average, CF01).
func (fx *fixture) sequence() {
	t := fx.t
	p := ids.New().String()

	// LY01: a purchase receipt priced by its valuation basis; CF01: its receipt fact
	l1 := fx.basis(fx.le, p, "10", "2026-11-02")
	r1 := fx.movement(fx.le, p, "receipt", "10", "1.1", 1, fx.receiptRef(l1))
	fx.record(r1)
	fx.scopeAt(fx.le, p, 2)
	f := fx.fact(core.FactOf(r1["fact_id"].(string), core.FactValued), core.AccountingFact)
	assert.Equal(t, "inventory_receipt", f["rule_key"])
	assert.Equal(t, map[string]any{"inventory": "100.00", "grni": "100.00"}, f["amounts"])
	assert.Equal(t, map[string]any{"component": core.ID, "document_type": "inventory_movement",
		"document_id": r1["fact_id"], "document_number": nil}, f["source"])
	assert.Equal(t, fx.le, f["legal_entity_id"])
	assert.Equal(t, "CNY", f["currency"])
	assert.Equal(t, "2026-11-02", f["business_date"])
	assert.Equal(t, "2026-11-02", f["posting_date"])
	assert.Nil(t, f["exchange_rate"])
	assert.Nil(t, f["partner_id"])

	// VS02: number 3 (an issue) arrives before number 2; it waits in waiting_gap (VS03) until 2 comes, then both are
	// valued in order: the issue takes the moving average of both receipts (IS01)
	l2 := fx.basis(fx.le, p, "13", "2026-11-03")
	r2 := fx.movement(fx.le, p, "receipt", "10", "1.1", 2, fx.receiptRef(l2))
	i3 := fx.movement(fx.le, p, "issue", "4", "1.1", 3, map[string]any{"kind": "sales_delivery", "id": ids.New().String()})
	fx.record(i3)
	s := fx.waitScope(fx.le, p, func(s map[string]any) bool { return s["state"] == "waiting_gap" })
	assert.EqualValues(t, 2, s["next_seq"], "VS02: nothing after a gap is valued")
	assert.NotNil(t, s["waiting_since"])
	fx.record(r2)
	s = fx.scopeAt(fx.le, p, 4)
	assert.Equal(t, "running", s["state"], "VS03: the gap filled")
	assert.Equal(t, "16", s["quantity"])
	assert.Equal(t, "184.00", s["value"], "100 + 130 - 46")
	assert.Equal(t, "moving_average", s["method"])
	assert.Equal(t, "CNY", s["currency"])
	assert.Equal(t, "1.1", s["epoch"])
	f = fx.fact(core.FactOf(i3["fact_id"].(string), core.FactValued), core.AccountingFact)
	assert.Equal(t, "cost_of_sales", f["rule_key"])
	assert.Equal(t, map[string]any{"cost_of_sales": "46.00", "inventory": "46.00"}, f["amounts"], "IS01: 4/20 of 230")

	// VS01: the same movement again is valued once
	fx.record(i3)
	time.Sleep(time.Second)
	s = fx.scopeAt(fx.le, p, 4)
	assert.Equal(t, "16", s["quantity"])

	// VS03: number 4 is held back by the bus (erp/inventory numbered it); after the gap wait the scope reads it back
	reads := fx.inventory.count()
	i4 := fx.movement(fx.le, p, "issue", "6", "1.1", 4, nil)
	fx.inventory.add(i4)
	i5 := fx.movement(fx.le, p, "issue", "10", "1.1", 5, nil)
	fx.record(i5)
	s = fx.waitScope(fx.le, p, func(s map[string]any) bool { return toInt(s["next_seq"]) == 6 })
	assert.Greater(t, fx.inventory.count(), reads, "VS03: read back from erp/inventory")
	assert.Equal(t, "0", s["quantity"])
	assert.Equal(t, "0.00", s["value"], "IS01: issuing all takes all the value")
	assert.Nil(t, s["unit_cost"])
	f = fx.fact(core.FactOf(i5["fact_id"].(string), core.FactValued), core.AccountingFact)
	assert.Equal(t, map[string]any{"cost_of_sales": "115.00", "inventory": "115.00"}, f["amounts"], "184 - 69")

	// VS03: a gap erp/inventory has not numbered either: each period reads it back and waits another, warned; the wait
	// keeps the time it began (the recovery check erp.costing.waiting reports it after an hour)
	g := ids.New().String()
	g1 := fx.movement(fx.le, g, "receipt", "1", "1.1", 1, nil)
	fx.record(fx.movement(fx.le, g, "receipt", "1", "1.1", 2, nil))
	s = fx.waitScope(fx.le, g, func(s map[string]any) bool { return s["state"] == "waiting_gap" })
	began := s["waiting_since"]
	reads = fx.inventory.count()
	require.Eventually(t, func() bool { return fx.inventory.count() >= reads+2 }, 15*time.Second, 200*time.Millisecond,
		"VS03: two periods read back")
	s = fx.scope(fx.le, g)
	assert.Equal(t, "waiting_gap", s["state"])
	assert.Equal(t, began, s["waiting_since"], "VS03: the wait began when the gap was found")
	fx.inventory.add(g1)
	fx.scopeAt(fx.le, g, 3)

	// LY01: a purchase receipt whose basis has not arrived waits (waiting_basis), and the movements after it with it;
	// the basis arriving values it
	l6 := ids.New().String()
	r6 := fx.movement(fx.le, p, "receipt", "5", "1.1", 6, fx.receiptRef(l6))
	i7 := fx.movement(fx.le, p, "issue", "1", "1.1", 7, nil)
	fx.record(r6)
	fx.record(i7)
	s = fx.waitScope(fx.le, p, func(s map[string]any) bool { return s["state"] == "waiting_basis" })
	assert.EqualValues(t, 6, s["next_seq"])
	fx.publishBasis(fx.le, p, l6, "12", "2026-11-05")
	s = fx.scopeAt(fx.le, p, 8)
	assert.Equal(t, "running", s["state"])
	assert.Equal(t, "4", s["quantity"])
	assert.Equal(t, "48.00", s["value"])
	assert.Equal(t, "12", s["unit_cost"])

	// LY01: a basis that never comes: after the basis wait the receipt takes the policy price (the latest layer's, 12)
	r8 := fx.movement(fx.le, p, "receipt", "2", "1.1", 8, fx.receiptRef(ids.New().String()))
	fx.record(r8)
	s = fx.scopeAt(fx.le, p, 9)
	assert.Equal(t, "6", s["quantity"])
	assert.Equal(t, "72.00", s["value"])
	f = fx.fact(core.FactOf(r8["fact_id"].(string), core.FactValued), core.AccountingFact)
	assert.Equal(t, map[string]any{"inventory": "24.00", "grni": "24.00"}, f["amounts"], "LY01: the policy price")

	// LY01: a receipt from no purchase is priced by the policy at once; a zero amount publishes no fact (VS02)
	q := ids.New().String()
	r1q := fx.movement(fx.le, q, "receipt", "3", "1.1", 1, nil)
	fx.record(r1q)
	s = fx.scopeAt(fx.le, q, 2)
	assert.Equal(t, "3", s["quantity"])
	assert.Equal(t, "0.00", s["value"], "LY01: no price anywhere is 0")
	fx.noFact(core.FactOf(r1q["fact_id"].(string), core.FactValued))

	// LY01, IS01: amounts are rounded to the currency's places, half away from zero; an issue's share of the value
	// too, and the last issue takes what is left
	c := ids.New().String()
	rc := fx.movement(fx.le, c, "receipt", "3", "1.1", 1, fx.receiptRef(fx.basis(fx.le, c, "3.335", "2026-11-02")))
	ic1 := fx.movement(fx.le, c, "issue", "1", "1.1", 2, nil)
	ic2 := fx.movement(fx.le, c, "issue", "2", "1.1", 3, nil)
	for _, mv := range []map[string]any{rc, ic1, ic2} {
		fx.record(mv)
	}
	fx.scopeAt(fx.le, c, 4)
	assert.Equal(t, "10.01", at(fx.fact(core.FactOf(rc["fact_id"].(string), core.FactValued), core.AccountingFact), "amounts", "inventory"))
	assert.Equal(t, "3.34", at(fx.fact(core.FactOf(ic1["fact_id"].(string), core.FactValued), core.AccountingFact), "amounts", "inventory"),
		"IS01: 10.01 / 3")
	assert.Equal(t, "6.67", at(fx.fact(core.FactOf(ic2["fact_id"].(string), core.FactValued), core.AccountingFact), "amounts", "inventory"),
		"IS01: all that is left")

	// VS01: a scope first seen at number 3 (installed after erp/inventory) reads 1 and 2 back
	late := ids.New().String()
	for n := 1; n <= 2; n++ {
		fx.inventory.add(fx.movement(fx.le, late, "receipt", "1", "1.1", n, nil))
	}
	fx.record(fx.movement(fx.le, late, "issue", "2", "1.1", 3, nil))
	s = fx.scopeAt(fx.le, late, 4)
	assert.Equal(t, "0", s["quantity"])

	// the list of scopes, by legal entity and product
	list := fx.get("/api/costing/scopes?legal_entity_id=" + fx.le + "&product_id=" + p)
	require.Len(t, list["scopes"], 1)
	assert.Equal(t, p, at(list, "scopes", 0, "product_id"))
	list = fx.get("/api/costing/scopes?legal_entity_id=" + fx.le + "&state=stopped")
	assert.Empty(t, list["scopes"])
}

// unknownKind: a movement kind this version does not know stops its scope; later movements are kept but not valued
// (VS02, VS05).
func (fx *fixture) unknownKind() {
	t := fx.t
	p := ids.New().String()
	fx.record(fx.movement(fx.le, p, "receipt", "1", "1.1", 1, nil))
	fx.record(fx.movement(fx.le, p, "transfer", "1", "1.1", 2, nil))
	s := fx.waitScope(fx.le, p, func(s map[string]any) bool { return s["state"] == "stopped" })
	assert.Contains(t, s["stopped_reason"], "transfer", "VS02")
	assert.EqualValues(t, 2, s["next_seq"])
	fx.record(fx.movement(fx.le, p, "receipt", "1", "1.1", 3, nil))
	time.Sleep(time.Second)
	s = fx.scope(fx.le, p)
	assert.Equal(t, "stopped", s["state"], "VS05")
	assert.EqualValues(t, 2, s["next_seq"], "VS05: not valued")
	var kept int
	require.NoError(t, fx.admin.QueryRow(fx.ctx, "SELECT count(*) FROM "+itSchema+".movement_in m JOIN "+itSchema+
		".scope s ON s.id = m.scope_id WHERE s.product_id = $1", p).Scan(&kept))
	assert.Equal(t, 2, kept, "VS05: kept")
	list := fx.get("/api/costing/scopes?legal_entity_id=" + fx.le + "&state=stopped")
	require.Len(t, list["scopes"], 1)
}

// epochs: erp/inventory moves to a new epoch; the scope first reads the old epoch to its end, then follows the new
// one from 1; a movement valued in the old epoch and numbered again in the new is not valued twice; a movement of an
// older epoch is dropped (VS04).
func (fx *fixture) epochs() {
	t := fx.t
	p := ids.New().String()
	m1 := fx.movement(fx.le, p, "receipt", "10", "1.1", 1, nil)
	m2 := fx.movement(fx.le, p, "receipt", "10", "1.1", 2, nil)
	fx.record(m1)
	fx.record(m2)
	fx.scopeAt(fx.le, p, 3)
	// number 3 of the old epoch never comes over the bus; the new epoch's first movement does
	m3 := fx.movement(fx.le, p, "issue", "5", "1.1", 3, nil)
	fx.inventory.add(m3)
	n1 := fx.movement(fx.le, p, "issue", "5", "2.1", 1, nil)
	again := fx.movement(fx.le, p, "receipt", "10", "2.1", 2, nil)
	again["fact_id"] = m2["fact_id"] // IC10: valued in 1.1, numbered again in 2.1
	n3 := fx.movement(fx.le, p, "issue", "1", "2.1", 3, nil)
	fx.record(n1)
	fx.record(again)
	fx.record(n3)
	s := fx.waitScope(fx.le, p, func(s map[string]any) bool { return s["epoch"] == "2.1" && toInt(s["next_seq"]) == 4 })
	assert.Equal(t, "running", s["state"])
	assert.Equal(t, "9", s["quantity"], "VS04: 20 - 5 (old 3) - 5 (new 1) - 1 (new 3); the renumbered receipt not again")
	var valued int
	require.NoError(t, fx.admin.QueryRow(fx.ctx, "SELECT count(*) FROM "+itSchema+".valued_movement WHERE fact_id = $1",
		m3["fact_id"]).Scan(&valued))
	assert.Equal(t, 1, valued, "VS04: the old epoch read to its end")
	// an older epoch's movement now is dropped
	old := fx.movement(fx.le, p, "issue", "1", "1.1", 4, nil)
	fx.record(old)
	time.Sleep(time.Second)
	require.NoError(t, fx.admin.QueryRow(fx.ctx, "SELECT count(*) FROM "+itSchema+".movement_in WHERE fact_id = $1",
		old["fact_id"]).Scan(&valued))
	assert.Zero(t, valued, "VS04: an older epoch is dropped")
	assert.Equal(t, "9", fx.scope(fx.le, p)["quantity"])

	// VS04: this component valued more of the old epoch than erp/inventory numbered (it recovered to an earlier
	// point): the scope stops
	q := ids.New().String()
	for n := 1; n <= 3; n++ {
		m := fx.movement(fx.le, q, "receipt", "1", "1.1", n, nil)
		if n < 3 {
			fx.inventory.add(m)
		}
		fx.publishMovement(m)
	}
	fx.scopeAt(fx.le, q, 4)
	fx.record(fx.movement(fx.le, q, "receipt", "1", "2.1", 1, nil))
	s = fx.waitScope(fx.le, q, func(s map[string]any) bool { return s["state"] == "stopped" })
	assert.Contains(t, s["stopped_reason"], "epoch", "VS04")
	assert.Equal(t, "1.1", s["epoch"])
}

// --- helpers of the valuation steps ---

// movement is a movement of erp/inventory numbered seq in epoch.
func (fx *fixture) movement(le, product, kind, qty, epoch string, seq int, ref map[string]any) map[string]any {
	var r any
	if ref != nil {
		r = ref
	}
	return map[string]any{"fact_id": ids.New().String(), "kind": kind, "product_id": product,
		"warehouse_id": fx.warehouse, "legal_entity_id": le, "quantity": qty, "batch": nil, "serials": []string{},
		"business_date": fmt.Sprintf("2026-11-%02d", seq%28+1), "reference": r, "branch_id": nil, "consumption_id": nil,
		"inventory_seq": map[string]any{"epoch": epoch, "seq": seq}, "valuation_seq": map[string]any{"epoch": epoch, "seq": seq},
		"recorded_at": "2026-11-10T08:00:00.000000Z"}
}

// receiptRef is the reference of a purchase receipt's movement to its receipt line.
func (fx *fixture) receiptRef(line string) map[string]any {
	return map[string]any{"kind": "purchase_receipt", "id": ids.New().String(), "revision": 1, "line_id": line}
}

// record numbers a movement in erp/inventory and publishes it, as its forwarder would.
func (fx *fixture) record(m map[string]any) {
	fx.inventory.add(m)
	fx.publishMovement(m)
}

func (fx *fixture) publishMovement(m map[string]any) {
	fx.publish(sourceStreams[0], core.Inventory, core.MovementRecorded, m["fact_id"].(string), m)
}

// basis publishes a valuation basis of a new receipt line and answers the line.
func (fx *fixture) basis(le, product, price, date string) string {
	line := ids.New().String()
	fx.publishBasis(le, product, line, price, date)
	return line
}

func (fx *fixture) publishBasis(le, product, line, price, date string) {
	fx.publish(sourceStreams[1], core.Purchase, core.ValuationBasis, line, map[string]any{"fact_id": line,
		"receipt_id": ids.New().String(), "order_id": ids.New().String(), "order_line_id": ids.New().String(),
		"legal_entity_id": le, "product_id": product, "base_quantity": "1", "unit_price": price, "currency": "CNY",
		"price_basis": "net_of_tax", "business_date": date, "recorded_at": "2026-11-10T08:00:00.000000Z"})
}

func (fx *fixture) scope(le, product string) map[string]any {
	return fx.get("/api/costing/scopes/" + le + "/" + product)
}

// waitScope waits until the scope exists and ok holds.
func (fx *fixture) waitScope(le, product string, ok func(map[string]any) bool) map[string]any {
	fx.t.Helper()
	var s map[string]any
	require.Eventually(fx.t, func() bool {
		code, out := fx.alice.do("GET", "/api/costing/scopes/"+le+"/"+product, nil)
		s = out
		return code == 200 && ok(out)
	}, 15*time.Second, 100*time.Millisecond, "the scope: %v", s)
	return s
}

// scopeAt waits until the scope's next number is next.
func (fx *fixture) scopeAt(le, product string, next int) map[string]any {
	fx.t.Helper()
	return fx.waitScope(le, product, func(s map[string]any) bool { return toInt(s["next_seq"]) == next })
}

// noFact: no fact with the ID is in the outbox.
func (fx *fixture) noFact(id string) {
	var n int
	require.NoError(fx.t, fx.admin.QueryRow(fx.ctx, "SELECT count(*) FROM "+itSchema+".be_outbox WHERE "+
		"convert_from(payload, 'UTF8')::jsonb->>'fact_id' = $1", id).Scan(&n))
	assert.Zero(fx.t, n, id)
}

func toInt(v any) int {
	f, _ := v.(float64)
	return int(f)
}
