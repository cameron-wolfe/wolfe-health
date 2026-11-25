package dashboard

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// PhaseMonth builds WolfePhase · Month for the month holding month: each
// night through last night as its WolfePhase night, in the cell for the
// evening it began (Oura files a night under the day of waking, so the
// night of the 23rd is read from the 24th's sleep), the time asleep under it,
// and the month's averages: asleep, bedtime and waking, and how far
// bedtime strayed.
func PhaseMonth(ctx context.Context, db *store.DB, month, today time.Time) (chart.PhaseMonth, error) {
	today = date(today)
	first := time.Date(month.Year(), month.Month(), 1, 0, 0, 0, 0, time.UTC)
	last := first.AddDate(0, 1, -1)
	m := chart.PhaseMonth{
		Month:     first,
		Title:     "WolfePhase · " + first.Format("January 2006"),
		Subtitle:  "Each night as a moon, lit from bedtime to waking on a 12-hour face",
		Nights:    map[int]chart.Night{},
		Asleep:    map[int]float64{},
		PanelHead: first.Format("January"),
		LeadLabel: "Asleep, avg",
		LeadValue: "—",
	}
	end := last // the last evening shown
	if !today.After(last) {
		end = today.AddDate(0, 0, -1) // last night; tonight is still to come
		m.Today = today.Day()
		m.Subtitle += " · so far"
	}

	var asleep, bed, wake []float64
	for d := first; !d.After(end); d = d.AddDate(0, 0, 1) {
		n, err := Night(ctx, db, d.AddDate(0, 0, 1).Format(dayLayout))
		if errors.Is(err, ErrNoSleep) {
			continue
		}
		if err != nil {
			return m, err
		}
		m.Nights[d.Day()] = n
		var mins float64
		for _, s := range n.Stages {
			if s == chart.StageDeep || s == chart.StageLight || s == chart.StageREM {
				mins += n.StageEpoch.Minutes()
			}
		}
		if mins > 0 {
			m.Asleep[d.Day()] = mins
			asleep = append(asleep, mins)
		}
		// clock times on a scale that doesn't break at midnight: bedtime
		// in minutes after noon, waking in minutes after midnight
		s, e := n.Start, n.End
		bed = append(bed, math.Mod(float64(s.Hour()*60+s.Minute())+12*60, 24*60))
		wake = append(wake, float64(e.Hour()*60+e.Minute()))
	}
	m.PanelNote = fmt.Sprintf("%d %s", len(m.Nights), plural(len(m.Nights), "night", "nights"))
	if len(m.Nights) == 0 {
		m.Headline = "no nights yet"
		return m, nil
	}

	// the longest and shortest nights, when there are enough to tell
	best, worst := 0, 0
	for d, a := range m.Asleep {
		if best == 0 || a > m.Asleep[best] || (a == m.Asleep[best] && d < best) {
			best = d
		}
		if worst == 0 || a < m.Asleep[worst] || (a == m.Asleep[worst] && d < worst) {
			worst = d
		}
	}
	if len(m.Asleep) >= 3 {
		m.Best, m.Worst = best, worst
	}

	if len(asleep) > 0 {
		m.LeadValue = chart.Duration(mean(asleep))
		m.Headline = m.LeadValue + " asleep, avg"
	}
	clock := func(mins float64, afterNoon bool) string {
		if afterNoon {
			mins += 12 * 60
		}
		t := time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC).Add(time.Duration(math.Round(mins)) * time.Minute)
		return t.Format("3:04 PM")
	}
	m.Rows = append(m.Rows,
		chart.PanelRow{Label: "Bedtime, avg", Value: clock(mean(bed), true)},
		chart.PanelRow{Label: "Waking, avg", Value: clock(mean(wake), false)},
	)
	spread := stddev(bed)
	if len(bed) >= 3 {
		m.Rows = append(m.Rows, chart.PanelRow{Label: "Bedtime spread", Value: "±" + chart.Duration(spread)})
	}
	if m.Best > 0 {
		m.Rows = append(m.Rows,
			chart.PanelRow{Label: "Longest", Value: chart.Duration(m.Asleep[m.Best]), Note: first.AddDate(0, 0, m.Best-1).Format("Jan 2")},
			chart.PanelRow{Label: "Shortest", Value: chart.Duration(m.Asleep[m.Worst]), Note: first.AddDate(0, 0, m.Worst-1).Format("Jan 2")},
		)
	}
	eight := 0
	for _, a := range asleep {
		if a >= 8*60 {
			eight++
		}
	}
	if len(asleep) > 0 {
		m.Rows = append(m.Rows, chart.PanelRow{Label: "Nights of 8h+", Value: fmt.Sprint(eight), Note: fmt.Sprintf("of %d", len(asleep))})
	}
	if len(bed) >= 3 {
		switch {
		case spread <= 30:
			m.Conclusion = "A steady bedtime: the moons hold still"
		case spread <= 60:
			m.Conclusion = "Bedtime wandered within the hour"
		default:
			m.Conclusion = "Bedtime wandered: watch the moons turn"
		}
	}
	return m, nil
}

func stddev(vs []float64) float64 {
	if len(vs) < 2 {
		return 0
	}
	mu, s := mean(vs), 0.0
	for _, v := range vs {
		s += (v - mu) * (v - mu)
	}
	return math.Sqrt(s / float64(len(vs)))
}
