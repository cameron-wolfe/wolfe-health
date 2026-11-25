package chart

import (
	"testing"
	"time"
)

func moonNight(start time.Time, hours float64) Night {
	n := Night{Title: "Night of Tue, Nov 18", Subtitle: "test", Headline: "7h asleep",
		Start: start, End: start.Add(time.Duration(hours * float64(time.Hour))), StageEpoch: 30 * time.Second}
	for i := 0; i < int(hours*120); i++ {
		n.Stages = append(n.Stages, Stage(1+i/60%4))
	}
	v := 55.0
	n.HeartRate = Series{Start: start, Interval: 5 * time.Minute}
	for i := 0; i < int(hours*12); i++ {
		n.HeartRate.Values = append(n.HeartRate.Values, &v)
	}
	return n
}

// WolfePhase draws a night across midnight, and one longer than the face's
// 12 hours without wrapping; a night without an end is refused.
func TestMoonViewRenders(t *testing.T) {
	loc := time.FixedZone("PST", -8*3600)
	for _, n := range []Night{
		moonNight(time.Date(2026, 11, 18, 23, 2, 0, 0, loc), 6.1),
		moonNight(time.Date(2026, 11, 18, 20, 0, 0, 0, loc), 14),
	} {
		for _, size := range [][2]int{{800, 480}, {640, 384}, {1200, 825}} {
			if _, err := MoonView(n, size[0], size[1]); err != nil {
				t.Fatalf("%v: %v", size, err)
			}
		}
	}
	// a large dial's box reaches the subtitle, which must survive whole:
	// its last word, at the right of the line, still inked
	big := moonNight(time.Date(2026, 11, 18, 23, 2, 0, 0, loc), 6.1)
	big.Subtitle = "11:02 PM - 5:10 AM · 6h 9m in bed"
	img, err := MoonView(big, 1354, 990)
	if err != nil {
		t.Fatal(err)
	}
	inked := false
	for y := 48; y < 53 && !inked; y++ {
		for x := 140; x < 260 && !inked; x++ {
			inked = img.GrayAt(x, y).Y < 170
		}
	}
	if !inked {
		t.Error("the dial erased the end of the subtitle")
	}
	if _, err := MoonView(Night{}, 800, 480); err == nil {
		t.Error("expected an error for a night without an end")
	}
	if got, want := moonShadeOK(StageDeep, 0.05), true; got != want {
		t.Errorf("deep fills its ring edge to edge")
	}
	if moonShadeOK(StageAwake, 0.05) || !moonShadeOK(StageAwake, 0.5) {
		t.Error("awake is a hairline in the middle of its ring")
	}
}

func moonShadeOK(st Stage, t float64) bool {
	_, ok := moonShade(st, t)
	return ok
}

// Smoothing merges a brief flip into the stretch around it and keeps real
// stretches whole.
func TestSmoothStages(t *testing.T) {
	d, l := StageDeep, StageLight
	in := []Stage{d, d, d, d, l, d, d, d, d, l, l, l, l, l, l, l, l}
	got := smoothStages(in, 2)
	if got[4] != d {
		t.Errorf("a one-epoch flip survived: %v", got)
	}
	for i := 11; i < len(got); i++ {
		if got[i] != l {
			t.Errorf("the light stretch lost epoch %d: %v", i, got)
		}
	}
	if got := smoothStages(in, 0); &got[0] != &in[0] {
		t.Error("no window should leave the stages as they are")
	}
	if moonSmoothWindow(30*time.Second) != 5 || moonSmoothWindow(5*time.Minute) != 0 {
		t.Errorf("windows: %d %d", moonSmoothWindow(30*time.Second), moonSmoothWindow(5*time.Minute))
	}
}
