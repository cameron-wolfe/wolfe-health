package dashboard

import (
	"context"
	"fmt"
	"testing"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Nights are placed by their main sleep's bedtime against the next
// morning's score; naps, day sleeps and unscored nights are left out; the
// earliest third is compared with the latest.
func TestBedtimePayoff(t *testing.T) {
	ctx := context.Background()
	var samples []store.Sample
	var ps []store.SleepPeriod
	for i := 0; i < 30; i++ {
		wake := today.AddDate(0, 0, -i)
		eve := wake.AddDate(0, 0, -1)
		h, sc := 21, 85.0 // a third of nights at 9 PM score 85,
		switch i % 3 {
		case 1:
			h, sc = 22, 80 // a third at 10 PM score 80,
		case 2:
			h, sc = 23, 70 // a third at 11 PM score 70
		}
		ps = append(ps, store.SleepPeriod{ID: fmt.Sprint("n", i), Day: wake.Format(dayLayout), Type: "long_sleep",
			BedtimeStart: fmt.Sprintf("%sT%02d:00:00-08:00", eve.Format(dayLayout), h),
			BedtimeEnd:   wake.Format(dayLayout) + "T06:30:00-08:00", TimeInBed: 8 * 3600,
			TotalSleep: ptr((30 - h) * 3600)}) // 9 PM: 9h asleep; 10 PM: 8h; 11 PM: 7h
		samples = append(samples, store.Sample{Metric: store.MetricSleep, Day: wake.Format(dayLayout), Value: sc})
	}
	// a long day sleep starting at 9 AM, scored: not a bedtime
	ps = append(ps, store.SleepPeriod{ID: "day", Day: today.AddDate(0, 0, -40).Format(dayLayout), Type: "long_sleep",
		BedtimeStart: today.AddDate(0, 0, -40).Format(dayLayout) + "T09:00:00-08:00",
		BedtimeEnd:   today.AddDate(0, 0, -40).Format(dayLayout) + "T15:00:00-08:00", TimeInBed: 6 * 3600})
	samples = append(samples, store.Sample{Metric: store.MetricSleep, Day: today.AddDate(0, 0, -40).Format(dayLayout), Value: 99})
	db := openWith(t, samples)
	if err := db.SaveSleepPeriods(ctx, ps); err != nil {
		t.Fatal(err)
	}
	p, err := BedtimePayoff(ctx, db, 60, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Nights) != 30 || len(p.Slots) != 3 {
		t.Fatalf("%d nights, %d slots", len(p.Nights), len(p.Slots))
	}
	if p.Slots[0].Avg != 85 || p.Slots[2].Avg != 70 || p.Slots[0].N != 10 {
		t.Errorf("slots %+v", p.Slots)
	}
	// best: 9 PM (85); 10 PM (80) is more than 3 points off, so the window
	// is the 9 PM half hour, and the usual bedtime (10 PM, the median) is
	// half an hour late
	if !p.HasBest || p.BestStart != 180 || p.BestEnd != 210 {
		t.Errorf("best window %v %v–%v", p.HasBest, p.BestStart, p.BestEnd)
	}
	if p.Headline != "best bedtime 9:00–9:30 PM · usual 10:00 PM, late" {
		t.Errorf("headline %q", p.Headline)
	}
	got := map[string]string{}
	for _, st := range append(p.Stats, p.Compare...) {
		got[st.Label] = st.Value
	}
	for k, v := range map[string]string{
		"Best window": "9:00–9:30 PM", "Inside?": "no, 30m late",
		"Sleep score": "85 vs 70", "Asleep": "9h vs 7h",
	} {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
	if p.EarlyBy != "9:00 PM" || p.LateAfter != "11:00 PM" {
		t.Errorf("early by %q, late after %q", p.EarlyBy, p.LateAfter)
	}
	if _, err := BedtimePayoff(ctx, db, 7, today); err == nil {
		t.Error("expected an error for too short a window")
	}
}

func TestClockRange(t *testing.T) {
	for _, c := range []struct {
		a, b float64
		want string
	}{{120, 240, "8:00–10:00 PM"}, {330, 390, "11:30 PM–12:30 AM"}} {
		if got := clockRange(c.a, c.b); got != c.want {
			t.Errorf("clockRange(%v, %v) = %q, want %q", c.a, c.b, got, c.want)
		}
	}
}
