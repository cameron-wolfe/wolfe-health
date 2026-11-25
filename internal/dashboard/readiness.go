package dashboard

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// readinessParts are the parts of the readiness score in a fixed order
// round the radar: sleep down the right, the body's signals across the
// bottom, and activity up the left.
var readinessParts = []struct{ key, label, short string }{
	{"previous_night", "Previous night", "Last night"},
	{"sleep_balance", "Sleep balance", "Sleep bal."},
	{"sleep_regularity", "Sleep regularity", "Regularity"},
	{"hrv_balance", "HRV balance", "HRV bal."},
	{"resting_heart_rate", "Resting HR", "Resting HR"},
	{"recovery_index", "Recovery index", "Recovery"},
	{"body_temperature", "Body temp", "Body temp"},
	{"previous_day_activity", "Previous day", "Prev. day"},
	{"activity_balance", "Activity balance", "Activity bal."},
}

// ReadinessBreakdown builds the readiness radar: the latest day's parts of
// the readiness score against their average over the days before it. A
// part Oura never reported in the window is left off the radar.
func ReadinessBreakdown(ctx context.Context, db *store.DB, days int, today time.Time) (chart.SleepRadar, error) {
	if days < 2 {
		return chart.SleepRadar{}, fmt.Errorf("dashboard: days must be at least 2, got %d", days)
	}
	to := date(today)
	from := to.AddDate(0, 0, -days)
	r := chart.SleepRadar{
		Title:      "WolfeSense · Readiness",
		Subtitle:   "The parts of the readiness score, 0 at the center to 100 at the rim",
		AvgLabel:   fmt.Sprintf("%d-day average", days),
		ScoreLabel: "Readiness",
	}
	byDay := make([]map[string]float64, len(readinessParts))
	latest := ""
	for i, p := range readinessParts {
		samples, err := db.Range(ctx, store.MetricReadinessPart+p.key, days+1, from.Format(dayLayout), to.Format(dayLayout))
		if err != nil {
			return r, err
		}
		byDay[i] = map[string]float64{}
		for _, s := range samples {
			byDay[i][s.Day] = s.Value
			if s.Day > latest {
				latest = s.Day
			}
		}
	}
	if latest == "" {
		// every spoke, empty, until the parts arrive
		r.Subtitle = "No readiness parts stored yet: they arrive with the next fetch"
		r.PanelHead, r.ShapeLabel = "TODAY", "today"
		for _, p := range readinessParts {
			if p.key != "sleep_regularity" { // newer, and not reported for everyone
				r.Parts = append(r.Parts, chart.RadarPart{Label: p.label, Short: p.short})
			}
		}
		return r, nil
	}
	day, err := time.Parse(dayLayout, latest)
	if err != nil {
		return r, err
	}
	r.Night = day
	r.PanelHead, r.ShapeLabel = "TODAY", "today"
	if !day.Equal(to) {
		r.PanelHead, r.ShapeLabel = "ON "+strings.ToUpper(day.Format("Jan 2")), day.Format("Jan 2")
	}
	r.Title += " · " + map[bool]string{true: "today", false: day.Format("Jan 2")}[day.Equal(to)]
	for i, p := range readinessParts {
		if len(byDay[i]) == 0 {
			continue
		}
		part := chart.RadarPart{Label: p.label, Short: p.short}
		if v, ok := byDay[i][latest]; ok {
			part.Value, part.Has = v, true
		}
		var sum float64
		var n int
		for d, v := range byDay[i] {
			if d != latest {
				sum += v
				n++
			}
		}
		if n > 0 {
			part.Avg, part.HasAvg = sum/float64(n), true
		}
		r.Parts = append(r.Parts, part)
	}
	samples, err := db.Range(ctx, store.MetricReadiness, 1, latest, latest)
	if err != nil {
		return r, err
	}
	if len(samples) > 0 {
		r.Score = score(samples[0].Value)
	}
	return r, nil
}
