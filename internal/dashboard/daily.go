// Package dashboard turns stored samples into the content of display screens.
//
// It sits between store and chart: it decides which day's number to show, what
// to compare it with, and how to word it. chart then only lays out what it is
// given, and never touches the database.
package dashboard

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

const (
	rangeDays = 30 // the spread a range strip shows
	avgDays   = 7  // window the delta compares against
	minAvgN   = 3  // fewer days than this in the window: no delta
	minRangeN = 5  // fewer days than this in the month: no range strip
)

// spec describes how one metric is shown.
type spec struct {
	metric string
	label  string
	format func(float64) string // the value; also used for the delta's size

	// completeOnly skips today's sample. Activity and stress accumulate over
	// the day, so mid-afternoon they read low against any full day; showing
	// yesterday's finished number is the honest comparison.
	completeOnly bool
}

var (
	readinessSpec = spec{store.MetricReadiness, "Readiness", score, false}
	sleepSpec     = spec{store.MetricSleep, "Sleep", score, false}
	activitySpec  = spec{store.MetricActivity, "Activity", score, true}
	stepsSpec     = spec{store.MetricSteps, "Steps", thousands, true}
	stressSpec    = spec{store.MetricStress, "High stress", duration, true}
)

// heldBackBelow is the contributor score under which a part of sleep is
// named as holding the score back: Oura's own "pay attention" band (85+ is
// optimal, 70-84 good, under 70 pay attention).
const heldBackBelow = 70

// Daily builds the daily screen for today: readiness as the hero with a
// verdict, four metrics, and the night behind the sleep score.
//
// today is a date; only its year, month and day are used.
func Daily(ctx context.Context, db *store.DB, today time.Time) (chart.Daily, error) {
	today = date(today)
	d := chart.Daily{Date: today, NightTitle: "Last night"}

	hero, err := buildMetric(ctx, db, readinessSpec, today)
	if err != nil {
		return d, err
	}
	d.Hero = hero.Metric
	d.Verdict, d.VerdictDetail = verdict(hero)

	sleep, err := buildMetric(ctx, db, sleepSpec, today)
	if err != nil {
		return d, err
	}
	d.Metrics[0] = sleep.Metric
	for i, sp := range []spec{activitySpec, stepsSpec, stressSpec} {
		m, err := buildMetric(ctx, db, sp, today)
		if err != nil {
			return d, err
		}
		d.Metrics[i+1] = m.Metric
	}

	if err := fillRings(ctx, db, &d, today); err != nil {
		return d, err
	}

	// the readiness parts for the brief's radar, when the latest are the
	// readiness shown's: a radar of an older day's parts would disagree
	// with the score above it
	parts, err := ReadinessBreakdown(ctx, db, rangeDays, today)
	if err != nil {
		return d, err
	}
	if !parts.Night.IsZero() && parts.Night.Format(dayLayout) == hero.day && len(parts.Parts) >= 3 {
		d.Parts, d.HasParts = parts, true
	}

	// the night behind the sleep score shown, so the two always agree
	if sleep.day == "" {
		return d, nil
	}
	return d, fillNight(ctx, db, &d, sleep.day, today)
}

// built is a metric plus what the hero's verdict and the night need.
type built struct {
	chart.Metric
	day      string  // the latest value's day, "" with no data
	value    float64 // the latest value
	delta    float64 // against the 7-day average
	hasDelta bool
	month    []float64 // values in the 30 days ending at day, for the verdict
}

// buildMetric reads one metric's recent history: its latest value, the
// change against the 7 days before, and the 30-day range strip.
func buildMetric(ctx context.Context, db *store.DB, sp spec, today time.Time) (built, error) {
	b := built{Metric: chart.Metric{Label: sp.label, Value: "—", Note: "no data"}}

	from := today.AddDate(0, 0, -(rangeDays + 1))
	samples, err := db.Range(ctx, sp.metric, rangeDays+2, from.Format(dayLayout), today.Format(dayLayout))
	if err != nil {
		return b, err
	}
	type row struct {
		day time.Time
		v   float64
	}
	var rows []row
	for _, s := range samples {
		day, err := time.Parse(dayLayout, s.Day)
		if err != nil {
			return b, fmt.Errorf("dashboard: bad day %q: %w", s.Day, err)
		}
		if sp.completeOnly && !day.Before(today) {
			continue
		}
		rows = append(rows, row{day, s.Value})
	}
	if len(rows) == 0 {
		return b, nil
	}

	latest := rows[len(rows)-1]
	b.day, b.value = latest.day.Format(dayLayout), latest.v
	b.Value = sp.format(latest.v)
	b.Note = relativeDay(latest.day, today)

	var sum float64
	var n int
	for _, r := range rows {
		ago := int(latest.day.Sub(r.day).Hours() / 24)
		if ago >= 1 && ago <= avgDays {
			sum += r.v
			n++
		}
		if ago >= 0 && ago < rangeDays {
			b.month = append(b.month, r.v)
		}
	}
	if n >= minAvgN {
		b.delta, b.hasDelta = latest.v-sum/float64(n), true
		b.Delta = deltaText(b.delta, sp.format)
	}
	if len(b.month) >= minRangeN {
		sorted := append([]float64(nil), b.month...)
		sort.Float64s(sorted)
		b.Range = chart.Range{
			Has: true, Low: sorted[0], High: sorted[len(sorted)-1],
			Typical: median(sorted), Now: latest.v,
			LowLabel: sp.format(sorted[0]), HighLabel: sp.format(sorted[len(sorted)-1]),
		}
	}
	return b, nil
}

func median(sorted []float64) float64 {
	n := len(sorted)
	if n%2 == 1 {
		return sorted[n/2]
	}
	return (sorted[n/2-1] + sorted[n/2]) / 2
}

// verdict words readiness against your own last month: where today ranks
// among those days, then the change against the last week in plain words.
// It describes, never advises.
func verdict(b built) (string, string) {
	if b.day == "" {
		return "No readiness yet", ""
	}
	var lower, others int
	for _, v := range b.month {
		others++
		if v < b.value {
			lower++
		}
	}
	others-- // today is in the month too
	head := "About your usual"
	if others >= minRangeN {
		switch f := float64(lower) / float64(others); {
		case f < 0.15:
			head = "Well below your usual"
		case f < 0.4:
			head = "Below your usual"
		case f > 0.85:
			head = "Well above your usual"
		case f > 0.6:
			head = "Above your usual"
		}
	}
	if !b.hasDelta {
		return head, ""
	}
	size := score(math.Abs(b.delta))
	switch {
	case size == "0":
		return head, "Level with your 7-day average"
	case b.delta < 0:
		return head, size + " under your 7-day average"
	}
	return head, size + " over your 7-day average"
}

// fillNight adds the night that counts toward day: its stage bar, when it
// ran, and the parts of sleep that held its score back.
func fillNight(ctx context.Context, db *store.DB, d *chart.Daily, day string, today time.Time) error {
	if t, _ := time.Parse(dayLayout, day); !t.Equal(today) {
		d.NightTitle = "Night of " + chart.NightOf(t).Format("Jan 2") // as "Last night" is: the evening it began
	}
	periods, err := db.SleepPeriodsOnDay(ctx, day)
	if err != nil {
		return err
	}
	if p, ok := mainPeriod(periods); ok {
		if n, ok := stageTimes(p); ok {
			d.Stages = n
			start, err1 := time.Parse(time.RFC3339, p.BedtimeStart)
			end, err2 := time.Parse(time.RFC3339, p.BedtimeEnd)
			if err1 == nil && err2 == nil {
				d.NightWhen = fmt.Sprintf("%s → %s · %s asleep",
					start.Format("3:04 PM"), end.Format("3:04 PM"), duration(n.Asleep().Seconds()))
			}
		}
	}

	// the night in full for the brief's moon; a night without its stages
	// still has the rest of the brief
	if n, err := Night(ctx, db, day); err == nil {
		d.Phase, d.HasPhase = n, true
	} else if !errors.Is(err, ErrNoSleep) {
		return err
	}

	nights, err := db.ContributorsOnDay(ctx, day)
	if err != nil || len(nights) != 1 {
		return err
	}
	c := nights[0]
	type part struct {
		name string
		v    *int
	}
	var low []part
	for _, p := range []part{
		{"Total sleep", c.TotalSleep}, {"Efficiency", c.Efficiency}, {"Restfulness", c.Restfulness},
		{"REM", c.RemSleep}, {"Deep sleep", c.DeepSleep}, {"Latency", c.Latency}, {"Timing", c.Timing},
	} {
		if p.v != nil && *p.v < heldBackBelow {
			low = append(low, p)
		}
	}
	if len(low) == 0 {
		d.HeldBack = "Every part of sleep scored 70 or better"
		return nil
	}
	sort.SliceStable(low, func(i, j int) bool { return *low[i].v < *low[j].v })
	var names []string
	for _, p := range low[:min(3, len(low))] {
		names = append(names, fmt.Sprintf("%s %d", p.name, *p.v))
	}
	d.HeldBack = "Held back by: " + strings.Join(names, " · ")
	return nil
}

const dayLayout = "2006-01-02"

// deltaText words a difference from the average. There is no color on e-ink
// to carry direction, so the sign does, with a true minus sign.
func deltaText(diff float64, format func(float64) string) string {
	size := format(math.Abs(diff))
	if size == format(0) { // rounds to zero in its own units
		return "level with 7-day avg"
	}
	sign := "+"
	if diff < 0 {
		sign = "−"
	}
	return sign + size + " vs 7-day avg"
}

// relativeDay names day relative to today.
func relativeDay(day, today time.Time) string {
	switch int(today.Sub(day).Hours() / 24) {
	case 0:
		return "today"
	case 1:
		return "yesterday"
	}
	return day.Format("Jan 2")
}

// date strips t to midnight UTC on its calendar date, matching how days are
// parsed from the database, so day arithmetic never meets a DST change.
func date(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// score formats a 0-100 score.
func score(v float64) string { return fmt.Sprintf("%.0f", v) }

// thousands formats a count with comma separators: 6765 -> "6,765".
func thousands(v float64) string {
	s := fmt.Sprintf("%.0f", math.Round(v))
	neg := strings.HasPrefix(s, "-")
	s = strings.TrimPrefix(s, "-")
	var b strings.Builder
	for i, c := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if neg {
		return "-" + b.String()
	}
	return b.String()
}

// duration formats seconds as hours and minutes: 5400 -> "1h 30m".
func duration(seconds float64) string { return chart.Duration(seconds / 60) }

// fillRings sets today's rings, the week in review's rings for today. Sleep
// and readiness are final by morning. The inner ring is progress toward
// Oura's activity goal for the day — active calories against the target,
// which Oura sets from readiness — rather than the activity score, which
// judges the whole day and can fall as easily as rise. Only today's values
// count: a ring never shows yesterday's number under today's date.
func fillRings(ctx context.Context, db *store.DB, d *chart.Daily, today time.Time) error {
	day := today.Format(dayLayout)
	get := func(metric string) (float64, bool, error) {
		samples, err := db.Range(ctx, metric, 1, day, day)
		if err != nil || len(samples) == 0 {
			return 0, false, err
		}
		return samples[0].Value, true, nil
	}
	r := chart.WeekDay{Day: today, ActivityGoal: true}
	var err error
	if r.Sleep, r.HasSleep, err = get(store.MetricSleep); err != nil {
		return err
	}
	if r.Readiness, r.HasReadiness, err = get(store.MetricReadiness); err != nil {
		return err
	}
	active, hasActive, err := get(store.MetricActiveCal)
	if err != nil {
		return err
	}
	goal, hasGoal, err := get(store.MetricGoalCal)
	if err != nil {
		return err
	}
	left, hasLeft, err := get(store.MetricGoalLeft)
	if err != nil {
		return err
	}
	d.GoalNote, d.TotalCal = "no goal yet today", "—"
	if total, ok, err := get(store.MetricTotalCal); err != nil {
		return err
	} else if ok {
		d.TotalCal = thousands(total)
	}
	if hasActive {
		d.CalNote = thousands(active) + " active cal"
	}
	if hasActive && hasGoal && goal > 0 {
		d.CalNote = fmt.Sprintf("%s of %s cal", thousands(active), thousands(goal))
		r.Activity, r.HasActivity = math.Floor(active/goal*100), true
		switch {
		case r.Activity >= 100:
			d.GoalNote = "goal met"
		case hasLeft && left > 0:
			d.GoalNote = distanceToGo(left)
		default:
			d.GoalNote = ""
		}
	}
	d.Rings, d.HasRings = r, r.HasSleep || r.HasReadiness || r.HasActivity
	return nil
}

// distanceToGo words the walking distance left to the activity goal.
func distanceToGo(meters float64) string {
	if meters < 1000 {
		return fmt.Sprintf("%.0f m to goal", math.Round(meters/50)*50)
	}
	return fmt.Sprintf("%.1f km to goal", meters/1000)
}
