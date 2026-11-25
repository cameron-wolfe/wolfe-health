package dashboard

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Each night's main sleep becomes its row; a nap that evening is extra
// sleep in the same row, an afternoon nap is left out; the usual bedtime
// and wake are medians; the best window comes from the nights' own scores.
func TestSleepSchedule(t *testing.T) {
	ctx := context.Background()
	db := openWith(t, nil)
	var ps []store.SleepPeriod
	sp := func(id, day, typ, start, end string) store.SleepPeriod {
		return store.SleepPeriod{ID: id, Day: day, Type: typ, BedtimeStart: start, BedtimeEnd: end, TimeInBed: 8 * 3600}
	}
	// a week of nights ending Nov 13-19: in bed 10:30 PM to 6:30 AM, but
	// the night ending Sat 14 runs an hour later and Sun 15 two hours later
	for i := 0; i < 7; i++ {
		wake := today.AddDate(0, 0, -i)
		eve := wake.AddDate(0, 0, -1)
		bedH := 22
		if wake.Day() == 14 {
			bedH = 23
		}
		start := fmt.Sprintf("%sT%02d:30:00-08:00", eve.Format(dayLayout), bedH)
		if wake.Day() == 15 {
			start = fmt.Sprintf("%sT00:30:00-08:00", wake.Format(dayLayout))
		}
		ps = append(ps, sp(fmt.Sprint("n", i), wake.Format(dayLayout), "long_sleep",
			start, wake.Format(dayLayout)+"T06:30:00-08:00"))
	}
	ps = append(ps,
		sp("doze", "2026-11-19", "sleep", "2026-11-18T20:00:00-08:00", "2026-11-18T20:30:00-08:00"),   // evening doze
		sp("nap", "2026-11-19", "late_nap", "2026-11-19T14:00:00-08:00", "2026-11-19T14:30:00-08:00"), // afternoon: not a night
	)
	if err := db.SaveSleepPeriods(ctx, ps); err != nil {
		t.Fatal(err)
	}
	// scores: the 10:30 PM nights 85, the two late ones 70, so the best
	// window is 10:30 to 11 PM
	var scores []store.Sample
	for i := 0; i < 7; i++ {
		wake := today.AddDate(0, 0, -i)
		v := 85.0
		if wake.Day() == 14 || wake.Day() == 15 {
			v = 70
		}
		scores = append(scores, store.Sample{Metric: store.MetricSleep, Day: wake.Format(dayLayout), Value: v})
	}
	if err := db.Save(ctx, scores); err != nil {
		t.Fatal(err)
	}

	s, err := SleepSchedule(ctx, db, 7, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.Nights) != 7 || !s.Nights[6].Day.Equal(today) {
		t.Fatalf("%d nights, last %v", len(s.Nights), s.Nights[len(s.Nights)-1].Day)
	}
	last := s.Nights[6]
	if !last.Has || last.Bed != 270 || last.Wake != 750 { // 10:30 PM, 6:30 AM
		t.Errorf("last night %+v", last)
	}
	if len(last.Extra) != 1 || last.Extra[0] != [2]float64{120, 150} {
		t.Errorf("extra sleep %+v; the evening doze only", last.Extra)
	}
	if !s.HasBest || s.BestStart != 270 || s.BestEnd != 300 || !last.InWindow || s.Nights[1].InWindow {
		t.Errorf("best window %v %v–%v; last inside %v, Saturday's inside %v", s.HasBest, s.BestStart, s.BestEnd, last.InWindow, s.Nights[1].InWindow)
	}
	if sat := s.Nights[1]; !sat.Weekend || sat.Bed != 330 {
		t.Errorf("Saturday's night %+v", sat)
	}
	if s.Headline != "bedtime 10:30 PM ± 0m" {
		t.Errorf("headline %q", s.Headline)
	}
	got := map[string]string{}
	for _, st := range s.Stats {
		got[st.Label] = st.Value
	}
	for k, v := range map[string]string{
		"Usual bedtime": "10:30 PM", "Usual wake": "6:30 AM", "Time in bed, avg": "7h 34m", // (5 × 8h + 7h + 6h) / 7
		"Weekend bedtime": "1h 30m later", "Best window": "10:30–11:00 PM", "Nights inside it": "5 of 7",
	} {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

func TestNightOf(t *testing.T) {
	loc := time.FixedZone("", -8*3600)
	for in, want := range map[time.Time]string{
		time.Date(2026, 11, 18, 22, 0, 0, 0, loc):  "2026-11-19",
		time.Date(2026, 11, 19, 1, 0, 0, 0, loc):   "2026-11-19",
		time.Date(2026, 11, 19, 11, 59, 0, 0, loc): "2026-11-19",
	} {
		if got := nightOf(in).Format(dayLayout); got != want {
			t.Errorf("nightOf(%v) = %s, want %s", in, got, want)
		}
	}
}
