package dashboard

import (
	"context"
	"testing"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// The week's averages are placed against the weeks before, turned so
// outward is better; a week well below usual on a measure is worse.
func TestWeekRadar(t *testing.T) {
	start := WeekStart(today).AddDate(0, 0, -7) // last complete week: Nov 8-14
	var samples []store.Sample
	for i := 0; i < 70; i++ {
		day := start.AddDate(0, 0, 6-i)
		sleep := []float64{80, 82, 84}[i%3]
		stress := []float64{3000, 3600, 4200}[i%3]
		if i < 7 { // this week: sleep well down, stress well down (better)
			sleep, stress = 70, 600
		}
		samples = append(samples,
			store.Sample{Metric: store.MetricSleep, Day: day.Format(dayLayout), Value: sleep},
			store.Sample{Metric: store.MetricStress, Day: day.Format(dayLayout), Value: stress})
	}
	w, err := WeekRadar(context.Background(), openWith(t, samples), start, today)
	if err != nil {
		t.Fatal(err)
	}
	if w.Title != "WolfeSense · Week · Nov 8 – 14" || len(w.Spokes) != 8 {
		t.Fatalf("title %q, %d spokes", w.Title, len(w.Spokes))
	}
	sleep, stress := w.Spokes[0], w.Spokes[7]
	if !sleep.Has || sleep.D >= -1 || !sleep.Alert || sleep.Text != "70" || !sleep.HasAvg {
		t.Errorf("sleep spoke %+v", sleep)
	}
	if stress.Label != "High stress" || !stress.Has || stress.D <= 1 || stress.Alert {
		t.Errorf("stress spoke %+v: lower stress should sit outward, not alert", stress)
	}
	if w.Headline != "1 better than usual, 1 worse" {
		t.Errorf("headline %q", w.Headline)
	}
	if empty, _ := WeekRadar(context.Background(), openWith(t, nil), start, today); empty.Headline != "not enough weeks yet" {
		t.Errorf("empty headline %q", empty.Headline)
	}
}
