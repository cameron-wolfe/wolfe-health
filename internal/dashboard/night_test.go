package dashboard

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

func ptr[T any](v T) *T { return &v }

func period(id, typ string, inBed int) store.SleepPeriod {
	return store.SleepPeriod{
		ID: id, Day: "2026-11-19", Type: typ,
		BedtimeStart: "2026-11-18T23:02:20-08:00", BedtimeEnd: "2026-11-19T05:10:50-08:00",
		TimeInBed: inBed, TotalSleep: ptr(19140), Efficiency: ptr(87), Latency: ptr(1140),
		Phases5Min:  ptr("4221"),
		Phases30Sec: ptr("44221111x3"),
		Movement30s: ptr("1123412341"),
		HeartRate:   &store.Series{Interval: 300, Items: []*float64{ptr(60.0), nil, ptr(52.0)}, Start: "2026-11-18T23:02:20-08:00"},
	}
}

func TestDecodeStages(t *testing.T) {
	got := decodeStages("1234x")
	want := []chart.Stage{chart.StageDeep, chart.StageLight, chart.StageREM, chart.StageAwake, chart.StageUnknown}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("stage %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestMainPeriodPrefersLongSleepThenLongest(t *testing.T) {
	ps := []store.SleepPeriod{period("nap", "sleep", 9000), period("main", "long_sleep", 8000), period("frag", "sleep", 1000)}
	if p, _ := mainPeriod(ps); p.ID != "main" {
		t.Errorf("picked %s, want the long_sleep", p.ID)
	}
	ps = []store.SleepPeriod{period("a", "sleep", 1000), period("b", "sleep", 7800), period("c", "sleep", 2400)}
	if p, _ := mainPeriod(ps); p.ID != "b" {
		t.Errorf("picked %s, want the longest", p.ID)
	}
	if _, ok := mainPeriod(nil); ok {
		t.Error("no periods should find nothing")
	}
}

func TestNightFromStore(t *testing.T) {
	db := openWith(t, nil)
	ctx := context.Background()
	if err := db.SaveSleepPeriods(ctx, []store.SleepPeriod{
		period("main", "long_sleep", 22110), period("nap", "sleep", 2000),
	}); err != nil {
		t.Fatal(err)
	}
	n, err := Night(ctx, db, "") // latest day
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "Night of Wed, Nov 18" {
		t.Errorf("title = %q", n.Title)
	}
	if n.Subtitle != "11:02 PM – 5:10 AM · 6h 9m in bed · 1 of 2 periods" {
		t.Errorf("subtitle = %q", n.Subtitle)
	}
	if n.Headline != "5h 19m asleep" || n.Subline != "efficiency 87% · asleep in 19m" {
		t.Errorf("headline = %q / %q", n.Headline, n.Subline)
	}
	// the finer 30-second phases win over the 5-minute ones
	if n.StageEpoch != 30*time.Second || len(n.Stages) != 10 || n.Stages[8] != chart.StageUnknown {
		t.Errorf("stages: epoch %v, %d entries, [8]=%v", n.StageEpoch, len(n.Stages), n.Stages[8])
	}
	if n.HeartRate.Interval != 5*time.Minute || n.HeartRate.Values[1] != nil {
		t.Errorf("heart rate series = %+v", n.HeartRate)
	}
	if _, err := chart.NightView(n, 800, 480); err != nil {
		t.Fatal(err)
	}
}

func TestNightFallsBackToFiveMinutePhases(t *testing.T) {
	db := openWith(t, nil)
	p := period("main", "long_sleep", 22110)
	p.Phases30Sec = nil
	if err := db.SaveSleepPeriods(context.Background(), []store.SleepPeriod{p}); err != nil {
		t.Fatal(err)
	}
	n, err := Night(context.Background(), db, "2026-11-19")
	if err != nil {
		t.Fatal(err)
	}
	if n.StageEpoch != 5*time.Minute || len(n.Stages) != 4 {
		t.Errorf("epoch %v, %d stages", n.StageEpoch, len(n.Stages))
	}
}

func TestNightWithNoData(t *testing.T) {
	db := openWith(t, nil)
	if _, err := Night(context.Background(), db, ""); !errors.Is(err, ErrNoSleep) {
		t.Errorf("empty db: err = %v, want ErrNoSleep", err)
	}
	if _, err := Night(context.Background(), db, "2026-01-01"); !errors.Is(err, ErrNoSleep) {
		t.Errorf("missing day: err = %v, want ErrNoSleep", err)
	}
}
