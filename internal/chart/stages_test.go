package chart

import (
	"testing"
	"time"
)

func TestStageNightTotals(t *testing.T) {
	n := StageNight{Deep: time.Hour, Light: 3 * time.Hour, REM: 2 * time.Hour, Awake: 30 * time.Minute}
	if n.Asleep() != 6*time.Hour || n.InBed() != 6*time.Hour+30*time.Minute {
		t.Errorf("asleep %v, in bed %v", n.Asleep(), n.InBed())
	}
	for _, s := range stackOrder {
		if n.of(s) == 0 {
			t.Errorf("of(%v) = 0", s)
		}
	}
}

// A bar's top sits at its total time in bed: the white gaps between
// segments come out of the segments, not on top of them.
func TestStageBarHeightIsExact(t *testing.T) {
	day := time.Date(2026, 11, 19, 0, 0, 0, 0, time.UTC)
	sn := StageNights{Title: "t", From: day.AddDate(0, 0, -1), To: day,
		Nights: []StageNight{{Day: day, Deep: 2 * time.Hour, Light: 3 * time.Hour, REM: 2 * time.Hour, Awake: time.Hour}}}
	img, err := StageNightsView(sn, 800, 480)
	if err != nil {
		t.Fatal(err)
	}
	// same layout constants as the renderer: 8h max -> niceScale(0,8,6) = 0..8
	plotL, plotR, plotT, plotB := 16+40, 800-16-176-20, 88, 480-44
	slotW := float64(plotR-plotL) / 2
	cx := int(float64(plotL) + 1.5*slotW)
	_, ymax, _ := niceScale(0, 8, 6)
	wantTop := plotB - int(float64(plotB-plotT)*8/ymax+0.5)
	top := -1
	for y := plotT; y < plotB; y++ {
		if img.GrayAt(cx, y).Y == ink.Y {
			top = y
			break
		}
	}
	if top != wantTop {
		t.Errorf("bar top at y=%d, want %d (8h of %vh)", top, wantTop, ymax)
	}
}
