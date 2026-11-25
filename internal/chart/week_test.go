package chart

import (
	"image"
	"math"
	"testing"
	"time"
)

// The target tick inverts what it crosses: white through a filled band,
// black across an empty one.
func TestTargetTickInverts(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 100, 100))
	fillRect(img, img.Bounds(), surface)
	styledArc(img, 50, 50, 30, 40, 1, ringSolid) // a full, filled band
	targetTick(img, 50, 50, 30, 40, 0.25)        // 3 o'clock, inside the band
	if img.GrayAt(85, 50).Y != surface.Y {
		t.Error("tick through a filled band should be white")
	}
	fillRect(img, img.Bounds(), surface)
	targetTick(img, 50, 50, 30, 40, 0.25)
	if img.GrayAt(85, 50).Y != ink.Y {
		t.Error("tick across an empty band should be black")
	}
}

// The three textures differ where it matters: the outlined band is hollow,
// the dashed band has gaps along its length.
func TestRingTextures(t *testing.T) {
	inked := func(st ringStyle) (mid, along int) {
		img := image.NewGray(image.Rect(0, 0, 100, 100))
		fillRect(img, img.Bounds(), surface)
		styledArc(img, 50, 50, 30, 40, 1, st)
		for a := 0.0; a < 1; a += 0.002 {
			x := 50 + 35*math.Sin(a*2*math.Pi)
			y := 50 - 35*math.Cos(a*2*math.Pi)
			if img.GrayAt(int(x), int(y)).Y == ink.Y {
				along++
			}
		}
		if img.GrayAt(50, 15).Y == ink.Y {
			mid = 1
		}
		return mid, along
	}
	if _, n := inked(ringSolid); n < 490 {
		t.Errorf("solid band: %d/500 inked along its middle", n)
	}
	if _, n := inked(ringOutlined); n > 10 {
		t.Errorf("outlined band should be hollow: %d/500 inked along its middle", n)
	}
	if _, n := inked(ringDashed); n < 200 || n > 380 {
		t.Errorf("dashed band: %d/500 inked along its middle, want roughly 4 in 7", n)
	}
}

func TestOptimal(t *testing.T) {
	all := WeekDay{Sleep: 85, Readiness: 90, Activity: 86, HasSleep: true, HasReadiness: true, HasActivity: true}
	if all.Optimal() != 3 || !all.AllOptimal() {
		t.Errorf("all three at or above 85: optimal %d", all.Optimal())
	}
	two := all
	two.Activity = 84
	if two.Optimal() != 2 || two.AllOptimal() {
		t.Errorf("activity 84: optimal %d", two.Optimal())
	}
	missing := all
	missing.HasActivity = false // no activity score: not all optimal
	if missing.Optimal() != 2 || missing.AllOptimal() {
		t.Errorf("missing activity: optimal %d", missing.Optimal())
	}
}

// On the daily brief the inner ring is goal progress: optimal means the goal
// met (100%), not 85.
func TestOptimalWithActivityGoal(t *testing.T) {
	d := WeekDay{Sleep: 90, Readiness: 90, Activity: 95, HasSleep: true, HasReadiness: true, HasActivity: true, ActivityGoal: true}
	if d.AllOptimal() {
		t.Error("95% of the goal is not the goal met")
	}
	d.Activity = 100
	if !d.AllOptimal() {
		t.Error("goal met with sleep and readiness optimal should be all optimal")
	}
}

// The best and toughest nights are named, so an inverted header can't be
// taken for today; a narrow card gets the short name, and other cards none.
func TestWeekCardTag(t *testing.T) {
	if got := weekCardTag(200, true, false); got != "BEST NIGHT" {
		t.Errorf("best = %q", got)
	}
	if got := weekCardTag(200, false, true); got != "TOUGHEST NIGHT" {
		t.Errorf("toughest = %q", got)
	}
	if got := weekCardTag(textWidth(face(true, 10), "TOUGHEST"), false, true); got != "TOUGHEST" {
		t.Errorf("narrow toughest = %q", got)
	}
	if got := weekCardTag(200, false, false); got != "" {
		t.Errorf("unmarked = %q", got)
	}
}

// Each day of the week that has come is a zone named for its date, side by
// side; a day still to come is not.
func TestWeekZones(t *testing.T) {
	sun := time.Date(2026, 11, 22, 0, 0, 0, 0, time.UTC)
	w := WeekReview{Title: "This week", Best: -1, Worst: -1}
	for i := range w.Days {
		w.Days[i] = WeekDay{Day: sun.AddDate(0, 0, i), Future: i > 2, Steps: "—", Stress: "—", Calories: "—"}
	}
	_, zones, err := BrandedFrameZones("Week", "", Footer{}, 800, 480,
		func(wd, h int) (*image.Gray, error) { return WeekView(w, wd, h) })
	if err != nil {
		t.Fatal(err)
	}
	if len(zones) != 3 || zones[0].Name != "day:2026-11-22" || zones[2].Name != "day:2026-11-24" ||
		zones[0].Rect.Max.X > zones[1].Rect.Min.X {
		t.Errorf("zones %v", zones)
	}
}
