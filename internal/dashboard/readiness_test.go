package dashboard

import (
	"context"
	"testing"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// The readiness radar shows the latest day's parts in a fixed order
// against the days before it, leaves off a part never reported, and names
// a day other than today.
func TestReadinessBreakdown(t *testing.T) {
	part := func(key, day string, v float64) store.Sample {
		return store.Sample{Metric: store.MetricReadinessPart + key, Day: day, Value: v}
	}
	db := openWith(t, []store.Sample{
		{Metric: store.MetricReadiness, Day: ago(1), Value: 81},
		part("hrv_balance", ago(3), 70), part("hrv_balance", ago(2), 80), part("hrv_balance", ago(1), 60),
		part("previous_night", ago(2), 90), part("previous_night", ago(1), 95),
		part("activity_balance", ago(2), 50), // not reported on the day shown
		part("hrv_balance", ago(40), 0),      // outside the window
	})
	r, err := ReadinessBreakdown(context.Background(), db, 30, today)
	if err != nil {
		t.Fatal(err)
	}
	if r.Title != "WolfeSense · Readiness · Nov 18" || r.Score != "81" || r.PanelHead != "ON NOV 18" {
		t.Errorf("title %q, score %q, head %q", r.Title, r.Score, r.PanelHead)
	}
	want := []string{"Previous night", "HRV balance", "Activity balance"}
	if len(r.Parts) != len(want) {
		t.Fatalf("parts = %+v", r.Parts)
	}
	for i, p := range r.Parts {
		if p.Label != want[i] {
			t.Errorf("part %d is %s, want %s", i, p.Label, want[i])
		}
	}
	if hrv := r.Parts[1]; hrv.Value != 60 || hrv.Avg != 75 {
		t.Errorf("HRV %v, avg %v: the average leaves out the day shown", hrv.Value, hrv.Avg)
	}
	if act := r.Parts[2]; act.Has || !act.HasAvg {
		t.Errorf("activity balance: %+v", act)
	}
	if _, err := chart.RadarView(r, 800, 432); err != nil {
		t.Fatal(err)
	}

	empty, err := ReadinessBreakdown(context.Background(), openWith(t, nil), 30, today)
	if err != nil || len(empty.Parts) != 8 || empty.Parts[0].Has {
		t.Fatalf("empty: %+v, %v", empty, err)
	}
	if _, err := chart.RadarView(empty, 800, 432); err != nil {
		t.Fatal(err)
	}
}
