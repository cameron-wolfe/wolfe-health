package dashboard

import (
	"context"
	"testing"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// The period's average against the period before, the typical range from the
// middle half of days, a 7-day average line, and steps leaving out today.
func TestTrends(t *testing.T) {
	var samples []store.Sample
	for i := 0; i < 28; i++ { // 14 days this period, 14 before
		v := 80.0
		if i >= 14 {
			v = 70
		}
		samples = append(samples,
			store.Sample{Metric: store.MetricSleep, Day: ago(i), Value: v + float64(i%4)},
			store.Sample{Metric: store.MetricSteps, Day: ago(i), Value: 5000})
	}
	samples = append(samples, store.Sample{Metric: store.MetricSteps, Day: ago(0), Value: 99999}) // today: partial
	tr, err := Trends(context.Background(), openWith(t, samples), 14, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(tr.Panels) != 3 || tr.Panels[0].Title != "Sleep score" || tr.Panels[2].Title != "Steps" {
		t.Fatalf("panels %+v", tr.Panels)
	}
	sleep, readiness, steps := tr.Panels[0], tr.Panels[1], tr.Panels[2]
	if sleep.Value != "avg 81" || sleep.Change != "+10 vs the 14 days before" || !sleep.HasBand {
		t.Errorf("sleep: %q %q band=%v", sleep.Value, sleep.Change, sleep.HasBand)
	}
	if sleep.BandLo < 80 || sleep.BandHi > 83 || len(sleep.Average) != 14 {
		t.Errorf("sleep band %.1f-%.1f, %d average points", sleep.BandLo, sleep.BandHi, len(sleep.Average))
	}
	if readiness.Value != "—" || readiness.Change != "no data yet" {
		t.Errorf("readiness with no data: %+v", readiness)
	}
	if steps.Value != "avg 5,000" || len(steps.Points) != 13 {
		t.Errorf("steps: %q with %d points (today's partial day should be left out)", steps.Value, len(steps.Points))
	}
	if _, err := chart.TrendsView(tr, 800, 450); err != nil {
		t.Fatal(err)
	}
}

func TestQuantile(t *testing.T) {
	xs := []float64{1, 2, 3, 4, 5}
	if quantile(xs, 0.25) != 2 || quantile(xs, 0.75) != 4 || quantile(xs, 0.5) != 3 {
		t.Errorf("quantiles of 1..5: %v %v %v", quantile(xs, 0.25), quantile(xs, 0.5), quantile(xs, 0.75))
	}
}
