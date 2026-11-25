package dashboard

import (
	"context"
	"fmt"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// radarParts are the seven parts of the sleep score in a fixed order round
// the radar, so every night's shape compares with any other's: the amount
// and quality of sleep down the right, its timing and onset up the left.
var radarParts = []struct {
	label string
	get   func(store.SleepContributors) *int
}{
	{"Total sleep", func(n store.SleepContributors) *int { return n.TotalSleep }},
	{"Efficiency", func(n store.SleepContributors) *int { return n.Efficiency }},
	{"Restfulness", func(n store.SleepContributors) *int { return n.Restfulness }},
	{"REM sleep", func(n store.SleepContributors) *int { return n.RemSleep }},
	{"Deep sleep", func(n store.SleepContributors) *int { return n.DeepSleep }},
	{"Latency", func(n store.SleepContributors) *int { return n.Latency }},
	{"Timing", func(n store.SleepContributors) *int { return n.Timing }},
}

// SleepBreakdown builds the sleep radar: the latest night's seven parts of
// the sleep score against their average over the trailing days (the nights
// before it, so the night isn't compared with itself).
func SleepBreakdown(ctx context.Context, db *store.DB, days int, today time.Time) (chart.SleepRadar, error) {
	if days < 2 {
		return chart.SleepRadar{}, fmt.Errorf("dashboard: days must be at least 2, got %d", days)
	}
	to := date(today)
	from := to.AddDate(0, 0, -days)
	r := chart.SleepRadar{
		Title:    "WolfeSense · Sleep",
		Subtitle: "The seven parts of the sleep score, 0 at the center to 100 at the rim",
		AvgLabel: fmt.Sprintf("%d-night average", days),
	}
	nights, err := db.RecentContributors(ctx, days+1) // newest first
	if err != nil {
		return r, err
	}
	var latest *store.SleepContributors
	sums, counts := make([]float64, len(radarParts)), make([]int, len(radarParts))
	for i := range nights {
		n := &nights[i]
		day, err := time.Parse(dayLayout, n.Day)
		if err != nil {
			return r, fmt.Errorf("dashboard: bad day %q: %w", n.Day, err)
		}
		if day.Before(from) || day.After(to) {
			continue
		}
		if latest == nil {
			latest, r.Night = n, day
			continue
		}
		for k, p := range radarParts {
			if v := p.get(*n); v != nil {
				sums[k] += float64(*v)
				counts[k]++
			}
		}
	}
	if latest == nil {
		r.Subtitle = "No sleep recorded yet"
	} else {
		r.Title += " · " + nightLabel(r.Night, to)
	}
	for k, p := range radarParts {
		part := chart.RadarPart{Label: p.label}
		if latest != nil {
			if v := p.get(*latest); v != nil {
				part.Value, part.Has = float64(*v), true
			}
		}
		if counts[k] > 0 {
			part.Avg, part.HasAvg = sums[k]/float64(counts[k]), true
		}
		r.Parts = append(r.Parts, part)
	}
	if latest != nil {
		samples, err := db.Range(ctx, store.MetricSleep, 1, r.Night.Format(dayLayout), r.Night.Format(dayLayout))
		if err != nil {
			return r, err
		}
		if len(samples) > 0 {
			r.Score = score(samples[0].Value)
		}
	}
	return r, nil
}

// nightLabel names the night ending on day: "last night", or its date.
func nightLabel(day, today time.Time) string {
	if day.Equal(today) {
		return "last night"
	}
	return "night of " + day.AddDate(0, 0, -1).Format("Jan 2")
}
