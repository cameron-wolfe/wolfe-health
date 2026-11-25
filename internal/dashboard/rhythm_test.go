package dashboard

import (
	"context"
	"testing"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// today (Thu Nov 19) is excluded from stress but kept for scores; weekdays
// index Monday first; best/worst respect "less is better" for stress; the
// scale never shrinks below its minimum.
func TestWeeklyRhythm(t *testing.T) {
	var s []store.Sample
	// Mon Nov 16 .. Thu Nov 19
	for i, v := range []float64{70, 80, 90, 100} {
		day := ago(3 - i)
		s = append(s, store.Sample{Metric: store.MetricSleep, Day: day, Value: v})
	}
	s = append(s,
		stress(ago(3), 60, "normal"),  // Mon 1h
		stress(ago(2), 120, "normal"), // Tue 2h
		stress(ago(0), 0, "normal"),   // Thu: today, partial
	)
	rh, err := WeeklyRhythm(context.Background(), openWith(t, s), 30, today)
	if err != nil {
		t.Fatal(err)
	}
	sleep, stressP := rh.Panels[0], rh.Panels[2]
	if !sleep.Has[0] || sleep.Values[0] != 70 || sleep.Values[3] != 100 || sleep.Has[4] {
		t.Errorf("sleep by weekday = %v %v", sleep.Values, sleep.Has)
	}
	if sleep.Avg != 85 || sleep.Caption != "best Thu 100 · worst Mon 70" {
		t.Errorf("sleep avg %v, caption %q", sleep.Avg, sleep.Caption)
	}
	if stressP.Has[3] {
		t.Error("today's partial stress should be excluded")
	}
	if stressP.Caption != "least Mon 60m · most Tue 120m" || !stressP.LowerIsBetter {
		t.Errorf("stress caption %q", stressP.Caption)
	}
	if stressP.Extent != 50*60 || stressP.ExtentLabel != "±50m" {
		t.Errorf("stress extent %v %q, want ±50m", stressP.Extent, stressP.ExtentLabel)
	}
	if rh.Panels[1].Caption != "no data" {
		t.Errorf("readiness with no data: %q", rh.Panels[1].Caption)
	}
	if _, err := chart.RhythmView(rh, 800, 480); err != nil {
		t.Fatal(err)
	}
}

// A tiny spread still gets the minimum ±5 scale, so it looks tiny.
func TestRhythmMinimumExtent(t *testing.T) {
	var s []store.Sample
	for i := 0; i < 7; i++ {
		s = append(s, store.Sample{Metric: store.MetricReadiness, Day: ago(i), Value: 80 + float64(i%2)})
	}
	rh, err := WeeklyRhythm(context.Background(), openWith(t, s), 30, today)
	if err != nil {
		t.Fatal(err)
	}
	if rh.Panels[1].Extent != 5 {
		t.Errorf("extent = %v, want the minimum 5", rh.Panels[1].Extent)
	}
}
