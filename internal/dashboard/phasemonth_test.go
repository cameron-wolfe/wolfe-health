package dashboard

import (
	"context"
	"fmt"
	"testing"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// The month holds each night through last night, in the cell for the
// evening it began (Oura's day for it, less one), with the time asleep; the longest and shortest are marked once there are three
// nights; and the averages read bedtime across midnight.
func TestPhaseMonth(t *testing.T) {
	db := openWith(t, nil)
	ctx := context.Background()
	var ps []store.SleepPeriod
	for i, n := range []struct {
		day        int
		start, end string
		phases     string
	}{
		{16, "2026-11-15T23:30:00-08:00", "2026-11-16T07:00:00-08:00", "1122"},   // 2 epochs asleep... of 5m
		{17, "2026-11-17T00:30:00-08:00", "2026-11-17T06:00:00-08:00", "12"},     // past midnight
		{18, "2026-11-17T22:30:00-08:00", "2026-11-18T07:30:00-08:00", "112233"}, // the longest
	} {
		p := period(fmt.Sprint("p", i), "long_sleep", 30000)
		p.Day = fmt.Sprintf("2026-11-%d", n.day)
		p.BedtimeStart, p.BedtimeEnd = n.start, n.end
		p.Phases30Sec = nil
		p.Phases5Min = ptr(n.phases)
		ps = append(ps, p)
	}
	if err := db.SaveSleepPeriods(ctx, ps); err != nil {
		t.Fatal(err)
	}
	m, err := PhaseMonth(ctx, db, today, today)
	if err != nil {
		t.Fatal(err)
	}
	if m.Today != 19 || len(m.Nights) != 3 || m.PanelNote != "3 nights" {
		t.Fatalf("today %d, %d nights, note %q", m.Today, len(m.Nights), m.PanelNote)
	}
	if m.Asleep[17] != 30 || m.Asleep[16] != 10 { // woke the 18th: the night of the 17th

		t.Errorf("asleep = %v", m.Asleep)
	}
	if m.Best != 17 || m.Worst != 16 {
		t.Errorf("best %d, worst %d", m.Best, m.Worst)
	}
	// bedtimes 11:30 PM, 12:30 AM, 10:30 PM average 11:30 PM, not midday
	if m.Rows[0].Label != "Bedtime, avg" || m.Rows[0].Value != "11:30 PM" {
		t.Errorf("bedtime row = %+v", m.Rows[0])
	}
	for _, sz := range [][2]int{{800, 432}, {600, 300}} {
		if _, err := chart.PhaseMonthView(m, sz[0], sz[1]); err != nil {
			t.Fatal(err)
		}
	}

	// a past month has no nights to come, and an empty one still draws
	m, err = PhaseMonth(ctx, db, today.AddDate(0, -1, 0), today)
	if err != nil || m.Today != 0 || len(m.Nights) != 0 {
		t.Fatalf("last month: today %d, %d nights, %v", m.Today, len(m.Nights), err)
	}
	if _, err := chart.PhaseMonthView(m, 800, 432); err != nil {
		t.Fatal(err)
	}
}
