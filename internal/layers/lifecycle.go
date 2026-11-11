package layers

import (
	"regexp"

	core "github.com/brickKit/erp-costing/v3/internal/core"
)

// A cost layer's price source in one table (docs/design.md, the layer's price source table; LY02-LY05): the state is
// the highest source in effect on the layer (an invoice on any of its units makes it invoice).

const (
	OnBasis    = "basis"    // a valuation basis came for the layer's receipt line
	OnWithdraw = "withdraw" // erp/purchase withdrew the basis
	OnConfirm  = "confirm"  // an invoice confirmed the price of some of the layer's units
	OnReverse  = "reverse"  // an invoice's confirmation was reversed
)

// Lifecycle is the table; its phases are the sources.
var Lifecycle = core.Table{
	Rule:     regexp.MustCompile(`^LY\d\d$`),
	Phases:   []string{SourcePolicy, SourceBasis, SourceInvoice},
	Commands: []string{OnBasis, OnWithdraw, OnConfirm, OnReverse},
	Cells: map[string]map[string]core.Decision{
		OnBasis: {
			SourcePolicy:  core.MoveCell("LY02", SourceBasis, ""),
			SourceBasis:   core.IgnoreCell("LY02"),
			SourceInvoice: core.ProceedCell("LY02"),
		},
		OnWithdraw: {
			SourcePolicy:  core.ProceedCell("LY05"),
			SourceBasis:   core.ProceedCell("LY05"),
			SourceInvoice: core.ProceedCell("LY05"),
		},
		OnConfirm: {
			SourcePolicy:  core.MoveCell("LY03", SourceInvoice, ""),
			SourceBasis:   core.MoveCell("LY03", SourceInvoice, ""),
			SourceInvoice: core.ProceedCell("LY03"),
		},
		OnReverse: {
			SourcePolicy:  core.IgnoreCell("LY04"),
			SourceBasis:   core.IgnoreCell("LY04"),
			SourceInvoice: core.ProceedCell("LY04"),
		},
	},
}
