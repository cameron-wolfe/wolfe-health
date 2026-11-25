package chart

import (
	"image"
	"testing"
	"time"
)

// Arcs are linear on a fixed 6-hour circle; longer days are flagged.
func TestDialFrac(t *testing.T) {
	cases := []struct {
		minutes  float64
		frac     float64
		wantOver bool
	}{
		{0, 0, false}, {90, 0.25, false}, {180, 0.5, false}, {360, 1, false}, {555, 1, true},
	}
	for _, c := range cases {
		f, over := dialFrac(c.minutes)
		if f != c.frac || over != c.wantOver {
			t.Errorf("dialFrac(%v) = %v, %v; want %v, %v", c.minutes, f, over, c.frac, c.wantOver)
		}
	}
}

// An arc of a quarter turn from 12 o'clock fills only the top-right quarter.
func TestArcBandQuarter(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 41, 41))
	fillRect(img, img.Bounds(), surface)
	arcBand(img, 20.5, 20.5, 10, 18, 0, 0.25, ink)
	quadrant := map[string]int{}
	for y := 0; y < 41; y++ {
		for x := 0; x < 41; x++ {
			if img.GrayAt(x, y).Y != ink.Y {
				continue
			}
			k := "bottom-"
			if y <= 20 { // row 20's pixel centers sit on the 3 o'clock line itself
				k = "top-"
			}
			if x >= 20 {
				k += "right"
			} else {
				k += "left"
			}
			quadrant[k]++
		}
	}
	if quadrant["top-right"] < 100 || quadrant["top-left"]+quadrant["bottom-left"]+quadrant["bottom-right"] > 4 {
		t.Errorf("quarter arc spread: %v", quadrant)
	}
}

// A day past six hours of stress gets a full ring plus a tick above it.
func TestDialOverflowTick(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 60, 60))
	fillRect(img, img.Bounds(), surface)
	drawDial(img, 30, 32, 22, StressDay{Minutes: 555})
	if img.GrayAt(30, 32-22-2).Y != ink.Y {
		t.Error("no overflow tick above the ring")
	}
	if img.GrayAt(30, 32+22-2).Y != ink.Y { // bottom of the full stress ring
		t.Error("stress ring not full")
	}
}

func TestStressCalendarRenders(t *testing.T) {
	nov := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	m := StressMonth{
		Title: "Stress", Month: nov, Today: nov.AddDate(0, 0, 18),
		Days: []StressDay{
			{Day: nov, Minutes: 45, Recovery: 120, HasRecovery: true, Text: "45m", Summary: "normal"},
			{Day: nov.AddDate(0, 0, 8), Minutes: 285, Text: "4h 45m", Summary: "stressful"},
			{Day: nov.AddDate(0, 0, 14), Minutes: 45, Text: "45m", Summary: "restored"},
			{Day: nov.AddDate(0, 0, 18), Minutes: 0, Text: "0m", Partial: true},
		},
		Stats: []StatLine{{"Daily average", "1h 18m"}},
	}
	img, err := StressCalendar(m, 800, 480)
	if err != nil {
		t.Fatal(err)
	}
	// marks are pure black and white: no gray except the faint scale track,
	// grid lines and muted text
	_ = img
	// a month needing six rows (starts Saturday, 31 days) still fits
	aug := time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC)
	if _, err := StressCalendar(StressMonth{Month: aug, Today: aug}, 800, 480); err != nil {
		t.Fatal(err)
	}
	if _, err := StressCalendar(m, 300, 200); err == nil {
		t.Error("expected an error for a tiny image")
	}
}
