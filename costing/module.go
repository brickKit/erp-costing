package costing

import (
	core "github.com/brickKit/erp-costing/v3/internal/core"

	"github.com/brickKit/be-sdk-go/v3/grpcx"
	"github.com/brickKit/be-sdk-go/v3/httpx"
	"github.com/brickKit/be-sdk-go/v3/runtime"

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
	add("GET", "/api/costing/policies/:legal_entity_id", core.OpPolicyRead, core.Unsupported("the costing policy"))         // getPolicy
	add("PUT", "/api/costing/policies/:legal_entity_id", core.OpPolicyManage, core.Unsupported("the costing policy"))       // setPolicy
	add("GET", "/api/costing/scopes", core.OpValuationRead, core.Unsupported("the valuation scopes"))                       // listScopes
	add("GET", "/api/costing/scopes/:legal_entity_id/:product_id", core.OpValuationRead, core.Unsupported("the valuation")) // getScope
	add("GET", "/api/costing/layers", core.OpValuationRead, core.Unsupported("the cost layers"))                            // listLayers
	add("GET", "/api/costing/layers/:movement_id", core.OpValuationRead, core.Unsupported("the cost layers"))               // getLayer
	add("GET", "/api/costing/facts", core.OpValuationRead, core.Unsupported("the cost facts"))                              // listFacts
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
