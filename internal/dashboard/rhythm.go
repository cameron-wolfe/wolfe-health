package dashboard

import (
	"context"
	"fmt"
	"math"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

var weekdayNames = [7]string{"Mon", "Tue", "Wed", "Thu", "Fri", "Sat", "Sun"}

// mondayIndex maps a date to 0 for Monday through 6 for Sunday.
func mondayIndex(t time.Time) int { return (int(t.Weekday()) + 6) % 7 }

// WeeklyRhythm averages sleep score, readiness and high-stress time by day of
// week over the trailing days. Sleep and readiness are grouped by the morning
// Oura scores them — "Monday" sleep is Sunday night's — and stress uses
// complete days only, as the other views do.
func WeeklyRhythm(ctx context.Context, db *store.DB, days int, today time.Time) (chart.Rhythm, error) {
	if days < 7 {
		return chart.Rhythm{}, fmt.Errorf("dashboard: days must be at least 7, got %d", days)
	}
	to := date(today)
	from := to.AddDate(0, 0, -(days - 1))
	rh := chart.Rhythm{}

	type spec struct {
		metric, title string
		format        func(float64) string // bar labels and caption
		lowerIsBetter bool
		completeOnly  bool
		unit          float64 // stored units per display unit, for rounding the scale
		minExtent     float64 // smallest scale, in display units, so noise looks small
	}
	minutes := func(sec float64) string { return fmt.Sprintf("%.0fm", math.Round(sec/60)) }
	specs := []spec{
		{store.MetricSleep, "Sleep score", score, false, false, 1, 5},
		{store.MetricReadiness, "Readiness", score, false, false, 1, 5},
		{store.MetricStress, "High-stress time", minutes, true, true, 60, 30},
	}

	var first time.Time
	var total int
	for _, sp := range specs {
		samples, err := db.Range(ctx, sp.metric, days+1, from.Format(dayLayout), to.Format(dayLayout))
		if err != nil {
			return rh, err
		}
		var sum [7]float64
		var cnt [7]int
		var all float64
		var allN int
		for _, s := range samples {
			d, err := time.Parse(dayLayout, s.Day)
			if err != nil {
				return rh, fmt.Errorf("dashboard: bad day %q: %w", s.Day, err)
			}
			if sp.completeOnly && !d.Before(to) {
				continue
			}
			i := mondayIndex(d)
			sum[i] += s.Value
			cnt[i]++
			all += s.Value
			allN++
			if first.IsZero() || d.Before(first) {
				first = d
			}
		}
		total = max(total, allN)

		p := chart.RhythmPanel{Title: sp.title, LowerIsBetter: sp.lowerIsBetter}
		best, worst := -1, -1
		for i := 0; i < 7; i++ {
			if cnt[i] == 0 {
				continue
			}
			p.Has[i] = true
			p.Values[i] = sum[i] / float64(cnt[i])
			p.Labels[i] = sp.format(p.Values[i])
			better := func(a, b float64) bool { return a > b != sp.lowerIsBetter }
			if best < 0 || better(p.Values[i], p.Values[best]) {
				best = i
			}
			if worst < 0 || better(p.Values[worst], p.Values[i]) {
				worst = i
			}
		}
		if allN == 0 {
			p.Caption = "no data"
			rh.Panels = append(rh.Panels, p)
			continue
		}
		p.Avg = all / float64(allN)
		p.AvgLabel = "avg " + sp.format(p.Avg)
		// a symmetric scale around the average, wide enough for the
		// furthest day, rounded to a clean number
		var dev float64
		for i := 0; i < 7; i++ {
			if p.Has[i] {
				dev = math.Max(dev, math.Abs(p.Values[i]-p.Avg))
			}
		}
		// Round in display units (minutes, not seconds), and never narrower
		// than minExtent: a ±2 scale would make a one-point wobble fill the panel.
		_, ext, _ := niceTop(dev / sp.unit)
		p.Extent = math.Max(ext, sp.minExtent) * sp.unit
		p.ExtentLabel = "±" + sp.format(p.Extent)
		bw := "best"
		if sp.lowerIsBetter {
			bw = "least"
		}
		ww := "worst"
		if sp.lowerIsBetter {
			ww = "most"
		}
		p.Caption = fmt.Sprintf("%s %s %s · %s %s %s",
			bw, weekdayNames[best], p.Labels[best], ww, weekdayNames[worst], p.Labels[worst])
		rh.Panels = append(rh.Panels, p)
	}

	rh.Title = "Weekly rhythm"
	if !first.IsZero() {
		rh.Title += " · since " + first.Format("Jan 2")
	}
	rh.Subtitle = fmt.Sprintf("Each weekday against your average over %d days · solid better, outlined worse", total)
	return rh, nil
}

// niceTop rounds a maximum up to a clean scale top: 1, 2 or 5 times a power
// of ten, at least 10% above the value so the tallest bar's label fits.
func niceTop(v float64) (float64, float64, float64) {
	if v <= 0 {
		return 0, 1, 1
	}
	v *= 1.1
	mag := math.Pow(10, math.Floor(math.Log10(v)))
	for _, m := range []float64{1, 2, 5, 10} {
		if m*mag >= v {
			return 0, m * mag, mag
		}
	}
	return 0, 10 * mag, mag
}
