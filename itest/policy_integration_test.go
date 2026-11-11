//go:build integration

package itest

import (
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// policies: a legal entity's costing method, by version; a legal entity never set is moving average (PO01).
func (fx *fixture) policies() {
	t := fx.t
	p := fx.get("/api/costing/policies/" + fx.le2)
	assert.Equal(t, "moving_average", p["method"], "PO01: the default")
	assert.EqualValues(t, 0, p["version"])
	assert.Equal(t, false, p["in_use"])

	code, out := fx.alice.do("PUT", "/api/costing/policies/"+fx.le2, map[string]any{"method": "fifo", "version": 0})
	require.Equal(t, 200, code, out)
	assert.Equal(t, "fifo", out["method"])
	assert.EqualValues(t, 1, out["version"])
	code, out = fx.alice.do("PUT", "/api/costing/policies/"+fx.le2, map[string]any{"method": "moving_average", "version": 0})
	assert.Equal(t, 409, code, out)
	assert.Equal(t, "VERSION_CONFLICT", out["reason"])
	code, out = fx.alice.do("PUT", "/api/costing/policies/"+fx.le2, map[string]any{"method": "lifo", "version": 1})
	assert.Equal(t, 400, code, out)
	// back to the default for the steps that follow
	code, out = fx.alice.do("PUT", "/api/costing/policies/"+fx.le2, map[string]any{"method": "moving_average", "version": 1})
	require.Equal(t, 200, code, out)

	code, out = fx.alice.do("GET", "/api/costing/policies/00000000-0000-7000-8000-000000000000", nil)
	assert.Equal(t, 409, code, out)
	assert.Equal(t, "LEGAL_ENTITY_NOT_USABLE", out["reason"])
}

// policyInUse: once a movement of the legal entity is valued, its method no longer changes (PO01); runs after the
// sequence steps valued Shanghai's movements.
func (fx *fixture) policyInUse() {
	t := fx.t
	p := fx.get("/api/costing/policies/" + fx.le)
	assert.Equal(t, true, p["in_use"])
	code, out := fx.alice.do("PUT", "/api/costing/policies/"+fx.le, map[string]any{"method": "fifo", "version": p["version"]})
	assert.Equal(t, 409, code, out)
	assert.Equal(t, "POLICY_IN_USE", out["reason"], "PO01")
}
