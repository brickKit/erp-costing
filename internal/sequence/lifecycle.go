package sequence

import (
	"regexp"

	core "github.com/brickKit/erp-costing/v3/internal/core"
)

// A valuation scope's states in one table (docs/design.md, the valuation progress table; VS02-VS05, LY01). A command
// is what one step of the processing sees (VS02 looks at the next number once a step).

const (
	Running      = "running"
	WaitingGap   = "waiting_gap"
	WaitingBasis = "waiting_basis"
	Stopped      = "stopped"
)

const (
	Value       = "value"        // the next number can be valued
	AwaitBasis  = "await_basis"  // the next number is a purchase receipt waiting for its basis
	AwaitGap    = "await_gap"    // the next number has not come, a later one has
	GapDue      = "gap_due"      // the gap wait is over
	Basis       = "basis"        // the basis of the receipt waiting came
	BasisDue    = "basis_due"    // the basis wait is over
	UnknownKind = "unknown_kind" // the next number's kind is not one this version values
	NewEpoch    = "new_epoch"    // the old epoch is read to its end: follow the new one
	EpochAhead  = "epoch_ahead"  // the old epoch is read to its end, and this component valued past it
)

// Lifecycle is the table.
var Lifecycle = core.Table{
	Rule:     regexp.MustCompile(`^(VS|LY)\d\d$`),
	Phases:   []string{Running, WaitingGap, WaitingBasis, Stopped},
	Commands: []string{Value, AwaitBasis, AwaitGap, GapDue, Basis, BasisDue, UnknownKind, NewEpoch, EpochAhead},
	Cells: map[string]map[string]core.Decision{
		Value: {
			Running:      core.ProceedCell("VS02"),
			WaitingGap:   core.MoveCell("VS03", Running, ""),
			WaitingBasis: core.IgnoreCell("LY01"),
			Stopped:      core.IgnoreCell("VS05"),
		},
		AwaitBasis: {
			Running:      core.MoveCell("LY01", WaitingBasis, ""),
			WaitingGap:   core.MoveCell("LY01", WaitingBasis, ""),
			WaitingBasis: core.IgnoreCell("LY01"),
			Stopped:      core.IgnoreCell("VS05"),
		},
		AwaitGap: {
			Running:      core.MoveCell("VS03", WaitingGap, ""),
			WaitingGap:   core.IgnoreCell("VS03"),
			WaitingBasis: core.IgnoreCell("VS03"),
			Stopped:      core.IgnoreCell("VS05"),
		},
		GapDue: {
			Running:      core.IgnoreCell("VS03"),
			WaitingGap:   core.ProceedCell("VS03"),
			WaitingBasis: core.IgnoreCell("VS03"),
			Stopped:      core.IgnoreCell("VS05"),
		},
		Basis: {
			Running:      core.IgnoreCell("LY01"),
			WaitingGap:   core.IgnoreCell("LY01"),
			WaitingBasis: core.MoveCell("LY01", Running, ""),
			Stopped:      core.IgnoreCell("VS05"),
		},
		BasisDue: {
			Running:      core.IgnoreCell("LY01"),
			WaitingGap:   core.IgnoreCell("LY01"),
			WaitingBasis: core.MoveCell("LY01", Running, ""),
			Stopped:      core.IgnoreCell("VS05"),
		},
		UnknownKind: {
			Running:      core.MoveCell("VS02", Stopped, ""),
			WaitingGap:   core.MoveCell("VS02", Stopped, ""),
			WaitingBasis: core.IgnoreCell("VS02"),
			Stopped:      core.IgnoreCell("VS05"),
		},
		NewEpoch: {
			Running:      core.ProceedCell("VS04"),
			WaitingGap:   core.MoveCell("VS04", Running, ""),
			WaitingBasis: core.IgnoreCell("VS04"),
			Stopped:      core.IgnoreCell("VS05"),
		},
		EpochAhead: {
			Running:      core.MoveCell("VS04", Stopped, ""),
			WaitingGap:   core.MoveCell("VS04", Stopped, ""),
			WaitingBasis: core.IgnoreCell("VS04"),
			Stopped:      core.IgnoreCell("VS05"),
		},
	},
}
