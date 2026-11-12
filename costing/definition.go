// Package costing is the costing component: the value of stock (D46). It values erp/inventory's movements in each
// product and legal entity's valuation sequence (protocol 05b), keeps the cost layers with their price sources (the
// policy price, erp/purchase's valuation basis, erp/finance's price confirmation), publishes the adjustments when a
// source is replaced, and the cost facts by erp/finance's shared accounting fact contract.
package costing

import (
	"embed"
	"io/fs"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	"github.com/brickKit/be-sdk-go/v3/component"
	"github.com/brickKit/be-sdk-go/v3/config"
	"github.com/brickKit/be-sdk-go/v3/events"
	"github.com/brickKit/be-sdk-go/v3/runtime"
)

// Identity.
const (
	Version  = "3.0.5"
	GRPCPort = 9229
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func migrations() fs.FS {
	sub, _ := fs.Sub(migrationFiles, "migrations")
	return sub
}

// Keys are the component's own configuration keys.
func Keys() []config.Key {
	return []config.Key{
		// how long a valuation scope waits for a missing sequence number before it reads it from erp/inventory (VS03,
		// protocol 05b's start value 5 minutes)
		{Name: core.KeyGapWait, Type: config.Duration},
		// how long a purchase receipt's movement waits for its valuation basis before the policy prices it (LY01,
		// protocol 05b's start value 5 minutes)
		{Name: core.KeyBasisWait, Type: config.Duration},
	}
}

// Own describes the own keys for the manifest's configSchema.
var Own = map[string]string{
	core.KeyGapWait: "how long a valuation sequence waits for a missing number before it reads it from erp/inventory " +
		"(protocol 05b's start value 5 minutes); unset: 5 minutes",
	core.KeyBasisWait: "how long a purchase receipt's movement waits for its valuation basis before the policy prices it " +
		"(protocol 05b's start value 5 minutes); unset: 5 minutes",
}

// Definition is the component as the launcher and the shells take it.
func Definition() component.Definition {
	return component.Definition{ID: core.ID, Version: Version, Keys: Keys(), Migrations: migrations(), GRPCPort: GRPCPort, Subscribes: true,
		Streams: []events.Stream{{Name: core.Stream, Partitions: 4}}, Authz: core.Definitions(), Resend: core.OpResend,
		Recovery: core.OpRecover, New: func(rt *runtime.Runtime) (component.Module, error) { return New(rt) }}
}
