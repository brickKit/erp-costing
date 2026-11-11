// Package methods is what differs between the costing methods (docs/design.md IS01, AD02): how much value an issue
// takes, and how an adjustment splits between the stock's value and the variance. Pure arithmetic on what the caller
// read in its transaction; the replacement point of a costing method (docs/design.md, 部署与替换).
package methods

import (
	core "github.com/brickKit/erp-costing/v3/internal/core"
)

// Range is a stretch of a layer's units: from the start-th unit (0 is the first), quantity units.
type Range struct {
	Start    string
	Quantity string
}

// AverageTake is the value a moving average issue of qty takes from a scope with onHand and value (IS01): qty ÷
// onHand × value, rounded; all of the stock takes all of its value. Past the stock (IS02) the rest is priced at
// policyPrice; past is that rest ("0" when the stock covers it).
func AverageTake(qty, onHand, value, policyPrice string, scale int32) (cost, past string) {
	switch c := core.Cmp(qty, onHand); {
	case c < 0:
		return core.Amount(core.Quo(core.Mul(qty, value), onHand), scale), "0"
	case c == 0:
		return core.Amount(value, scale), "0"
	}
	covered, past := "0", qty
	if core.Cmp(onHand, "0") > 0 {
		covered, past = value, core.Minus(qty, onHand)
	}
	return core.Amount(core.Sum(covered, core.Amount(core.Mul(past, policyPrice), scale)), scale), past
}

// Split is an adjustment of diff a unit over ranges of a layer (AD01, AD02): total = diff × their quantity, rounded;
// inventory the part that changes the stock's value, the rest the variance (the rounding lands there).
//   - moving average: inventory = total × min(onHand, quantity) ÷ quantity (nothing when the scope has no stock);
//   - FIFO: the units already gone are the layer's first issued units; the ranges' units still in stock carry the
//     difference into the value, the units gone into the variance.
func Split(method string, onHand, issued string, ranges []Range, diff string, scale int32) (total, inventory, variance string) {
	qty := "0"
	for _, r := range ranges {
		qty = core.Sum(qty, r.Quantity)
	}
	total = core.Amount(core.Mul(diff, qty), scale)
	switch {
	case core.Cmp(qty, "0") == 0:
		inventory = core.Amount("0", scale)
	case method == core.MethodFIFO:
		inStock := "0"
		for _, r := range ranges {
			gone := core.Minus(smaller(issued, core.Sum(r.Start, r.Quantity)), r.Start)
			if core.Cmp(gone, "0") < 0 {
				gone = "0"
			}
			inStock = core.Sum(inStock, core.Minus(r.Quantity, gone))
		}
		inventory = core.Amount(core.Mul(diff, inStock), scale)
	case core.Cmp(onHand, "0") <= 0:
		inventory = core.Amount("0", scale)
	default:
		in := onHand
		if core.Cmp(in, qty) > 0 {
			in = qty
		}
		inventory = core.Amount(core.Quo(core.Mul(total, in), qty), scale)
	}
	return total, inventory, core.Amount(core.Minus(total, inventory), scale)
}

// smaller is the smaller decimal.
func smaller(a, b string) string {
	if core.Cmp(a, b) <= 0 {
		return a
	}
	return b
}
