package costing

import (
	core "github.com/brickKit/erp-costing/v3/internal/core"
	sequence "github.com/brickKit/erp-costing/v3/internal/sequence"

	"github.com/brickKit/be-sdk-go/v3/recovery"
)

// RecoveryChecks registers what a full recovery asks this member (be-protocol chapter 14; docs/design.md, "整套恢复
// 之后"): whether the policies its audit names are still there and at which version, and the scope checks. The cost
// data is worked out from the movements and is not audited.
func (m *Module) RecoveryChecks(c *recovery.Checks) error {
	c.Presence(core.Policies, recovery.TablePresence("policy", "legal_entity_id", "version"))
	for _, check := range sequence.Checks() {
		if err := c.Add(check); err != nil {
			return err
		}
	}
	return nil
}
