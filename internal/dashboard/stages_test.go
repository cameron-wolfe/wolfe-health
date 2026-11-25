package dashboard

import (
	"context"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Stage times count the 30-second phases when present, fall back to Oura's
// durations, and skip a period with neither.
func TestStageTimes(t *testing.T) {
	p := period("a", "long_sleep", 600)
	p.Phases30Sec = ptr("1122233344") // 2 deep, 3 light, 3 REM, 2 awake
	n, ok := stageTimes(p)
	if !ok || n.Deep != time.Minute || n.Light != 90*time.Second || n.REM != 90*time.Second || n.Awake != time.Minute {
		t.Errorf("from 30s phases: %+v ok=%v", n, ok)
	}

	p.Phases30Sec, p.Phases5Min = nil, nil
	p.DeepSleep, p.LightSleep, p.RemSleep, p.AwakeTime = ptr(600), ptr(1200), ptr(300), nil
	n, ok = stageTimes(p)
	if !ok || n.Deep != 10*time.Minute || n.Light != 20*time.Minute || n.REM != 5*time.Minute || n.Awake != 0 {
		t.Errorf("from durations: %+v ok=%v", n, ok)
	}

	p.DeepSleep, p.LightSleep, p.RemSleep = nil, nil, nil
	if _, ok := stageTimes(p); ok {
		t.Error("a period with no stage data should be skipped")
	}
}

// Each day's bar is its main period, the same choice the night view makes;
// a nap with more stage time does not replace the main sleep.
func TestStageNightsUsesMainPeriod(t *testing.T) {
	db := openWith(t, nil)
	main := period("main", "long_sleep", 22110)
	main.Phases30Sec = ptr("1111")
	nap := period("nap", "sleep", 30000)
	nap.Phases30Sec = ptr("22222222")
	other := period("y", "long_sleep", 20000)
	other.Day = ago(1)
	other.Phases30Sec = ptr("33")
	if err := db.SaveSleepPeriods(context.Background(), []store.SleepPeriod{main, nap, other}); err != nil {
		t.Fatal(err)
	}
	sn, err := StageNights(context.Background(), db, 30, today)
	if err != nil {
		t.Fatal(err)
	}
	if len(sn.Nights) != 2 {
		t.Fatalf("%d nights, want 2", len(sn.Nights))
	}
	last := sn.Nights[1]
	if !last.Day.Equal(today) || last.Deep != 2*time.Minute || last.Light != 0 {
		t.Errorf("today's bar = %+v, want the long_sleep's 2m deep", last)
	}
	if _, err := chart.StageNightsView(sn, 800, 480); err != nil {
		t.Fatal(err)
	}
}
