package dashboard

import (
	"context"
	"testing"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// The radar shows the latest night's parts in a fixed order against the
// average of the nights before it (not including itself); a part with no
// data says so; nights outside the window are ignored.
func TestSleepBreakdown(t *testing.T) {
	db := openWith(t, []store.Sample{{Metric: store.MetricSleep, Day: ago(0), Value: 66}})
	n := func(day string, rem, deep int) store.SleepContributors {
		hundred := 100
		return store.SleepContributors{Day: day, RemSleep: ptr(rem), DeepSleep: ptr(deep),
			TotalSleep: &hundred, Efficiency: &hundred, Restfulness: &hundred, Latency: &hundred}
		// Timing left nil throughout
	}
	if err := db.SaveSleepContributors(context.Background(), []store.SleepContributors{
		n(ago(2), 40, 80), n(ago(1), 60, 90), n(ago(0), 50, 70),
		n(ago(40), 0, 0), // outside the window: ignored
	}); err != nil {
		t.Fatal(err)
	}
	r, err := SleepBreakdown(context.Background(), db, 30, today)
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "WolfeSense · Sleep · last night" || r.Score != "66" || r.AvgLabel != "30-night average" {
		t.Errorf("title %q, score %q, avg label %q", r.Title, r.Score, r.AvgLabel)
	}
	want := []string{"Total sleep", "Efficiency", "Restfulness", "REM sleep", "Deep sleep", "Latency", "Timing"}
	if len(r.Parts) != len(want) {
		t.Fatalf("%d parts", len(r.Parts))
	}
	for i, p := range r.Parts {
		if p.Label != want[i] {
			t.Errorf("part %d is %s, want %s (fixed order)", i, p.Label, want[i])
		}
	}
	rem, deep, timing := r.Parts[3], r.Parts[4], r.Parts[6]
	if rem.Value != 50 || rem.Avg != 50 || deep.Value != 70 || deep.Avg != 85 {
		t.Errorf("REM %v/avg %v, deep %v/avg %v: the average leaves out the night shown", rem.Value, rem.Avg, deep.Value, deep.Avg)
	}
	if timing.Has || timing.HasAvg {
		t.Errorf("timing has no data: %+v", timing)
	}
	if _, err := chart.RadarView(r, 800, 450); err != nil {
		t.Fatal(err)
	}
	if _, err := chart.RadarView(r, 640, 354); err != nil {
		t.Fatal(err)
	}
}

func TestSleepBreakdownEmpty(t *testing.T) {
	r, err := SleepBreakdown(context.Background(), openWith(t, nil), 30, today)
	if err != nil {
		t.Fatal(err)
	}
	if r.Subtitle != "No sleep recorded yet" || len(r.Parts) != 7 {
		t.Errorf("empty: %q, %d parts", r.Subtitle, len(r.Parts))
	}
	if _, err := chart.RadarView(r, 800, 450); err != nil {
		t.Fatal(err)
	}
}
