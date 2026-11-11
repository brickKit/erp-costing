//go:build integration

package itest

import (
	"github.com/stretchr/testify/assert"
)

// notInThisVersion: P3a's skeleton answers CAPABILITY_UNSUPPORTED on the routes not built yet (each later step
// replaces its routes; this step goes once every route is built).
func (fx *fixture) notInThisVersion() {
	for _, r := range []struct{ method, path string }{
		{"GET", "/api/costing/policies/" + fx.le},
		{"GET", "/api/costing/scopes?legal_entity_id=" + fx.le},
		{"GET", "/api/costing/layers?legal_entity_id=" + fx.le},
		{"GET", "/api/costing/facts?legal_entity_id=" + fx.le},
	} {
		code, out := fx.alice.do(r.method, r.path, nil)
		assert.Equal(fx.t, "CAPABILITY_UNSUPPORTED", out["reason"], "%s %s: %d %v", r.method, r.path, code, out)
	}
}
