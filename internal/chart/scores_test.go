package chart

import (
	"image"
	"math"
	"testing"
	"time"
)

func TestScoreCalendarRenders(t *testing.T) {
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	m := ScoreMonth{
		Title: "Scores", Month: nov, Today: nov.AddDate(0, 0, 18),
		Days: []WeekDay{
			{Day: nov, Sleep: 88, Readiness: 90, Activity: 86, HasSleep: true, HasReadiness: true, HasActivity: true},
			{Day: nov.AddDate(0, 0, 1), Sleep: 70, HasSleep: true},
		},
		StatsNote: "averages",
		Stats:     []StatLine{{"Sleep", "83 · +3"}},
	}
	for _, size := range [][2]int{{800, 480}, {640, 384}, {1200, 825}} {
		if _, err := ScoreCalendar(m, size[0], size[1]); err != nil {
			t.Fatalf("%v: %v", size, err)
		}
	}
	// a month needing six rows (starts Saturday, 31 days) still fits
	aug := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if _, err := ScoreCalendar(ScoreMonth{Month: aug, Today: aug.AddDate(0, 1, 0)}, 800, 480); err != nil {
		t.Fatal(err)
	}
	if _, err := ScoreCalendar(m, 300, 200); err == nil {
		t.Error("expected an error for a tiny image")
	}
}

// The rings keep clear of the date in the corner, in short and tall cells.
func TestRingSpotClearsDate(t *testing.T) {
	for _, r := range []image.Rectangle{
		image.Rect(0, 0, 69, 58), image.Rect(0, 0, 69, 71), image.Rect(10, 20, 120, 110),
	} {
		cx, cy, R := ringSpot(r, 14)
		if R < 12 {
			t.Errorf("%v: rings too small, R=%v", r, R)
		}
		corner := [2]float64{float64(r.Min.X + 19), float64(r.Min.Y + 15)}
		if d := math.Hypot(cx-corner[0], cy-corner[1]); d < R+2 {
			t.Errorf("%v: rings reach the date (%.1f < %.1f)", r, d, R+2)
		}
		if cx+R > float64(r.Max.X) || cy+R > float64(r.Max.Y) || cx-R < float64(r.Min.X) || cy-R < float64(r.Min.Y) {
			t.Errorf("%v: rings leave the cell: c=(%v,%v) R=%v", r, cx, cy, R)
		}
	}
}
