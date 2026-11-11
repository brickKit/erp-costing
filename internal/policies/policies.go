// Package policies is a legal entity's costing method (docs/design.md PO01).
package policies

import (
	"context"
	"fmt"
	"time"

	core "github.com/brickKit/erp-costing/v3/internal/core"
	masterdata "github.com/brickKit/erp-costing/v3/internal/masterdata"

	"github.com/brickKit/be-sdk-go/v3/canon"
	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/brickKit/be-sdk-go/v3/httpx"
)

// The costing methods (PO01).
const (
	MovingAverage = core.MethodMovingAverage
	FIFO          = core.MethodFIFO
)

// Policy is a legal entity's policy as the user surface reads it.
type Policy struct {
	LegalEntityID string `json:"legal_entity_id"`
	Method        string `json:"method"`
	InUse         bool   `json:"in_use"`
	Version       int64  `json:"version"`
}

// read is the policy of a legal entity: the default (moving average, version 0) when never set. A lock first writes
// the default row, so that there is a row to lock.
func read(ctx context.Context, tx db.Tx, le string, lock string) (Policy, error) {
	p := Policy{LegalEntityID: le, Method: MovingAverage}
	if lock != "" {
		if _, err := tx.Exec(ctx, `INSERT INTO policy (legal_entity_id) VALUES ($1) ON CONFLICT DO NOTHING`, le); err != nil {
			return p, err
		}
	}
	err := tx.QueryRow(ctx, `SELECT coalesce((SELECT method FROM policy WHERE legal_entity_id = $1 `+lock+`), 'moving_average'),
		coalesce((SELECT version FROM policy WHERE legal_entity_id = $1), 0),
		EXISTS (SELECT 1 FROM valued_movement WHERE legal_entity_id = $1)`, le).Scan(&p.Method, &p.Version, &p.InUse)
	return p, err
}

// MethodOf is the method a valuation of the legal entity uses, its policy row held FOR SHARE until the valuation's
// transaction ends: a change waits for it and then sees the legal entity in use (PO01).
func MethodOf(ctx context.Context, tx db.Tx, le string) (string, error) {
	p, err := read(ctx, tx, le, "FOR SHARE")
	return p.Method, err
}

// GetPolicy answers a legal entity's policy; a legal entity mdm/org does not know is refused.
func GetPolicy(m *core.Base, c *httpx.Context) error {
	le := c.Param("legal_entity_id")
	if !core.UUIDForm.MatchString(le) {
		return core.NotFound("legal entity")
	}
	ctx, _, err := core.Check(c, core.OpPolicyRead, core.EntityFacts(le))
	if err != nil {
		return err
	}
	if _, err := masterdata.LegalEntityOf(m, ctx, le); err != nil {
		return err
	}
	var p Policy
	err = m.RT.DB.Tx(ctx, func(ctx context.Context, tx db.Tx) error {
		p, err = read(ctx, tx, le, "")
		return err
	})
	if err != nil {
		return err
	}
	return c.JSON(200, p)
}

type policyIn struct {
	Method  string `json:"method"`
	Version *int64 `json:"version"`
}

// SetPolicy changes a legal entity's method by version (PO01): refused once a movement of the legal entity is valued
// (POLICY_IN_USE); the same method again changes nothing.
func SetPolicy(m *core.Base, c *httpx.Context) error {
	le := c.Param("legal_entity_id")
	if !core.UUIDForm.MatchString(le) {
		return core.NotFound("legal entity")
	}
	var in policyIn
	if err := c.BindJSON(&in); err != nil {
		return err
	}
	if in.Method != MovingAverage && in.Method != FIFO {
		return core.Invalid("method", "method is moving_average or fifo")
	}
	if in.Version == nil {
		return core.Invalid("version", "version is required")
	}
	ctx, actor, err := core.Check(c, core.OpPolicyManage, core.EntityFacts(le))
	if err != nil {
		return err
	}
	if _, err := masterdata.LegalEntityOf(m, ctx, le); err != nil {
		return err
	}
	var p Policy
	err = m.RT.DB.Tx(ctx, func(ctx context.Context, tx db.Tx) error {
		if p, err = read(ctx, tx, le, "FOR UPDATE"); err != nil {
			return err
		}
		switch {
		case *in.Version != p.Version:
			return core.ErrVersion.New(fmt.Sprintf("the policy is at version %d", p.Version))
		case in.Method == p.Method:
			return nil
		case p.InUse:
			return core.ErrPolicyInUse.New("legal entity " + le + " has valued movements under " + p.Method)
		}
		if err := tx.QueryRow(ctx, `UPDATE policy SET method = $2, version = version + 1, updated_at = now()
			WHERE legal_entity_id = $1 RETURNING version`, le, in.Method).Scan(&p.Version); err != nil {
			return err
		}
		p.Method = in.Method
		return core.Change(m, ctx, tx, actor, core.OpPolicyManage, core.Policies, le,
			map[string]any{"legal_entity_id": le, "method": p.Method, "version": p.Version, "updated_at": canon.Time(time.Now())})
	})
	if err != nil {
		return err
	}
	return c.JSON(200, p)
}
