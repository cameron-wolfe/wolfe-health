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

// The long-term screen's trend: a 30-day average, long enough that a month
// moves it and a week doesn't.
const fitnessRoll = 30

// fitnessStale is how old a marker's newest reading can be before the panel
// stops calling its average "now".
const fitnessStale = 14 * 24 * time.Hour

// fitnessSpec is one slow-moving marker on the long-term screen.
type fitnessSpec struct {
	title, unit  string
	higherBetter bool
	minReadings  int // in a 30-day window, for an average: VO2 max comes rarely
	format       func(float64) string
	change       func(float64) string // a change, unsigned: "2", "2 years", "3 bpm"
}

var (
	fitVO2 = fitnessSpec{"VO2 max", "ml/kg/min", true, 1,
		func(v float64) string { return fmt.Sprintf("%.0f", v) },
		func(v float64) string { return fmt.Sprintf("%.1f", v) }}
	fitCardio = fitnessSpec{"Cardio age", "years", false, 1,
		func(v float64) string { return fmt.Sprintf("%.0f", v) },
		func(v float64) string {
			n := int(math.Round(v))
			return fmt.Sprintf("%d %s", n, plural(n, "year", "years"))
		}}
	fitResting = fitnessSpec{"Resting HR", "bpm, lowest each night", false, 7,
		func(v float64) string { return fmt.Sprintf("%.0f", v) },
		func(v float64) string { return fmt.Sprintf("%.0f bpm", v) }}
	fitSleep = fitnessSpec{"Sleep score", "nightly", true, 7,
		score,
		func(v float64) string { return fmt.Sprintf("%.0f", v) }}
)

// Fitness builds the long-term screen: VO2 max, cardiovascular age,
// resting heart rate and sleep score over the trailing days (a year by
// default), each with its 30-day average and where that average has gone
// since the start of the window, worded as better or worse. A marker with
// no readings in the window is left out.
//
// These move slowly; the 30-day line is the answer to "am I getting
// fitter, and sleeping better?" and the daily dots only its background.
func Fitness(ctx context.Context, db *store.DB, days int, today time.Time) (chart.Trends, error) {
	if days < 60 {
		return chart.Trends{}, fmt.Errorf("dashboard: days must be at least 60, got %d", days)
	}
	to := date(today)
	from := to.AddDate(0, 0, -(days - 1))
	early := from.AddDate(0, 0, -fitnessRoll) // so the line starts with a full window
	t := chart.Trends{
		Title:    "Fitness and sleep · " + spanWords(days),
		Subtitle: "Each day, the 30-day average, and your typical range (the middle half of days)",
		From:     from, To: to,
	}
	load := func(metric string) (map[time.Time]float64, error) {
		samples, err := db.Range(ctx, metric, days+fitnessRoll+1, early.Format(dayLayout), to.Format(dayLayout))
		if err != nil {
			return nil, err
		}
		out := map[time.Time]float64{}
		for _, s := range samples {
			d, err := time.Parse(dayLayout, s.Day)
			if err != nil {
				return nil, fmt.Errorf("dashboard: bad day %q: %w", s.Day, err)
			}
			out[d] = s.Value
		}
		return out, nil
	}
	vo2, err := load(store.MetricVO2Max)
	if err != nil {
		return t, err
	}
	cardio, err := load(store.MetricCardioAge)
	if err != nil {
		return t, err
	}
	sleep, err := load(store.MetricSleep)
	if err != nil {
		return t, err
	}
	_, resting, _, err := loadSignals(ctx, db, early, to, days+fitnessRoll+1)
	if err != nil {
		return t, err
	}
	var empty []chart.TrendPanel
	for _, x := range []struct {
		sp   fitnessSpec
		vals map[time.Time]float64
	}{{fitVO2, vo2}, {fitCardio, cardio}, {fitResting, resting}, {fitSleep, sleep}} {
		p := fitnessPanel(x.sp, x.vals, from, to)
		if len(p.Points) == 0 {
			empty = append(empty, p)
			continue
		}
		t.Panels = append(t.Panels, p)
	}
	// A marker with no readings in the window — VO2 max, until Oura has an
	// estimate — is left out, so the others get its room. With none at all,
	// every panel stays, each saying it has no data yet.
	if len(t.Panels) == 0 {
		t.Panels = empty
	}
	return t, nil
}

// fitnessPanel places one marker's days, its 30-day average, its typical
// range, and the change in the average from the window's first month to
// now.
func fitnessPanel(sp fitnessSpec, vals map[time.Time]float64, from, to time.Time) chart.TrendPanel {
	p := chart.TrendPanel{Title: sp.title, Unit: sp.unit, Value: "—", Change: "no data yet"}
	var cur []float64
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		if v, ok := vals[d]; ok {
			cur = append(cur, v)
			p.Points = append(p.Points, chart.Point{Day: d, Value: v})
		}
	}
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		var sum float64
		n := 0
		for i := 0; i < fitnessRoll; i++ {
			if v, ok := vals[d.AddDate(0, 0, -i)]; ok {
				sum += v
				n++
			}
		}
		if n >= sp.minReadings {
			p.Average = append(p.Average, chart.Point{Day: d, Value: sum / float64(n)})
		}
	}
	if len(cur) == 0 {
		return p
	}
	if n := len(p.Average); n > 0 {
		now := p.Average[n-1]
		p.Value = "now " + sp.format(now.Value)
		first := p.Average[0]
		if now.Day.Sub(first.Day) >= 30*24*time.Hour {
			p.Change = changeWords(sp, now.Value-first.Value)
		} else {
			p.Change = "not long enough for a trend"
		}
		p.Note = fmt.Sprintf("from %s in %s", sp.format(first.Value), first.Day.Format("Jan 2006"))
	}
	if len(cur) >= 5 {
		sorted := append([]float64(nil), cur...)
		sort.Float64s(sorted)
		p.BandLo, p.BandHi, p.HasBand = quantile(sorted, 0.25), quantile(sorted, 0.75), true
		p.Detail = fmt.Sprintf("typical %s to %s", sp.format(p.BandLo), sp.format(p.BandHi))
	}
	// A marker that has stopped arriving — Oura's VO2 max estimates come in
	// spells — isn't "now": the panel says when its last reading was.
	if last := p.Points[len(p.Points)-1].Day; to.Sub(last) > fitnessStale && len(p.Average) > 0 {
		p.Value = fmt.Sprintf("%s in %s", sp.format(p.Average[len(p.Average)-1].Value), last.Format("Jan"))
		p.Detail = "last reading " + last.Format("Jan 2")
	}
	return p
}

// changeWords says how far the average moved and whether that's better:
// "+2.6 · better", "−2 years · better", "steady". The panel's note says
// since when.
func changeWords(sp fitnessSpec, diff float64) string {
	size := sp.change(math.Abs(diff))
	if size == sp.change(0) {
		return "steady"
	}
	sign, better := "+", diff > 0
	if diff < 0 {
		sign = "−"
	}
	if !sp.higherBetter {
		better = !better
	}
	verdict := "worse"
	if better {
		verdict = "better"
	}
	return fmt.Sprintf("%s%s · %s", sign, size, verdict)
}

// spanWords names a window: "last 12 months", "last 6 months", "last 90 days".
func spanWords(days int) string {
	if days%365 == 0 {
		if y := days / 365; y == 1 {
			return "last 12 months"
		} else {
			return fmt.Sprintf("last %d years", y)
		}
	}
	if m := int(math.Round(float64(days) / 30.4)); days >= 180 && math.Abs(float64(days)-float64(m)*30.4) < 4 {
		return fmt.Sprintf("last %d months", m)
	}
	return fmt.Sprintf("last %d days", days)
}
