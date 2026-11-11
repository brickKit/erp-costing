package costing

import (
	"context"

	core "github.com/brickKit/erp-costing/v3/internal/core"
	policies "github.com/brickKit/erp-costing/v3/internal/policies"
	sequence "github.com/brickKit/erp-costing/v3/internal/sequence"
	sources "github.com/brickKit/erp-costing/v3/internal/sources"

	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/brickKit/be-sdk-go/v3/events"
	"github.com/brickKit/be-sdk-go/v3/grpcx"
	"github.com/brickKit/be-sdk-go/v3/httpx"
	"github.com/brickKit/be-sdk-go/v3/runtime"
	"github.com/brickKit/be-sdk-go/v3/work"

	costingv3 "github.com/brickKit/erp-costing/contract/v3/gen/go/be/erp/costing/v3"
)

// Module is the costing component as the launcher and the shells run it: it only assembles the business modules
// under internal/ (their map is internal/AGENTS.md).
type Module struct {
	costingv3.UnimplementedFactServiceServer
	costingv3.UnimplementedLayerServiceServer
	b *core.Base
}

// New builds the module from its runtime.
func New(rt *runtime.Runtime) (*Module, error) {
	b, err := core.NewBase(rt)
	if err != nil {
		return nil, err
	}
	return &Module{b: b}, nil
}

// Routes registers the user surface (contract/openapi.yaml).
func (m *Module) Routes(r *httpx.Routes) error {
	add := func(method, path, op string, h httpx.Handler) {
		r.Add(httpx.Route{Method: method, Path: path, Operation: op, Handle: h})
	}
	add("GET", "/api/costing/policies/:legal_entity_id", core.OpPolicyRead, m.http(policies.GetPolicy))             // getPolicy
	add("PUT", "/api/costing/policies/:legal_entity_id", core.OpPolicyManage, m.http(policies.SetPolicy))           // setPolicy
	add("GET", "/api/costing/scopes", core.OpValuationRead, m.http(sequence.ListScopes))                            // listScopes
	add("GET", "/api/costing/scopes/:legal_entity_id/:product_id", core.OpValuationRead, m.http(sequence.GetScope)) // getScope
	add("GET", "/api/costing/layers", core.OpValuationRead, core.Unsupported("the cost layers"))                    // listLayers
	add("GET", "/api/costing/layers/:movement_id", core.OpValuationRead, core.Unsupported("the cost layers"))       // getLayer
	add("GET", "/api/costing/facts", core.OpValuationRead, core.Unsupported("the cost facts"))                      // listFacts
	return nil
}

// Services registers the system surface: the facts by identity for any component; a purchase return's limit for
// erp/purchase.
func (m *Module) Services(s *grpcx.Services) error {
	s.Register(&costingv3.FactService_ServiceDesc, m, map[string]grpcx.Method{
		"Read": {Operation: core.OpSysRead, Callers: []string{grpcx.AnyComponent}}})
	s.Register(&costingv3.LayerService_ServiceDesc, m, map[string]grpcx.Method{
		"ReturnLimit": {Operation: core.OpSysRead, Callers: []string{core.Purchase}}})
	return nil
}

// Subscriptions declares the inputs: erp/inventory's movements (VS01); erp/purchase's valuation bases and their
// withdrawals, erp/finance's price confirmations and their reversals (LY01-LY05).
func (m *Module) Subscriptions(s *events.Subscriptions) error {
	s.Add(events.Sub{Name: "movements", Streams: []string{core.InventoryStream}, Types: []string{core.MovementRecorded},
		Handle: m.received(sequence.OnMovement)})
	s.Add(events.Sub{Name: "valuation-bases", Streams: []string{core.PurchaseStream}, Types: []string{core.ValuationBasis},
		Handle: m.received(sources.OnBasis)})
	s.Add(events.Sub{Name: "basis-withdrawals", Streams: []string{core.PurchaseStream}, Types: []string{core.BasisWithdrawn},
		Handle: m.received(sources.OnWithdrawn)})
	s.Add(events.Sub{Name: "price-confirmations", Streams: []string{core.FinanceStream}, Types: []string{core.PriceConfirmed},
		Handle: m.received(sources.OnConfirmed)})
	s.Add(events.Sub{Name: "price-reversals", Streams: []string{core.FinanceStream}, Types: []string{core.PriceReversed},
		Handle: m.received(sources.OnReversed)})
	return nil
}

// Jobs declares the background work (protocol 01): a scope's processing reads erp/inventory between its
// transactions, so it is idempotent external.
func (m *Module) Jobs(j *work.Jobs) error {
	j.Add(work.Job{Name: sequence.JobProcess, Kind: work.Queue, Effect: work.IdempotentExternal,
		MaxAttempts: work.Unlimited, Handle: m.job(sequence.Process)})
	j.Add(work.Job{Name: sources.JobApply, Kind: work.Queue, Effect: work.IdempotentExternal,
		MaxAttempts: work.Unlimited, Handle: m.job(sources.Apply)})
	return nil
}

// http, job and received adapt a module's function, which takes the shared base first, to what the SDK registers.
func (m *Module) http(f func(*core.Base, *httpx.Context) error) httpx.Handler {
	return func(c *httpx.Context) error { return f(m.b, c) }
}

func (m *Module) job(f func(*core.Base, context.Context, *work.Run) error) func(context.Context, *work.Run) error {
	return func(ctx context.Context, r *work.Run) error { return f(m.b, ctx, r) }
}

func (m *Module) received(f func(*core.Base, context.Context, db.Tx, events.Received) error) func(context.Context, db.Tx, events.Received) error {
	return func(ctx context.Context, tx db.Tx, e events.Received) error { return f(m.b, ctx, tx, e) }
}
