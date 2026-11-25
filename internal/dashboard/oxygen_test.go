package dashboard

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Thirty steady nights build a normal range; a night well below it is low
// for the user and labeled low tide, and only a night with more dips than
// usual is marked; the 7-night average leads, with its
// two-week direction.
func TestOxygenTide(t *testing.T) {
	var samples []store.Sample
	for i := 0; i < 60; i++ {
		d := today.AddDate(0, 0, -i)
		v := 95.0 + float64(i%3-1)*0.5 // 94.5, 95, 95.5
		if i < 7 {
			v -= 1 // the last week dips a point
		}
		if i == 10 {
			v = 91.0 // one low night
		}
		samples = append(samples, store.Sample{Metric: store.MetricSpO2, Day: d.Format(dayLayout), Value: v})
	}
	// dips: mostly 0 or 1 a night (usual, so unmarked), 3 on Nov 18 (still
	// within the usual: median 1, and the range's floor puts its top at 4), and 5 on the
	// low night
	for i := 0; i < 60; i++ {
		v := float64(i % 2)
		switch i {
		case 1:
			v = 3
		case 10:
			v = 5
		}
		samples = append(samples, store.Sample{Metric: store.MetricBreathingDI,
			Day: today.AddDate(0, 0, -i).Format(dayLayout), Value: v})
	}
	o, err := OxygenTide(context.Background(), openWith(t, samples), 28, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(o.SpO2) != 28 || len(o.Smooth) != 28 || len(o.Band) != 28 {
		t.Fatalf("%d nights, %d smoothed, %d banded", len(o.SpO2), len(o.Smooth), len(o.Band))
	}
	if !o.HasLow || o.Low.Value != 91 || o.Low.Day != today.AddDate(0, 0, -10) {
		t.Errorf("low tide %v %+v", o.HasLow, o.Low)
	}
	if len(o.BDI) != 1 || o.BDI[0].Value != 5 {
		t.Errorf("dip marks %+v; only the night with more than usual", o.BDI)
	}
	if want := "93.9% · down 1.2 in 2 weeks"; o.Headline != want {
		t.Errorf("headline %q, want %q", o.Headline, want)
	}
	got := map[string]string{}
	for _, st := range o.Stats {
		got[st.Label] = st.Value
	}
	for k, v := range map[string]string{
		"Last night": "93.5%", "7-night average": "93.9%", // trailing: 93.5, 94, 94.5, 93.5
		"Extra-dip nights": "1 of 28", "Dips a night, avg": "0.8", // 21 dips / 28 nights
		"Crest at": "5+ dips", // above median 1 + 1.5 × floor 2 = 4
	} {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if got["Low for you"] == "" || got["Your normal"] == "" {
		t.Errorf("stats %v", got)
	}
	if _, err := OxygenTide(context.Background(), openWith(t, nil), 3, today); err == nil {
		t.Error("expected an error for too few days")
	}
}

// Fewer than three readings in a window leave a gap; the average doesn't run
// on past the last reading.
func TestSmoothed(t *testing.T) {
	d := func(n int) time.Time { return today.AddDate(0, 0, n) }
	vals := monthAvg{d(0): 94, d(1): 96, d(2): 95, d(10): 90}
	s := smoothed(vals, d(0), d(12))
	if v, ok := s[d(1)]; !ok || v != 95 {
		t.Errorf("day 1 = %v %v, want 95", v, ok)
	}
	if _, ok := s[d(10)]; ok {
		t.Error("a lone reading shouldn't make an average")
	}
	if _, ok := s[d(12)]; ok {
		t.Error("no average past the last reading")
	}
}

func TestTrendWords(t *testing.T) {
	cur := chart.Point{Day: today, Value: 95}
	for then, want := range map[float64]string{95.2: "steady over 2 weeks", 95.8: "down 0.8 in 2 weeks", 94.5: "up 0.5 in 2 weeks"} {
		s := monthAvg{today.AddDate(0, 0, -trendDays): then}
		if got := trendWords(s, cur); got != want {
			t.Errorf("from %v: %q, want %q", then, got, want)
		}
	}
	if got := trendWords(monthAvg{}, cur); got != "7-night average" {
		t.Errorf("no history: %q", got)
	}
}

func TestWeeklyAverages(t *testing.T) {
	vals := monthAvg{}
	for i := 0; i < 10; i++ { // Sun Nov 8 to Tue Nov 17
		vals[time.Date(2026, 11, 8+i, 0, 0, 0, 0, time.UTC)] = 94 + float64(i%2)
	}
	w := weeklyAverages(vals, time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC), today)
	if len(w) != 2 || w[0].Start.Day() != 8 || fmt.Sprintf("%.2f", w[0].Value) != "94.43" || w[1].Start.Day() != 15 {
		t.Errorf("weeks %+v", w)
	}
}
