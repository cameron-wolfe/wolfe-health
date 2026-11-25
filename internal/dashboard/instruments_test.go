package dashboard

import (
	"context"
	"math"
	"strings"
	"testing"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Each bar reads last night against zones: personal ones from the nights
// before for body signals, Oura's bands for scores; a signal with no
// readings has no pointer.
func TestInstruments(t *testing.T) {
	var samples []store.Sample
	for i := 0; i < 40; i++ {
		day := today.AddDate(0, 0, -i).Format(dayLayout)
		v := []float64{-0.1, 0, 0.1}[i%3]
		if i == 0 {
			v = 0.8 // last night: well above normal
		}
		samples = append(samples,
			store.Sample{Metric: store.MetricTempDev, Day: day, Value: v},
			store.Sample{Metric: store.MetricSleep, Day: day, Value: 78})
	}
	p, err := Instruments(context.Background(), openWith(t, samples), today)
	if err != nil {
		t.Fatal(err)
	}
	if !p.Night.Equal(today) || len(p.Gauges) != 8 {
		t.Fatalf("night %v, %d gauges", p.Night, len(p.Gauges))
	}
	temp, hrv, sleep := p.Gauges[0], p.Gauges[1], p.Gauges[3]
	if temp.Label != "Temp" || !temp.Has || !temp.HasBand || temp.Value <= temp.CautionHi || temp.Min > temp.Lo || temp.Max < 0.8 {
		t.Errorf("temp gauge %+v", temp)
	}
	if hrv.Has || hrv.HasBand {
		t.Errorf("HRV with no readings: %+v", hrv)
	}
	if !sleep.Has || sleep.Lo != 85 || sleep.CautionLo != 70 || sleep.Min != 50 {
		t.Errorf("sleep gauge %+v", sleep)
	}
	if !p.Gauges[6].Round || p.Gauges[6].Label != "Respiration" {
		t.Errorf("center readouts %+v", p.Gauges[6:])
	}
}

// On a clean night the status box gets the standouts: optimal scores, the
// week's best, HRV in its upper half, pulse in its lower half.
func TestVitalNotes(t *testing.T) {
	nan := math.NaN()
	gs := []chart.Gauge{
		{Label: "HRV", Value: 57, Has: true, HasBand: true, Lo: 40, Hi: 60},
		{Label: "Sleep", Value: 87, Has: true, HasBand: true, Lo: 85, Hi: 100, Recent: []float64{80, nan, 86, 87}},
		{Label: "Readiness", Value: 92, Has: true, HasBand: true, Lo: 85, Hi: 100, Recent: []float64{95, 92}},
		{Label: "Pulse", Value: 47, Has: true, HasBand: true, Lo: 46, Hi: 53},
	}
	got := strings.Join(vitalNotes(gs), "; ")
	// sleep 87 is also the week's best, and four notes is the most: pulse
	// doesn't make the cut
	want := "HRV 57 · UPPER HALF; SLEEP 87 · OPTIMAL; BEST SLEEP THIS WEEK; READINESS 92 · OPTIMAL"
	if got != want {
		t.Errorf("notes %q\n want %q", got, want)
	}
}

// Time asleep's scale stops at 14 hours however varied the record.
func TestInstrumentsAsleepCap(t *testing.T) {
	ctx := context.Background()
	db := openWith(t, nil)
	var ps []store.SleepPeriod
	for i := 0; i < 35; i++ {
		day := today.AddDate(0, 0, -i).Format(dayLayout)
		hours := []int{3, 7, 11}[i%3] // a wildly varied record
		ps = append(ps, store.SleepPeriod{ID: day, Day: day, Type: "long_sleep",
			BedtimeStart: day + "T00:00:00-08:00", BedtimeEnd: day + "T08:00:00-08:00",
			TimeInBed: 12 * 3600, TotalSleep: ptr(hours * 3600)})
	}
	if err := db.SaveSleepPeriods(ctx, ps); err != nil {
		t.Fatal(err)
	}
	p, err := Instruments(ctx, db, today)
	if err != nil {
		t.Fatal(err)
	}
	if a := p.Gauges[5]; a.Label != "Asleep" || !a.HasBand || a.Max > 14 || a.Min < 0 {
		t.Errorf("asleep scale %v to %v (%+v)", a.Min, a.Max, a)
	}
}

// A reading beyond its range the good way leads the standouts.
func TestVitalNotesBeyond(t *testing.T) {
	gs := []chart.Gauge{
		{Label: "HRV", Value: 67, Has: true, HasBand: true, Lo: 40, Hi: 60, CautionLo: 20, CautionHi: 80, Better: 1},
		{Label: "Pulse", Value: 46, Has: true, HasBand: true, Lo: 48, Hi: 52, CautionLo: 44, CautionHi: 56, Better: -1},
		{Label: "SpO2", Value: 99, Has: true, HasBand: true, Lo: 94, Hi: 96, CautionLo: 92, CautionHi: 98, Better: 1}, // far above: an alert
	}
	got := strings.Join(vitalNotes(gs), "; ")
	if want := "HRV 67 · ABOVE RANGE; PULSE 46 · BELOW RANGE"; got != want {
		t.Errorf("notes %q, want %q", got, want)
	}
}
