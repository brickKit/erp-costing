package sequence

import (
	"context"
	"fmt"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	"github.com/brickKit/be-sdk-go/v3/db"
	"github.com/brickKit/be-sdk-go/v3/recovery"
)

// The recovery checks of the valuation scopes (docs/design.md, 整套恢复之后).
const (
	CheckStopped  = "erp.costing.stopped_scopes"
	CheckNegative = "erp.costing.negative_stock"
	CheckWaiting  = "erp.costing.waiting"
)

// Checks are the scope checks a full recovery runs; each reports its scopes as differences.
func Checks() []recovery.Check {
	return []recovery.Check{
		{Name: CheckStopped, Basis: "设计 VS05；装配项目协议 05b 的计价序列", Run: scopes(
			`SELECT id::text, legal_entity_id::text, product_id::text, coalesce(stopped_reason, '') FROM scope
			WHERE state = 'stopped' ORDER BY id`,
			"stopped: %s",
			"a kind not known waits for the version of erp/costing that values it; an epoch not aligned waits for the "+
				"alignment by identity (docs/design.md, 未决问题); nothing after the stop is valued meanwhile")},
		{Name: CheckNegative, Basis: "设计 IS02", Run: scopes(
			`SELECT id::text, legal_entity_id::text, product_id::text, quantity::text FROM scope WHERE quantity < 0 ORDER BY id`,
			"the valued stock is %s: issues outran the valued receipts",
			"the next receipts level it (IS02); if none comes, compare with erp/inventory's stock (opening layers, "+
				"protocol 16)")},
		{Name: CheckWaiting, Basis: "设计 VS03、LY01", Run: scopes(
			`SELECT id::text, legal_entity_id::text, product_id::text, state || ' since ' || waiting_since::text FROM scope
			WHERE state IN ('waiting_gap', 'waiting_basis') AND waiting_since < now() - interval '1 hour' ORDER BY id`,
			"%s",
			"a gap: erp/inventory has not numbered the missing movement (look at its numbering); a basis: the policy "+
				"price takes over at the end of the wait, a wait this long is a stuck job")},
	}
}

// scopes is a check reporting each scope the query finds: its ID, legal entity, product and a detail.
func scopes(query, detail, disposal string) func(context.Context, recovery.Read) (recovery.Result, error) {
	return func(ctx context.Context, read recovery.Read) (recovery.Result, error) {
		res := recovery.Result{Outcome: recovery.Pass}
		err := read(ctx, func(ctx context.Context, tx db.Tx) error {
			rows, err := tx.Query(ctx, query)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var id, le, product, what string
				if err := rows.Scan(&id, &le, &product, &what); err != nil {
					return err
				}
				res.Differences = append(res.Differences, recovery.Difference{ObjectType: core.Valuation, ObjectID: id,
					Detail: fmt.Sprintf("legal entity %s, product %s: "+detail, le, product, what)})
			}
			return rows.Err()
		})
		if len(res.Differences) > 0 {
			res.Outcome, res.Disposal = recovery.Diff, disposal
		}
		return res, err
	}
}
