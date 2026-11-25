package chart

import (
	"image"
	"testing"
	"time"
)

// The brief draws with nothing to show, and at its smallest with
// everything; it refuses a panel too small for its banks.
func TestBriefView(t *testing.T) {
	empty := Daily{Date: time.Date(2026, 11, 21, 0, 0, 0, 0, time.UTC)}
	empty.Hero.Value = "—"
	for i := range empty.Metrics {
		empty.Metrics[i].Value = "—"
	}
	if _, err := BriefView(empty, 800, 432); err != nil {
		t.Fatal(err)
	}
	full := empty
	full.Hero = Metric{Label: "Readiness", Value: "90", Range: Range{Has: true, Low: 51, High: 95, Typical: 80, Now: 90, LowLabel: "51", HighLabel: "95"}}
	full.Verdict, full.VerdictDetail, full.HeldBack = "Above your usual", "2 over your 7-day average", "Held back by: Timing 54 · REM 60"
	full.Rings, full.HasRings, full.CalNote = WeekDay{Sleep: 88, HasSleep: true, Readiness: 90, HasReadiness: true}, true, "391 of 700 cal"
	for i := 1; i < 4; i++ {
		full.Metrics[i] = Metric{Label: "Steps", Value: "5,071", Delta: "+77 vs 7-day avg", Note: "yesterday",
			Range: Range{Has: true, Low: 1295, High: 22991, Typical: 6000, Now: 5071, LowLabel: "1,295", HighLabel: "22,991"}}
	}
	start := time.Date(2026, 11, 20, 23, 13, 0, 0, time.UTC)
	full.Phase = Night{Start: start, End: start.Add(9 * time.Hour), StageEpoch: 5 * time.Minute,
		Stages: []Stage{StageAwake, StageLight, StageDeep, StageDeep, StageREM, StageLight}}
	full.HasPhase = true
	for _, sz := range [][2]int{{800, 432}, {600, 340}, {1200, 760}} {
		if _, err := BriefView(full, sz[0], sz[1]); err != nil {
			t.Fatalf("%v: %v", sz, err)
		}
	}
	// framed, it names its banks as zones, placed under the masthead and
	// side by side; without today's rings, there's no today zone
	img, zones, err := BrandedFrameZones("Daily brief", "", Footer{Left: "ring", Right: "now"}, 800, 480,
		func(w, h int) (*image.Gray, error) { return BriefView(full, w, h) })
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string]image.Rectangle{}
	for _, z := range zones {
		byName[z.Name] = z.Rect
	}
	rd, nt, td := byName["readiness"], byName["night"], byName["today"]
	if len(zones) != 3 || rd.Min.Y < mastheadHeight(800, 480) || !rd.In(img.Bounds()) ||
		rd.Max.X > nt.Min.X || nt.Max.X > td.Min.X || td.Dy() >= nt.Dy() {
		t.Errorf("zones %v", zones)
	}
	if _, zones, _ := BrandedFrameZones("", "", Footer{}, 800, 480,
		func(w, h int) (*image.Gray, error) { return BriefView(empty, w, h) }); len(zones) != 2 {
		t.Errorf("no rings: zones %v", zones)
	}
	if zoneSink != nil {
		t.Error("the zone sink was left set")
	}

	// the night's sleep score sits large at the right of the time-asleep
	// line, in the right half of the last-night bank
	without, err := BriefView(full, 800, 432)
	if err != nil {
		t.Fatal(err)
	}
	full.Metrics[0] = Metric{Label: "Sleep", Value: "88", Delta: "+5 vs 7-day avg"}
	with, err := BriefView(full, 800, 432)
	if err != nil {
		t.Fatal(err)
	}
	changed := 0
	for y := 66; y < 420; y++ {
		for x := 400; x < 526; x++ {
			if with.GrayAt(x, y) != without.GrayAt(x, y) {
				changed++
			}
		}
	}
	if changed < 60 {
		t.Errorf("no sleep score in the last-night bank: %d pixels changed", changed)
	}
	if _, err := BriefView(full, 500, 300); err == nil {
		t.Error("a 500x300 panel should be refused")
	}
}
