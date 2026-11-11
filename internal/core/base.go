package core

import (
	"fmt"
	"time"

	"github.com/brickKit/be-sdk-go/v3/runtime"
)

// Base is what every module works with: the component's runtime (its database, calls, events, audit, logger) and
// the configuration read once at the start.
type Base struct {
	RT *runtime.Runtime
	// GapWait is how long a valuation scope waits for a missing sequence number before it reads it from erp/inventory
	// (KeyGapWait, VS03)
	GapWait time.Duration
	// BasisWait is how long a purchase receipt's movement waits for its valuation basis before the policy prices it
	// (KeyBasisWait, LY01)
	BasisWait time.Duration
}

// The configuration keys of the component (costing/definition.go declares them).
const (
	KeyGapWait   = "VALUATION_GAP_WAIT"
	KeyBasisWait = "VALUATION_BASIS_WAIT"
)

// The start values of protocol 05b's parameters.
const (
	DefaultGapWait   = 5 * time.Minute
	DefaultBasisWait = 5 * time.Minute
)

// NewBase reads the configuration a module needs; a malformed value stops the start.
func NewBase(rt *runtime.Runtime) (*Base, error) {
	b := &Base{RT: rt, GapWait: DefaultGapWait, BasisWait: DefaultBasisWait}
	for _, k := range []struct {
		key string
		to  *time.Duration
	}{{KeyGapWait, &b.GapWait}, {KeyBasisWait, &b.BasisWait}} {
		if rt.Config.Has(k.key) {
			if *k.to = rt.Config.Duration(k.key); *k.to <= 0 {
				return nil, fmt.Errorf("%s: a wait is positive", k.key)
			}
		}
	}
	return b, nil
}
