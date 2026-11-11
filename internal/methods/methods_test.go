package methods

import (
	"testing"

	core "github.com/brickKit/erp-costing/v3/internal/core"

	"github.com/stretchr/testify/assert"
)

// TestAverageTake: IS01 by the average, all of the stock all of its value; IS02 past the stock at the policy price.
func TestAverageTake(t *testing.T) {
	cost, past := AverageTake("4", "20", "230", "0", 2)
	assert.Equal(t, []string{"46.00", "0"}, []string{cost, past})
	cost, _ = AverageTake("1", "3", "10.01", "0", 2)
	assert.Equal(t, "3.34", cost)
	cost, _ = AverageTake("2", "2", "6.67", "0", 2)
	assert.Equal(t, "6.67", cost)
	cost, past = AverageTake("5", "2", "20", "12", 2)
	assert.Equal(t, []string{"56.00", "3"}, []string{cost, past}, "IS02: 20 + 3 × 12")
	cost, past = AverageTake("2", "-1", "-12", "12", 2)
	assert.Equal(t, []string{"24.00", "2"}, []string{cost, past}, "IS02: below zero already")
}

// TestSplit: AD02 by method, the protocol's cases IC24 (FIFO) and IC25 (moving average).
func TestSplit(t *testing.T) {
	// IC25: a layer of 10, 4 in stock in the scope, +20 in all: 8 into the value, 12 a variance
	total, inv, v := Split(core.MethodMovingAverage, "4", "0", []Range{{"0", "10"}}, "2", 2)
	assert.Equal(t, []string{"20.00", "8.00", "12.00"}, []string{total, inv, v})
	// IC24: 10 units, 6 issued; A confirms the first 6 at +2: all a variance; B the next 4 at +1: all into the value
	total, inv, v = Split(core.MethodFIFO, "4", "6", []Range{{"0", "6"}}, "2", 2)
	assert.Equal(t, []string{"12.00", "0.00", "12.00"}, []string{total, inv, v})
	total, inv, v = Split(core.MethodFIFO, "4", "6", []Range{{"6", "4"}}, "1", 2)
	assert.Equal(t, []string{"4.00", "4.00", "0.00"}, []string{total, inv, v})
	// a range across the issued edge, in two stretches
	total, inv, v = Split(core.MethodFIFO, "4", "6", []Range{{"4", "4"}, {"9", "1"}}, "-1", 2)
	assert.Equal(t, []string{"-5.00", "-3.00", "-2.00"}, []string{total, inv, v})
	// moving average with nothing in stock: all a variance; the rounding lands in the variance
	total, inv, v = Split(core.MethodMovingAverage, "0", "0", []Range{{"0", "4"}}, "-1", 2)
	assert.Equal(t, []string{"-4.00", "0.00", "-4.00"}, []string{total, inv, v})
	total, inv, v = Split(core.MethodMovingAverage, "1", "0", []Range{{"0", "3"}}, "0.01", 2)
	assert.Equal(t, []string{"0.03", "0.01", "0.02"}, []string{total, inv, v})
}
