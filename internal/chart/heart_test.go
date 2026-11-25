package chart

import (
	"testing"
	"time"
)

func TestHeartDayViewRenders(t *testing.T) {
	loc := time.FixedZone("PST", -8*3600)
	start := time.Date(2026, 11, 20, 0, 0, 0, 0, loc)
	h := HeartDay{
		Title: "Heart rate today", Start: start, End: start.AddDate(0, 0, 1),
		Now: start.Add(11 * time.Hour), SlotLen: 30 * time.Minute,
		Asleep:     []Span{{Start: start, End: start.Add(6 * time.Hour)}},
		Activities: []Span{{Start: start.Add(9 * time.Hour), End: start.Add(9*time.Hour + 10*time.Minute), Label: "walking"}},
	}
	for m := 0; m < 11*60; m += 5 {
		if m > 7*60 && m < 8*60 {
			continue // a gap the line breaks across
		}
		h.Readings = append(h.Readings, HeartPoint{Time: start.Add(time.Duration(m) * time.Minute), BPM: 55 + float64(m%40)})
	}
	for i := 0; i < 48; i++ {
		h.Usual = append(h.Usual, HeartSlot{Start: start.Add(time.Duration(i) * 30 * time.Minute), Lo: 58, Hi: 70})
	}
	h.Peak, h.HasPeak = h.Readings[10], true
	for _, size := range [][2]int{{800, 480}, {640, 384}, {1200, 825}} {
		if _, err := HeartDayView(h, size[0], size[1]); err != nil {
			t.Fatalf("%v: %v", size, err)
		}
	}
	// no readings yet still draws
	if _, err := HeartDayView(HeartDay{Start: start, End: start.AddDate(0, 0, 1), Now: start}, 800, 480); err != nil {
		t.Fatal(err)
	}
	if _, err := HeartDayView(h, 300, 200); err == nil {
		t.Error("expected an error for a tiny image")
	}
}
