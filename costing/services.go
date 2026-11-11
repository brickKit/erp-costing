package costing

import (
	"context"

	facts "github.com/brickKit/erp-costing/v3/internal/facts"

	costingv3 "github.com/brickKit/erp-costing/contract/v3/gen/go/be/erp/costing/v3"
)

// Read is FactService.Read: this component's incremental facts by identity (internal/facts).
func (m *Module) Read(ctx context.Context, in *costingv3.ReadRequest) (*costingv3.ReadResponse, error) {
	return facts.Read(m.b, ctx, in)
}
