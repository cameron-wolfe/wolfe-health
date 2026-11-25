package dashboard

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

func day(n int) time.Time { return today.AddDate(0, 0, -n) }

// The range is median ± 1.5 robust spreads; a few wild nights barely move
// it, where a mean and SD would swell.
func TestBaselineIsRobust(t *testing.T) {
	vals := map[time.Time]float64{}
	for i := 1; i <= 30; i++ {
		vals[day(i)] = 50 + float64(i%5) // 50..54, median 52
	}
	calm, ok := baseline(vals, today, 1)
	if !ok {
		t.Fatal("no baseline with 30 nights")
	}
	vals[day(1)], vals[day(2)], vals[day(3)] = 90, 95, 99 // a wild spell
	wild, _ := baseline(vals, today, 1)
	// A mean ± 1.5 SD range would grow from ~4 to ~45 wide here; the robust
	// one should stay within a few points of its calm width and center.
	if grew := (wild.Hi - wild.Lo) - (calm.Hi - calm.Lo); grew > 3 {
		t.Errorf("outliers widened the range by %.1f: calm %+v, wild %+v", grew, calm, wild)
	}
	if math.Abs((wild.Hi+wild.Lo)/2-52) > 1 {
		t.Errorf("outliers moved the center: %+v", wild)
	}
}

func TestBaselineNeedsHistoryAndHasAFloor(t *testing.T) {
	vals := map[time.Time]float64{}
	for i := 1; i < minBaseline; i++ {
		vals[day(i)] = 50
	}
	if _, ok := baseline(vals, today, 1); ok {
		t.Error("a range from too few nights")
	}
	vals[day(minBaseline)] = 50 // now enough, all identical
	b, ok := baseline(vals, today, 2)
	if !ok || b.Lo != 47 || b.Hi != 53 {
		t.Errorf("flat history: %+v, want 50 ± 1.5×2", b)
	}
}

// End to end: HRV and lowest HR come from each night's main period,
// temperature from the stored metric; the latest night is worded against
// its range, and nights outside are counted.
func TestBodySignals(t *testing.T) {
	var samples []store.Sample
	var periods []store.SleepPeriod
	for i := 0; i < 60; i++ {
		d := day(i).Format(dayLayout)
		temp := 0.0
		if i == 0 {
			temp = 0.9 // last night: feverish
		}
		samples = append(samples, store.Sample{Metric: store.MetricTempDev, Day: d, Value: temp})
		hrv, hr := 50+i%3, 50
		p := period("p"+d, "long_sleep", 25000)
		p.Day, p.AverageHRV, p.LowestHR = d, ptr(hrv), ptr(hr)
		periods = append(periods, p)
	}
	db := openWith(t, samples)
	if err := db.SaveSleepPeriods(context.Background(), periods); err != nil {
		t.Fatal(err)
	}
	sig, err := BodySignals(context.Background(), db, 30, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(sig.Panels) != 3 {
		t.Fatalf("%d panels", len(sig.Panels))
	}
	hrv, hr, temp := sig.Panels[0], sig.Panels[1], sig.Panels[2]
	if len(hrv.Points) != 30 || hrv.Status != "within your normal range" {
		t.Errorf("hrv: %d points, %q", len(hrv.Points), hrv.Status)
	}
	if hr.Value != "50 bpm" || hr.Detail != "normal 48 to 52" {
		t.Errorf("hr: %q %q", hr.Value, hr.Detail)
	}
	if temp.Value != "+0.90 °C" || temp.Status != "above your normal range" || temp.Note != "1 night outside in 30 days" {
		t.Errorf("temp: %q %q %q", temp.Value, temp.Status, temp.Note)
	}
	if _, err := chart.SignalsView(sig, 800, 450); err != nil {
		t.Fatal(err)
	}
}

func TestBodySignalsEmpty(t *testing.T) {
	sig, err := BodySignals(context.Background(), openWith(t, nil), 60, today)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range sig.Panels {
		if p.Value != "—" || p.Status != "no data yet" {
			t.Errorf("empty panel %+v", p)
		}
	}
	if _, err := chart.SignalsView(sig, 800, 450); err != nil {
		t.Fatal(err)
	}
}
