package core

import (
	"encoding/json"
	"strconv"
	"strings"

	"github.com/brickKit/be-sdk-go/v3/money"
	apd "github.com/cockroachdb/apd/v3"
	"github.com/google/uuid"
)

// Movement is erp/inventory's movement as this component reads it (erp.inventory_movement.recorded.v1; the read-back
// of StockService.Movements answers the same payload).
type Movement struct {
	FactID        string     `json:"fact_id"`
	Kind          string     `json:"kind"`
	ProductID     string     `json:"product_id"`
	LegalEntityID string     `json:"legal_entity_id"`
	Quantity      string     `json:"quantity"`
	BusinessDate  string     `json:"business_date"`
	Reference     *Reference `json:"reference"`
	ValuationSeq  *Seq       `json:"valuation_seq"`
	// OriginMovementID is a purchase return's receipt movement (RT01).
	OriginMovementID *string `json:"origin_movement_id"`
}

// Reference is a movement's source document.
type Reference struct {
	Kind   string  `json:"kind"`
	ID     string  `json:"id"`
	LineID *string `json:"line_id"`
}

// Seq is a number in a valuation sequence.
type Seq struct {
	Epoch string `json:"epoch"`
	Seq   int64  `json:"seq"`
}

// The movement kinds this version values (VS02); any other stops its scope.
const (
	KindReceipt        = "receipt"
	KindIssue          = "issue"
	KindPurchaseReturn = "purchase_return"
)

// The costing methods (PO01).
const (
	MethodMovingAverage = "moving_average"
	MethodFIFO          = "fifo"
)

// RefPurchaseReceipt is the reference kind of a purchase receipt's movement: its line_id is the receipt line, the
// valuation basis's fact_id (LY01).
const RefPurchaseReceipt = "purchase_receipt"

// ParseMovement reads a movement's payload; ok is false when it is not one this component can place in a sequence.
func ParseMovement(b []byte) (Movement, bool) {
	var m Movement
	if err := json.Unmarshal(b, &m); err != nil {
		return m, false
	}
	ok := UUIDForm.MatchString(m.FactID) && UUIDForm.MatchString(m.ProductID) && UUIDForm.MatchString(m.LegalEntityID) &&
		m.ValuationSeq != nil && m.ValuationSeq.Seq >= 1 && EpochForm(m.ValuationSeq.Epoch) && DateForm.MatchString(m.BusinessDate)
	if ok {
		_, err := money.Parse(m.Quantity)
		ok = err == nil && Cmp(m.Quantity, "0") > 0
	}
	return m, ok
}

// EpochForm: an epoch is "<recovery generation>.<timeline>" (protocol 14).
func EpochForm(e string) bool {
	g, t, ok := strings.Cut(e, ".")
	_, e1 := strconv.ParseUint(g, 10, 64)
	_, e2 := strconv.ParseUint(t, 10, 64)
	return ok && e1 == nil && e2 == nil
}

// CmpEpoch orders two epochs by generation, then timeline: -1, 0 or 1 (VS04 takes only a larger one).
func CmpEpoch(a, b string) int {
	ag, at, _ := strings.Cut(a, ".")
	bg, bt, _ := strings.Cut(b, ".")
	for _, p := range [][2]string{{ag, bg}, {at, bt}} {
		x, _ := strconv.ParseUint(p[0], 10, 64)
		y, _ := strconv.ParseUint(p[1], 10, 64)
		if x != y {
			if x < y {
				return -1
			}
			return 1
		}
	}
	return 0
}

// The triggers a cost fact's identity is worked out from (CF01).
const (
	// FactValued: a movement's valuation (receipt, issue, purchase return)
	FactValued = "valued"
	// FactReplaced: a source replacing a layer's price (with the layer's movement and the source's fact)
	FactReplaced = "replaced"
	// FactReversed: an invoice's contribution taken back (with the layer's movement and the confirmation)
	FactReversed = "reversed"
)

// factNamespace is the namespace of the cost facts' UUIDv5 identities (CF01).
var factNamespace = uuid.MustParse("3f6d2a8e-9b1c-5e47-a2d0-c8e5b7f41a93")

// FactOf is the identity of the cost fact a trigger makes (CF01): the same trigger again is the same fact.
func FactOf(parts ...string) string {
	return uuid.NewSHA1(factNamespace, []byte(strings.Join(parts, ":"))).String()
}

// Mul is a × b, exact.
func Mul(a, b string) string {
	var r apd.Decimal
	_, _ = exact.Mul(&r, dec(a), dec(b))
	return text(&r)
}

// Quo is a ÷ b to 34 significant digits (a unit cost; never rounded to a currency).
func Quo(a, b string) string {
	var r apd.Decimal
	_, _ = apd.BaseContext.WithPrecision(34).Quo(&r, dec(a), dec(b))
	return text(&r)
}

// Amount is a rounded to the currency's places, half away from zero (P5.26).
func Amount(a string, scale int32) string { return money.Format(dec(a), scale) }

// Plain is a decimal as the contract writes a quantity: exact, no trailing zeros.
func Plain(a string) string { return text(dec(a)) }

// Scope is a valuation scope as a valuation works on it, locked in the valuation's transaction (VS02): its progress,
// its stock, and what the step needs from outside (the legal entity's base currency and its places, the method).
type Scope struct {
	ID            string
	LegalEntityID string
	ProductID     string
	Epoch         string
	NextSeq       int64
	State         string
	Quantity      string
	Value         string
	Currency      string
	Scale         int32
	Method        string
}
