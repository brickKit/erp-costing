//go:build integration

package itest

import (
	"time"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	"github.com/brickKit/be-sdk-go/v3/ids"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// sources: a layer's price replaced by a higher source, each replacement an adjustment that splits into the
// inventory value and the variance (docs/design.md LY02-LY05, AD01, AD02 moving average); runs in Shanghai (moving
// average).
func (fx *fixture) sources() {
	t := fx.t

	// LY02: a receipt priced by the policy (its basis late) takes the basis when it comes: the whole layer, the
	// difference split by the stock left (AD02 moving average: 5 of 10 in stock, half into the value)
	a := ids.New().String()
	r0 := fx.movement(fx.le, a, "receipt", "10", "1.1", 1, fx.receiptRef(fx.basis(fx.le, a, "10", "2026-11-02")))
	l1 := ids.New().String()
	r1 := fx.movement(fx.le, a, "receipt", "10", "1.1", 2, fx.receiptRef(l1))
	i2 := fx.movement(fx.le, a, "issue", "15", "1.1", 3, nil)
	for _, mv := range []map[string]any{r0, r1, i2} {
		fx.record(mv)
	}
	s := fx.scopeAt(fx.le, a, 4)
	assert.Equal(t, "50.00", s["value"], "r1 at the policy price 10")
	fx.publishBasis(fx.le, a, l1, "12", "2026-11-06")
	adj := fx.waitFact(core.FactOf(r1["fact_id"].(string), l1, core.FactReplaced))
	assert.Equal(t, "cost_adjustment_up", adj["rule_key"])
	assert.Equal(t, map[string]any{"inventory": "10.00", "variance": "10.00", "grni": "20.00"}, adj["amounts"], "AD01, AD02")
	assert.Equal(t, "2026-11-06", adj["business_date"], "AD01: the basis's date")
	assert.Equal(t, r1["fact_id"], at(adj, "source", "document_id"))
	assert.Nil(t, adj["partner_id"])
	assert.Equal(t, "60.00", fx.scope(fx.le, a)["value"])
	fx.publishBasis(fx.le, a, l1, "12", "2026-11-06") // LY02: the same basis again changes nothing
	time.Sleep(time.Second)
	assert.Equal(t, "60.00", fx.scope(fx.le, a)["value"])

	// LY02: a basis in another currency is converted at the spot rate of its date (USD 1.5 × 7.2)
	f := ids.New().String()
	lf := ids.New().String()
	fx.publishBasisIn(fx.le, f, lf, "1.5", "USD", "2026-11-02")
	rf := fx.movement(fx.le, f, "receipt", "2", "1.1", 1, fx.receiptRef(lf))
	fx.record(rf)
	fx.scopeAt(fx.le, f, 2)
	assert.Equal(t, "21.60", at(fx.fact(core.FactOf(rf["fact_id"].(string), core.FactValued), core.AccountingFact), "amounts", "inventory"))

	// LY03: invoices replace the price of the quantity they match, one after another (IC19); the supplier is the
	// partner and the invoice date the date; in another currency at its rate
	b := ids.New().String()
	rb := fx.movement(fx.le, b, "receipt", "10", "1.1", 1, fx.receiptRef(fx.basis(fx.le, b, "10", "2026-11-02")))
	fx.record(rb)
	fx.scopeAt(fx.le, b, 2)
	c1 := fx.confirm(rb, "4", "11", "CNY", "2026-11-07")
	adj = fx.waitFact(core.FactOf(rb["fact_id"].(string), c1, core.FactReplaced))
	assert.Equal(t, map[string]any{"inventory": "4.00", "grni": "4.00"}, adj["amounts"], "AD02: all of it in stock")
	assert.Equal(t, fx.supplier, adj["partner_id"])
	assert.Equal(t, "2026-11-07", adj["business_date"], "AD01: the invoice's date (IC29)")
	c2 := fx.confirm(rb, "3", "1.5", "USD", "2026-11-07")
	adj = fx.waitFact(core.FactOf(rb["fact_id"].(string), c2, core.FactReplaced))
	assert.Equal(t, map[string]any{"inventory": "2.40", "grni": "2.40"}, adj["amounts"], "LY03: (10.8 - 10) × 3")
	assert.Equal(t, "106.40", fx.scope(fx.le, b)["value"])

	// LY04, AD02: the first invoice reversed after all is issued: its 4 units go back to the basis's 10, all of it a
	// variance (nothing in stock)
	ib := fx.movement(fx.le, b, "issue", "10", "1.1", 2, nil)
	fx.record(ib)
	fx.scopeAt(fx.le, b, 3)
	fx.reverse(c1, "2026-11-08")
	adj = fx.waitFact(core.FactOf(rb["fact_id"].(string), c1, core.FactReversed))
	assert.Equal(t, "cost_adjustment_down", adj["rule_key"])
	assert.Equal(t, map[string]any{"variance": "4.00", "grni": "4.00"}, adj["amounts"])
	assert.Equal(t, "2026-11-08", adj["business_date"], "AD01: the reversal's date")
	assert.Equal(t, "0.00", fx.scope(fx.le, b)["value"])

	// LY04: a reversal before its confirmation: the confirmation does nothing
	c3 := ids.New().String()
	fx.reverse(c3, "2026-11-08")
	time.Sleep(500 * time.Millisecond)
	fx.confirmAs(c3, rb, "1", "20", "CNY", "2026-11-07")
	time.Sleep(1500 * time.Millisecond)
	fx.noFact(core.FactOf(rb["fact_id"].(string), c3, core.FactReplaced))

	// LY03: a confirmation of a movement not valued yet is kept; the layer enters at the invoice's price, no
	// adjustment
	r4 := fx.movement(fx.le, b, "receipt", "5", "1.1", 3, fx.receiptRef(fx.basis(fx.le, b, "10", "2026-11-02")))
	c4 := fx.confirm(r4, "5", "9", "CNY", "2026-11-07")
	time.Sleep(time.Second)
	fx.record(r4)
	fx.scopeAt(fx.le, b, 4)
	assert.Equal(t, "45.00", at(fx.fact(core.FactOf(r4["fact_id"].(string), core.FactValued), core.AccountingFact), "amounts", "inventory"))
	fx.noFact(core.FactOf(r4["fact_id"].(string), c4, core.FactReplaced))

	// LY02: a basis after an invoice: the invoiced units keep the invoice's price (IC14), the others take the basis
	d := ids.New().String()
	fx.record(fx.movement(fx.le, d, "receipt", "1", "1.1", 1, fx.receiptRef(fx.basis(fx.le, d, "10", "2026-11-02"))))
	ld := ids.New().String()
	rd := fx.movement(fx.le, d, "receipt", "4", "1.1", 2, fx.receiptRef(ld))
	fx.record(rd)
	fx.scopeAt(fx.le, d, 3) // after the basis wait, at the policy price 10
	cd := fx.confirm(rd, "2", "11", "CNY", "2026-11-07")
	fx.waitFact(core.FactOf(rd["fact_id"].(string), cd, core.FactReplaced))
	fx.publishBasis(fx.le, d, ld, "12", "2026-11-06")
	adj = fx.waitFact(core.FactOf(rd["fact_id"].(string), ld, core.FactReplaced))
	assert.Equal(t, map[string]any{"inventory": "4.00", "grni": "4.00"}, adj["amounts"], "LY02: 2 units not invoiced, 10 to 12")
	assert.Equal(t, "56.00", fx.scope(fx.le, d)["value"], "10 + 2 × 11 + 2 × 12")

	// LY05: a withdrawn basis is not used for a new layer; a layer that used it keeps it, warned
	w := ids.New().String()
	lw := fx.basis(fx.le, w, "10", "2026-11-02")
	fx.record(fx.movement(fx.le, w, "receipt", "2", "1.1", 1, fx.receiptRef(lw)))
	fx.scopeAt(fx.le, w, 2)
	fx.withdraw(lw)
	l5 := fx.basis(fx.le, w, "99", "2026-11-02")
	fx.withdraw(l5)
	time.Sleep(time.Second)
	r5 := fx.movement(fx.le, w, "receipt", "1", "1.1", 2, fx.receiptRef(l5))
	fx.record(r5)
	s = fx.scopeAt(fx.le, w, 3)
	assert.Equal(t, "30.00", s["value"], "LY05: the first layer keeps 10; the second waits, then the policy's 10")
	var withdrawn int
	require.NoError(t, fx.admin.QueryRow(fx.ctx, "SELECT count(*) FROM "+itSchema+".basis_withdrawal WHERE receipt_line_id = ANY($1)",
		[]string{lw, l5}).Scan(&withdrawn))
	assert.Equal(t, 2, withdrawn)
}

// --- helpers of the sources ---

func (fx *fixture) publishBasisIn(le, product, line, price, currency, date string) {
	fx.publish(sourceStreams[1], core.Purchase, core.ValuationBasis, line, map[string]any{"fact_id": line,
		"receipt_id": ids.New().String(), "order_id": ids.New().String(), "order_line_id": ids.New().String(),
		"legal_entity_id": le, "product_id": product, "base_quantity": "1", "unit_price": price, "currency": currency,
		"price_basis": "net_of_tax", "business_date": date, "recorded_at": "2026-11-10T08:00:00.000000Z"})
}

// withdrawal is erp/purchase's withdrawal of a basis; its identity is worked out from the basis (purchase 3.0.1).
func (fx *fixture) withdraw(line string) {
	id := uuid.NewSHA1(uuid.MustParse("6c1b2e9a-5f0d-5a8e-9c3b-7e2f4a1d8b60"), []byte(line)).String()
	fx.publish(sourceStreams[1], core.Purchase, core.BasisWithdrawn, id, map[string]any{"fact_id": id, "basis_fact_id": line,
		"receipt_id": ids.New().String(), "reason": "the receipt failed", "recorded_at": "2026-11-10T08:00:00.000000Z"})
}

// confirm publishes erp/finance's price confirmation of qty of the receipt movement and answers its fact ID.
func (fx *fixture) confirm(receipt map[string]any, qty, price, currency, date string) string {
	id := ids.New().String()
	fx.confirmAs(id, receipt, qty, price, currency, date)
	return id
}

func (fx *fixture) confirmAs(id string, receipt map[string]any, qty, price, currency, date string) {
	ref := receipt["reference"].(map[string]any)
	fx.publish(sourceStreams[2], core.Finance, core.PriceConfirmed, id, map[string]any{"fact_id": id,
		"invoice_id": ids.New().String(), "legal_entity_id": receipt["legal_entity_id"], "supplier_id": fx.supplier,
		"order_id": ref["id"], "order_line_id": ids.New().String(), "movements": []any{map[string]any{
			"movement_id": receipt["fact_id"], "receipt_line_id": ref["line_id"], "base_quantity": qty}},
		"unit_price": price, "currency": currency, "price_basis": "net_of_tax", "business_date": date,
		"recorded_at": "2026-11-10T08:00:00.000000Z"})
}

// reverse publishes erp/finance's reversal of a confirmation.
func (fx *fixture) reverse(confirmed, date string) {
	id := ids.New().String()
	fx.publish(sourceStreams[2], core.Finance, core.PriceReversed, id, map[string]any{"fact_id": id,
		"confirmed_fact_id": confirmed, "invoice_id": ids.New().String(), "reason": "corrected", "business_date": date,
		"recorded_at": "2026-11-10T08:00:00.000000Z"})
}

// waitFact waits for the cost fact with the ID and answers it.
func (fx *fixture) waitFact(id string) map[string]any {
	fx.t.Helper()
	require.Eventually(fx.t, func() bool {
		var n int
		_ = fx.admin.QueryRow(fx.ctx, "SELECT count(*) FROM "+itSchema+".be_outbox WHERE "+
			"convert_from(payload, 'UTF8')::jsonb->>'fact_id' = $1", id).Scan(&n)
		return n == 1
	}, 15*time.Second, 100*time.Millisecond, "fact %s", id)
	return fx.fact(id, core.AccountingFact)
}
