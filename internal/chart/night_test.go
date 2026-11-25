package chart

import (
	"image"
	"testing"
	"time"
)

var bed = time.Date(2026, 11, 18, 23, 0, 0, 0, time.UTC)

func TestRunsCollapseAndKeepGaps(t *testing.T) {
	s := []Stage{StageAwake, StageAwake, StageLight, StageUnknown, StageLight, StageDeep, StageDeep}
	got := runs(s)
	want := []run{{StageAwake, 0, 2}, {StageLight, 2, 3}, {StageLight, 4, 5}, {StageDeep, 5, 7}}
	if len(got) != len(want) {
		t.Fatalf("runs = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("run %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestStageTotalsSkipUnknown(t *testing.T) {
	s := []Stage{StageDeep, StageDeep, StageUnknown, StageREM}
	tot := stageTotals(s, 30*time.Second)
	if tot[StageDeep] != time.Minute || tot[StageREM] != 30*time.Second || tot[StageUnknown] != 0 {
		t.Errorf("totals = %v", tot)
	}
}

// A run narrower than a pixel must still ink something, or a 30-second
// awakening would vanish into a gap in the dotted pattern.
func TestNarrowBlockStillDrawn(t *testing.T) {
	img := image.NewGray(image.Rect(0, 0, 50, 50))
	fillRect(img, img.Bounds(), surface)
	// x = 2 falls in the awake pattern's gap (x%4 >= 2)
	drawBlock(img, StageAwake, 2.1, 2.4, 25)
	inked := false
	for y := 0; y < 50; y++ {
		if img.GrayAt(2, y).Y == ink.Y {
			inked = true
		}
	}
	if !inked {
		t.Error("sub-pixel awake run drew nothing")
	}
}

func testNight(stages []Stage) Night {
	return Night{
		Title: "Night", Start: bed, End: bed.Add(time.Duration(len(stages)) * 30 * time.Second),
		StageEpoch: 30 * time.Second, Stages: stages,
	}
}

// Deep is a solid bar in the bottom lane; the lane above it, light, is
// hatched, so its center row is not solid ink.
func TestNightViewInksStagesInTheirLanes(t *testing.T) {
	stages := make([]Stage, 480) // 4 hours
	for i := range stages {
		stages[i] = StageLight
		if i >= 120 && i < 240 {
			stages[i] = StageDeep
		}
	}
	n := testNight(stages)
	img, err := NightView(n, 800, 480)
	if err != nil {
		t.Fatal(err)
	}
	lanes, _, _ := nightLayout(800, 480)
	plotL, plotR, top := lanes.Min.X, lanes.Max.X, lanes.Min.Y
	laneH := float64(lanes.Dy()) / 4
	deepMid := top + int(laneH*3.5)
	tx := timeAxis{start: n.Start, end: n.End, left: float64(plotL), right: float64(plotR)}
	// sample near the block's start: its center carries the white duration label
	deepX := int(tx.x(bed.Add(65 * time.Minute)))
	if g := img.GrayAt(deepX, deepMid).Y; g != ink.Y {
		t.Errorf("deep block center: gray %d, want ink", g)
	}
	// light's hatch leaves most interior pixels white
	lightMid := top + int(laneH*2.5)
	lightX := int(tx.x(bed.Add(30 * time.Minute)))
	white := 0
	for dx := 0; dx < 8; dx++ {
		if img.GrayAt(lightX+dx, lightMid).Y != ink.Y {
			white++
		}
	}
	if white < 4 {
		t.Errorf("light block looks solid (%d/8 non-ink); want hatched", white)
	}
}

func TestNightViewRejectsBadInput(t *testing.T) {
	n := testNight([]Stage{StageLight})
	if _, err := NightView(n, 300, 200); err == nil {
		t.Error("expected an error for a tiny image")
	}
	n.End = n.Start
	if _, err := NightView(n, 800, 480); err == nil {
		t.Error("expected an error for an empty period")
	}
}

// Panels with no readings, and a night with no stages, still render.
func TestNightViewCopesWithMissingData(t *testing.T) {
	n := Night{Start: bed, End: bed.Add(7 * time.Hour)}
	if _, err := NightView(n, 800, 480); err != nil {
		t.Fatal(err)
	}
}

func TestTimeAxisTicks(t *testing.T) {
	a := timeAxis{start: bed.Add(2 * time.Minute), end: bed.Add(6*time.Hour + 10*time.Minute), left: 0, right: 600}
	ticks := a.ticks()
	if len(ticks) != 6 || ticks[0].Hour() != 0 || ticks[5].Hour() != 5 {
		t.Errorf("hourly ticks = %v", ticks)
	}
	// a long period in a narrow plot thins to every few hours
	long := timeAxis{start: bed, end: bed.Add(20 * time.Hour), left: 0, right: 300}
	for _, tk := range long.ticks() {
		if tk.Hour()%2 != 0 {
			t.Errorf("crowded axis kept odd hour %v", tk)
		}
	}
}

func TestHM(t *testing.T) {
	for d, want := range map[time.Duration]string{
		0: "0m", 48 * time.Minute: "48m", 87 * time.Minute: "1h 27m", 2 * time.Hour: "2h",
	} {
		if got := hm(d); got != want {
			t.Errorf("hm(%v) = %q, want %q", d, got, want)
		}
	}
}

// stagesFrom builds 30-second stages from (stage, epochs) pairs.
func stagesFrom(parts ...any) []Stage {
	var out []Stage
	for i := 0; i < len(parts); i += 2 {
		for j := 0; j < parts[i+1].(int); j++ {
			out = append(out, parts[i].(Stage))
		}
	}
	return out
}

// Interruptions of up to a minute join runs into one episode; longer ones
// do not. inStage counts only the epochs actually in the stage.
func TestEpisodesJoinBriefInterruptions(t *testing.T) {
	s := stagesFrom(StageREM, 20, StageAwake, 2, StageREM, 20, StageLight, 3, StageREM, 10)
	var rem []episode
	for _, e := range episodes(runs(s), 2) {
		if e.stage == StageREM {
			rem = append(rem, e)
		}
	}
	if len(rem) != 2 {
		t.Fatalf("got %d REM episodes, want 2: %+v", len(rem), rem)
	}
	if rem[0].start != 0 || rem[0].end != 42 || rem[0].inStage != 40 {
		t.Errorf("first episode = %+v, want span 0-42 with 40 in stage", rem[0])
	}
	if rem[1].inStage != 10 {
		t.Errorf("second episode = %+v", rem[1])
	}
}

func labelsFor(t *testing.T, s []Stage, pxPerEpoch float64) []blockLabel {
	t.Helper()
	x := func(i int) float64 { return float64(i) * pxPerEpoch }
	width := func(text string) int { return 20 } // every label 20px wide
	return chooseLabels(runs(s), 30*time.Second, x, width, 0, float64(len(s))*pxPerEpoch)
}

// The threshold is time, not pixels: a 10-minute episode is labeled however
// narrow it is drawn, a 9.5-minute one never, and light is never labeled.
func TestLabelThresholdIsTime(t *testing.T) {
	s := stagesFrom(StageLight, 40, StageDeep, 20, StageLight, 40, StageDeep, 19, StageLight, 40)
	got := labelsFor(t, s, 0.5) // the 10-minute block is only 10px wide
	if len(got) != 1 || got[0].text != "10m" || got[0].stage != StageDeep {
		t.Errorf("labels = %+v, want just the 10m deep block", got)
	}
}

// A blip-split REM stretch gets one label for its time in REM.
func TestLabelSpansBlips(t *testing.T) {
	s := stagesFrom(StageLight, 10, StageREM, 14, StageAwake, 1, StageREM, 14, StageLight, 10)
	got := labelsFor(t, s, 2)
	if len(got) != 1 || got[0].text != "14m" {
		t.Errorf("labels = %+v, want one 14m REM label", got)
	}
}

// When two labels would collide, the longer episode keeps its label.
func TestLabelCollisionKeepsLonger(t *testing.T) {
	s := stagesFrom(StageDeep, 20, StageLight, 3, StageDeep, 30)
	got := labelsFor(t, s, 0.5) // centers ~13px apart, labels 20px wide
	if len(got) != 1 || got[0].text != "15m" {
		t.Errorf("labels = %+v, want only the longer 15m", got)
	}
}
