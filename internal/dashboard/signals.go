package dashboard

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// The normal range for a night is built from the nights before it, with
// robust statistics: the median, plus and minus bandSD times a spread
// estimated from the median absolute deviation (scaled by 1.4826 to match a
// standard deviation for ordinary data). Mean and SD would let a few unusual
// nights — a fever — swell the range for weeks afterwards and hide the next
// spell; the median and MAD barely move. ±1.5 holds roughly 87% of ordinary
// nights, so a night outside is unusual without every third one being
// flagged. Each signal has a floor on its spread, so a run of very steady
// nights doesn't make a tiny wobble look abnormal.
const (
	baselineDays = 30 // nights the range is built from
	minBaseline  = 10 // fewer than this: no range yet
	bandSD       = 1.5
)

// signalSpec describes one body signal.
type signalSpec struct {
	title, unit string
	sdFloor     float64
	format      func(float64) string // the value as shown: "60 ms", "+0.3 °C"
	rangeFmt    func(float64) string // the range's ends: "41", "+0.3"
}

var (
	hrvSignal  = signalSpec{"HRV", "ms, nightly average", 3, func(v float64) string { return fmt.Sprintf("%.0f ms", v) }, func(v float64) string { return fmt.Sprintf("%.0f", v) }}
	hrSignal   = signalSpec{"Lowest HR", "bpm, each night", 1.5, func(v float64) string { return fmt.Sprintf("%.0f bpm", v) }, func(v float64) string { return fmt.Sprintf("%.0f", v) }}
	tempSignal = signalSpec{"Temperature", "°C from your baseline", 0.1, func(v float64) string { return fmt.Sprintf("%+.2f °C", v) }, func(v float64) string { return fmt.Sprintf("%+.2f", v) }}
)

// BodySignals builds the body-signals chart: nightly HRV, lowest heart rate
// and temperature deviation over the trailing days, each against a normal
// range rolled from the user's own previous 30 nights.
//
// Together these move when the body is under strain — illness, hard
// training, poor recovery — often before the scores do. The chart says
// where each night sits relative to normal; it does not diagnose.
func BodySignals(ctx context.Context, db *store.DB, days int, today time.Time) (chart.Signals, error) {
	if days < 7 {
		return chart.Signals{}, fmt.Errorf("dashboard: days must be at least 7, got %d", days)
	}
	to := date(today)
	from := to.AddDate(0, 0, -(days - 1))
	early := from.AddDate(0, 0, -baselineDays) // history the first nights' ranges need
	sig := chart.Signals{
		Title:    fmt.Sprintf("Body signals · last %d days", days),
		Subtitle: "Each night against your normal range (shaded), built from your previous 30 nights",
		From:     from, To: to,
	}

	hrv, hr, temp, err := loadSignals(ctx, db, early, to, days+baselineDays+1)
	if err != nil {
		return sig, err
	}

	for _, x := range []struct {
		spec signalSpec
		vals map[time.Time]float64
	}{{hrvSignal, hrv}, {hrSignal, hr}, {tempSignal, temp}} {
		sig.Panels = append(sig.Panels, signalPanel(x.spec, x.vals, from, to))
	}
	return sig, nil
}

// signalPanel places each night in the window against the range built from
// the nights before it, and words the latest night.
func signalPanel(sp signalSpec, vals map[time.Time]float64, from, to time.Time) chart.SignalPanel {
	p := chart.SignalPanel{Title: sp.title, Unit: sp.unit, Value: "—", Status: "no data yet"}
	outside, latest := 0, time.Time{}
	var latestBand *chart.BandPoint
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		band, ok := baseline(vals, d, sp.sdFloor)
		if ok {
			p.Band = append(p.Band, band)
		}
		v, has := vals[d]
		if !has {
			continue
		}
		p.Points = append(p.Points, chart.Point{Day: d, Value: v})
		if ok && (v < band.Lo || v > band.Hi) {
			outside++
		}
		latest = d
		if ok {
			b := band
			latestBand = &b
		} else {
			latestBand = nil
		}
	}
	if latest.IsZero() {
		return p
	}
	v := vals[latest]
	p.Value = sp.format(v)
	if latestBand == nil {
		p.Status = "building your range"
		p.Detail = fmt.Sprintf("needs %d nights of history", minBaseline)
		return p
	}
	switch {
	case v < latestBand.Lo:
		p.Status = "below your normal range"
	case v > latestBand.Hi:
		p.Status = "above your normal range"
	default:
		p.Status = "within your normal range"
	}
	nights := "nights"
	if outside == 1 {
		nights = "night"
	}
	p.Detail = fmt.Sprintf("normal %s to %s", sp.rangeFmt(latestBand.Lo), sp.rangeFmt(latestBand.Hi))
	p.Note = fmt.Sprintf("%d %s outside in %d days", outside, nights, int(to.Sub(from).Hours()/24)+1)
	return p
}

// baseline is the normal range for day d, from the values in the
// baselineDays before it: their median ± bandSD robust spreads.
func baseline(vals map[time.Time]float64, d time.Time, sdFloor float64) (chart.BandPoint, bool) {
	var xs []float64
	for i := 1; i <= baselineDays; i++ {
		if v, ok := vals[d.AddDate(0, 0, -i)]; ok {
			xs = append(xs, v)
		}
	}
	if len(xs) < minBaseline {
		return chart.BandPoint{}, false
	}
	sort.Float64s(xs)
	mid := median(xs)
	devs := make([]float64, len(xs))
	for i, x := range xs {
		devs[i] = math.Abs(x - mid)
	}
	sort.Float64s(devs)
	spread := math.Max(1.4826*median(devs), sdFloor)
	return chart.BandPoint{Day: d, Lo: mid - bandSD*spread, Hi: mid + bandSD*spread}, true
}

// loadSignals reads the nightly signals between from and to: average HRV and
// lowest heart rate from each day's main sleep period, and temperature
// deviation from its stored metric. limit bounds the temperature rows read.
func loadSignals(ctx context.Context, db *store.DB, from, to time.Time, limit int) (hrv, hr, temp map[time.Time]float64, err error) {
	early := from
	// HRV and lowest heart rate: from each day's main sleep period
	periods, err := db.SleepPeriodsBetween(ctx, early.Format(dayLayout), to.Format(dayLayout))
	if err != nil {
		return nil, nil, nil, err
	}
	byDay := map[string][]store.SleepPeriod{}
	for _, p := range periods {
		byDay[p.Day] = append(byDay[p.Day], p)
	}
	hrv, hr = map[time.Time]float64{}, map[time.Time]float64{}
	for day, ps := range byDay {
		p, _ := mainPeriod(ps)
		d, err := time.Parse(dayLayout, day)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("dashboard: bad day %q: %w", day, err)
		}
		if p.AverageHRV != nil {
			hrv[d] = float64(*p.AverageHRV)
		}
		if p.LowestHR != nil {
			hr[d] = float64(*p.LowestHR)
		}
	}

	// temperature deviation: a daily metric stored from readiness
	samples, err := db.Range(ctx, store.MetricTempDev, limit,
		early.Format(dayLayout), to.Format(dayLayout))
	if err != nil {
		return nil, nil, nil, err
	}
	temp = map[time.Time]float64{}
	for _, s := range samples {
		d, err := time.Parse(dayLayout, s.Day)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("dashboard: bad day %q: %w", s.Day, err)
		}
		temp[d] = s.Value
	}

	return hrv, hr, temp, nil
}
