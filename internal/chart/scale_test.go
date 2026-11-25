package chart

import (
	"testing"
	"time"
)

func TestNiceScale(t *testing.T) {
	cases := []struct {
		lo, hi         float64
		min, max, step float64
	}{
		{55, 94, 50, 100, 10},         // 30 days of real sleep scores
		{66, 94, 60, 100, 10},         // 14 days
		{55, 96, 40, 100, 20},         // just past a step boundary: coarser
		{0, 100, 0, 100, 50},          // fixed score scale
		{3000, 14000, 0, 15000, 5000}, // steps
		{80, 80, 75, 85, 5},           // flat series gets a band
	}
	for _, c := range cases {
		min, max, step := niceScale(c.lo, c.hi, 4)
		if min != c.min || max != c.max || step != c.step {
			t.Errorf("niceScale(%v, %v) = %v..%v step %v, want %v..%v step %v",
				c.lo, c.hi, min, max, step, c.min, c.max, c.step)
		}
	}
}

// Across the US spring-forward change, consecutive dates are still one day
// apart, so the line is not broken there.
func TestDayIndexAcrossDST(t *testing.T) {
	ny, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Skip("no tzdata:", err)
	}
	a := time.Date(2026, 3, 8, 0, 0, 0, 0, ny)
	b := time.Date(2026, 3, 9, 0, 0, 0, 0, ny)
	if d := dayIndex(a, b); d != 1 {
		t.Errorf("dayIndex across DST = %d, want 1", d)
	}
}
