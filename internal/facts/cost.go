package facts

import (
	"context"
	"time"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	"github.com/brickKit/be-sdk-go/v3/canon"
	"github.com/brickKit/be-sdk-go/v3/db"
)

// Source is the document a fact comes from (erp/finance's shared contract).
type Source struct {
	Component      string  `json:"component"`
	DocumentType   string  `json:"document_type"`
	DocumentID     string  `json:"document_id"`
	DocumentNumber *string `json:"document_number"`
}

// Dimensions are the analysis dimensions a fact carries: a movement has none in the first batch (CF01).
type Dimensions struct {
	DepartmentID *string `json:"department_id"`
	ProjectID    *string `json:"project_id"`
}

// Accounting is a cost fact in erp/finance's shared contract (accounting_fact.v1).
type Accounting struct {
	FactID        string            `json:"fact_id"`
	Source        Source            `json:"source"`
	LegalEntityID string            `json:"legal_entity_id"`
	BusinessDate  string            `json:"business_date"`
	PostingDate   string            `json:"posting_date"`
	RuleKey       string            `json:"rule_key"`
	Currency      string            `json:"currency"`
	ExchangeRate  *struct{}         `json:"exchange_rate"`
	Amounts       map[string]string `json:"amounts"`
	PartnerID     *string           `json:"partner_id"`
	Dimensions    Dimensions        `json:"dimensions"`
	ServicePeriod *struct{}         `json:"service_period"`
	RecordedAt    string            `json:"recorded_at"`
}

// The rule keys of erp/finance's catalog this component posts under, and their roles.
const (
	RuleReceipt     = "inventory_receipt"    // inventory debit, grni credit
	RuleCostOfSales = "cost_of_sales"        // cost_of_sales debit, inventory credit
	RuleReturn      = "inventory_return"     // grni debit, inventory credit
	RuleCostReturn  = "cost_of_sales_return" // inventory debit, cost_of_sales credit
	RuleAdjustUp    = "cost_adjustment_up"   // inventory and variance debit, grni credit
	RuleAdjustDown  = "cost_adjustment_down" // grni debit, inventory and variance credit
	RoleVariance    = "variance"
	RoleInventory   = "inventory"
	RoleGRNI        = "grni"
	RoleCostOfSales = "cost_of_sales"
)

// Cost is one cost fact of a valuation (CF01).
type Cost struct {
	FactID        string // core.FactOf of its trigger
	LegalEntityID string
	MovementID    string // the source document: the movement (an adjustment: its layer's receipt movement)
	Date          string // business and posting date
	Rule          string
	Currency      string // the legal entity's base currency
	Amounts       map[string]string
	PartnerID     *string
}

// Publish publishes a cost fact in the valuation's transaction (CF01); a role of zero is left out, and a fact whose
// amounts are all zero is not published (VS02).
func Publish(m *core.Base, ctx context.Context, tx db.Tx, c Cost) error {
	amounts := map[string]string{}
	for role, a := range c.Amounts {
		if core.Cmp(a, "0") != 0 {
			amounts[role] = a
		}
	}
	if len(amounts) == 0 {
		return nil
	}
	f := Accounting{FactID: c.FactID, Source: Source{Component: core.ID, DocumentType: "inventory_movement",
		DocumentID: c.MovementID}, LegalEntityID: c.LegalEntityID, BusinessDate: c.Date, PostingDate: c.Date,
		RuleKey: c.Rule, Currency: c.Currency, Amounts: amounts, PartnerID: c.PartnerID, RecordedAt: canon.Time(time.Now())}
	return core.Fact(m, ctx, tx, core.AccountingFact, c.FactID, c.LegalEntityID, f)
}
