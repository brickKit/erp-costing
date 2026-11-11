package core

import (
	sdkauthz "github.com/brickKit/be-sdk-go/v3/authz"
)

// ID is the component's ID.
const ID = "erp/costing"

// Dependencies (the IDs runtime.GRPC takes).
const (
	Org      = "mdm/org"
	Currency = "mdm/currency"
	// Inventory numbers the movements and answers a gap in a valuation sequence (StockService.Movements, VS03).
	Inventory = "erp/inventory"
	// Purchase publishes the receipts' valuation bases; it calls LayerService.ReturnLimit before a purchase return.
	Purchase = "erp/purchase"
	// Finance publishes the price confirmations and posts the cost facts; optional.
	Finance = "erp/finance"
)

// The stream and its event type (contract/events), and the streams and event types subscribed to.
const (
	Stream         = "erp.costing"
	AccountingFact = "erp.costing.accounting_fact.v1"

	InventoryStream  = "erp.inventory"
	MovementRecorded = "erp.inventory_movement.recorded.v1"
	PurchaseStream   = "erp.purchase"
	ValuationBasis   = "erp.purchase_receipt.valuation_basis.v1"
	BasisWithdrawn   = "erp.purchase_receipt.valuation_basis_withdrawn.v1"
	FinanceStream    = "erp.finance"
	PriceConfirmed   = "erp.supplier_invoice.price_confirmed.v1"
	PriceReversed    = "erp.supplier_invoice.price_reversed.v1"
)

// Resources and operations (contract/openapi.yaml names each route's operation).
const (
	Policies        = "erp.costing.policy"
	OpPolicyRead    = "erp.costing.policy.read"
	OpPolicyManage  = "erp.costing.policy.manage"
	Valuation       = "erp.costing.valuation"
	OpValuationRead = "erp.costing.valuation.read"
	System          = "erp.costing.system"
	OpSysRead       = "erp.costing.system.read"
	// OpResend is the redelivery of this component's stream (be-protocol P6.38).
	OpResend = "erp.costing.system.resend"
	// OpRecover is a full recovery's checks (be-protocol chapter 14: the recovery tool only).
	OpRecover = "erp.costing.system.recover"
)

// DimLegalEntity is the one dimension (security 07): every resource carries the legal entity.
const DimLegalEntity = "legal_entity"

// Parameters (protocol 05, protocol 14).
const (
	MaxReadIDs = 500
	// MaxRefetch is the most movements one read from erp/inventory answers (StockService.Movements caps it at 1000).
	MaxRefetch = 1000
)

func fields(names ...string) map[string]sdkauthz.Field {
	out := map[string]sdkauthz.Field{}
	for _, n := range names {
		out[n] = sdkauthz.Field{Class: sdkauthz.Ordinary}
	}
	return out
}

// PolicyFields are a costing policy's fields.
var PolicyFields = []string{"legal_entity_id", "method", "version", "created_at", "updated_at"}

// ValuationFields are what the lists of valuation scopes, cost layers and cost facts may filter and sort on (one
// resource, one read operation: the union of the three).
var ValuationFields = []string{"legal_entity_id", "product_id", "state", "epoch", "next_seq", "updated_at",
	"movement_id", "business_date", "source", "in_stock", "fact_id", "rule_key", "recorded_at"}

func entity() map[string]sdkauthz.Dimension {
	return map[string]sdkauthz.Dimension{DimLegalEntity: {Kind: sdkauthz.ValueSet, Column: "legal_entity_id"}}
}

// Definitions are the component's resources and operations.
func Definitions() *sdkauthz.Definitions {
	return &sdkauthz.Definitions{Component: ID,
		Resources: []sdkauthz.Resource{
			{Type: Policies, ViewOperation: OpPolicyRead, Dimensions: entity(), Fields: fields(PolicyFields...)},
			{Type: Valuation, ViewOperation: OpValuationRead, Dimensions: entity(), Fields: fields(ValuationFields...)},
			{Type: System, ViewOperation: OpSysRead, Dimensions: map[string]sdkauthz.Dimension{},
				Fields: map[string]sdkauthz.Field{}},
		},
		Operations: []sdkauthz.Operation{
			{Key: OpPolicyRead, ResourceType: Policies, Purpose: "read", RiskClass: "low"},
			{Key: OpPolicyManage, ResourceType: Policies, Purpose: "manage", RiskClass: "high", Audit: true,
				SideEffects: []string{"business_write"}},
			{Key: OpValuationRead, ResourceType: Valuation, Purpose: "read", RiskClass: "low"},
			{Key: OpSysRead, ResourceType: System, Purpose: "read", RiskClass: "low"},
			{Key: OpResend, ResourceType: System, Purpose: "read", RiskClass: "low"},
			{Key: OpRecover, ResourceType: System, Purpose: "read", RiskClass: "low"},
		}}
}
