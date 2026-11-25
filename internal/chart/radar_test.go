package chart

import (
	"image"
	"testing"
)

func TestPolygonMask(t *testing.T) {
	m := polygonMask(image.Rect(0, 0, 100, 100), [][2]float64{{10, 10}, {20, 10}, {20, 20}, {10, 20}})
	if len(m) != 100 || !m[image.Pt(10, 10)] || !m[image.Pt(19, 19)] || m[image.Pt(20, 20)] {
		t.Errorf("10x10 square: %d pixels", len(m))
	}
}

func TestJoinAnd(t *testing.T) {
	for want, in := range map[string][]string{
		"REM":                   {"REM"},
		"REM and Deep":          {"REM", "Deep"},
		"Total, REM and Deep":   {"Total", "REM", "Deep"},
		"Total, REM and 2 more": {"Total", "REM", "Deep", "Timing"},
	} {
		if got := joinAnd(in); got != want {
			t.Errorf("joinAnd(%v) = %q, want %q", in, got, want)
		}
	}
}
