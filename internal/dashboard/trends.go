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

// rollDays is the window of the trend line: a 7-day average smooths the
// day-to-day jitter that made raw lines read as noise.
const rollDays = 7

// trendSpec is one metric on the trends screen.
type trendSpec struct {
	title, unit  string
	metric       string
	completeOnly bool // leave out today's unfinished day
	format       func(float64) string
}

var trendSpecs = []trendSpec{
	{"Sleep score", "nightly", store.MetricSleep, false, score},
	{"Readiness", "daily", store.MetricReadiness, false, score},
	{"Steps", "per day", store.MetricSteps, true, thousands},
}

// Trends builds the trends screen: sleep score, readiness and steps over
// the trailing days, each as daily values, a 7-day average, and the middle
// half of the period's days shaded as the typical range — with the period's
// average against the period before it.
func Trends(ctx context.Context, db *store.DB, days int, today time.Time) (chart.Trends, error) {
	if days < 14 {
		return chart.Trends{}, fmt.Errorf("dashboard: days must be at least 14, got %d", days)
	}
	to := date(today)
	from := to.AddDate(0, 0, -(days - 1))
	prior := from.AddDate(0, 0, -days)
	t := chart.Trends{
		Title:    fmt.Sprintf("Trends · last %d days", days),
		Subtitle: "Daily values, the 7-day average, and your typical range (the middle half of days)",
		From:     from, To: to,
	}
	for _, sp := range trendSpecs {
		samples, err := db.Range(ctx, sp.metric, 2*days+1, prior.Format(dayLayout), to.Format(dayLayout))
		if err != nil {
			return t, err
		}
		vals := map[time.Time]float64{}
		for _, s := range samples {
			d, err := time.Parse(dayLayout, s.Day)
			if err != nil {
				return t, fmt.Errorf("dashboard: bad day %q: %w", s.Day, err)
			}
			if sp.completeOnly && !d.Before(to) {
				continue
			}
			vals[d] = s.Value
		}
		t.Panels = append(t.Panels, trendPanel(sp, vals, prior, from, to, days))
	}
	return t, nil
}

func trendPanel(sp trendSpec, vals map[time.Time]float64, prior, from, to time.Time, days int) chart.TrendPanel {
	p := chart.TrendPanel{Title: sp.title, Unit: sp.unit, Value: "—", Change: "no data yet"}
	var cur, before []float64
	for d := prior; !d.After(to); d = d.AddDate(0, 0, 1) {
		v, ok := vals[d]
		if !ok {
			continue
		}
		if d.Before(from) {
			before = append(before, v)
			continue
		}
		cur = append(cur, v)
		p.Points = append(p.Points, chart.Point{Day: d, Value: v})
	}
	// the 7-day average, for each day with at least 3 values in its window
	for d := from; !d.After(to); d = d.AddDate(0, 0, 1) {
		var sum float64
		var n int
		for i := 0; i < rollDays; i++ {
			if v, ok := vals[d.AddDate(0, 0, -i)]; ok {
				sum += v
				n++
			}
		}
		if n >= 3 {
			p.Average = append(p.Average, chart.Point{Day: d, Value: sum / float64(n)})
		}
	}
	if len(cur) == 0 {
		return p
	}
	avg := mean(cur)
	p.Value = "avg " + sp.format(avg)
	if len(before) > 0 {
		diff := avg - mean(before)
		size := sp.format(math.Abs(diff))
		switch {
		case size == sp.format(0):
			p.Change = fmt.Sprintf("same as the %d days before", days)
		case diff < 0:
			p.Change = fmt.Sprintf("−%s vs the %d days before", size, days)
		default:
			p.Change = fmt.Sprintf("+%s vs the %d days before", size, days)
		}
	} else {
		p.Change = ""
	}
	if len(cur) >= 5 {
		sorted := append([]float64(nil), cur...)
		sort.Float64s(sorted)
		p.BandLo, p.BandHi, p.HasBand = quantile(sorted, 0.25), quantile(sorted, 0.75), true
		p.Detail = fmt.Sprintf("typical %s to %s", sp.format(p.BandLo), sp.format(p.BandHi))
	}
	if n := len(p.Average); n > 0 {
		p.Note = "7-day average now " + sp.format(p.Average[n-1].Value)
	}
	return p
}

func mean(xs []float64) float64 {
	var s float64
	for _, x := range xs {
		s += x
	}
	return s / float64(len(xs))
}

// quantile of sorted values, interpolating between neighbors.
func quantile(sorted []float64, q float64) float64 {
	pos := q * float64(len(sorted)-1)
	lo := int(math.Floor(pos))
	hi := int(math.Ceil(pos))
	return sorted[lo] + (sorted[hi]-sorted[lo])*(pos-float64(lo))
}
