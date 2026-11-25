package dashboard

import (
	"context"
	"testing"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Each marker gets its 30-day average and the change from the window's
// first month, worded better or worse by the marker's direction: a rising
// VO2 max and sleep score are better, a falling cardio age is better.
func TestFitness(t *testing.T) {
	var samples []store.Sample
	for i := 0; i < 200; i++ {
		day := today.AddDate(0, 0, -i).Format(dayLayout)
		late := i < 100 // the recent 100 days
		vo2, cardio, sleep := 40.0, 40.0, 80.0
		if late {
			vo2, cardio, sleep = 43, 37, 74
		}
		samples = append(samples,
			store.Sample{Metric: store.MetricSleep, Day: day, Value: sleep},
			store.Sample{Metric: store.MetricCardioAge, Day: day, Value: cardio})
		if i%7 == 0 { // VO2 max comes about weekly
			samples = append(samples, store.Sample{Metric: store.MetricVO2Max, Day: day, Value: vo2})
		}
	}
	f, err := Fitness(context.Background(), openWith(t, samples), 180, today)
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Fitness and sleep · last 6 months" || len(f.Panels) != 3 { // no resting HR readings: left out
		t.Fatalf("title %q, %d panels", f.Title, len(f.Panels))
	}
	want := map[string][2]string{
		"VO2 max":     {"now 43", "+3.0 · better"},
		"Cardio age":  {"now 37", "−3 years · better"},
		"Sleep score": {"now 74", "−6 · worse"},
	}
	for _, p := range f.Panels {
		w, ok := want[p.Title]
		if !ok {
			t.Errorf("unexpected panel %q", p.Title)
			continue
		}
		if p.Value != w[0] || p.Change != w[1] {
			t.Errorf("%s: %q, %q; want %q, %q", p.Title, p.Value, p.Change, w[0], w[1])
		}
	}
	// with nothing at all, every panel stays, saying so
	if none, err := Fitness(context.Background(), openWith(t, nil), 365, today); err != nil || len(none.Panels) != 4 || none.Panels[0].Change != "no data yet" {
		t.Errorf("empty database: %d panels, %v", len(none.Panels), err)
	}
	if _, err := Fitness(context.Background(), openWith(t, nil), 30, today); err == nil {
		t.Error("expected an error for too short a window")
	}
}

// A marker whose readings stopped long ago gives its last value with the
// month, and when the last reading was, rather than "now".
func TestFitnessStaleMarker(t *testing.T) {
	var samples []store.Sample
	for i := 90; i < 150; i += 7 { // weekly, ending three months ago
		samples = append(samples, store.Sample{Metric: store.MetricVO2Max, Day: today.AddDate(0, 0, -i).Format(dayLayout), Value: 44})
	}
	f, err := Fitness(context.Background(), openWith(t, samples), 365, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Panels) != 1 || f.Panels[0].Value != "44 in Aug" || f.Panels[0].Detail != "last reading Aug 21" {
		t.Errorf("stale VO2 max: %+v", f.Panels)
	}
}

func TestSpanWords(t *testing.T) {
	for days, want := range map[int]string{365: "last 12 months", 730: "last 2 years", 182: "last 6 months", 90: "last 90 days", 200: "last 200 days"} {
		if got := spanWords(days); got != want {
			t.Errorf("spanWords(%d) = %q, want %q", days, got, want)
		}
	}
}
