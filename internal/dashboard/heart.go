package dashboard

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Heart-rate day.
const (
	heartHistoryDays = 28               // the usual band is built from these days before
	heartSlot        = 30 * time.Minute // the band's resolution through the day
	heartMinSlot     = 8                // fewer past readings in a slot: no band there
	heartStep        = 5 * time.Minute  // the most a single reading stands for
)

// HeartToday builds the heart-rate chart for day (a date; only its year,
// month and day are used), midnight to midnight in now's time zone, with
// readings up to now. Each half hour's usual is the middle half (25th to
// 75th percentile) of the readings at that time of day over the four weeks
// before, leaving out workouts, which would widen it for every day.
func HeartToday(ctx context.Context, db *store.DB, day, now time.Time) (chart.HeartDay, error) {
	loc := now.Location()
	start := time.Date(day.Year(), day.Month(), day.Day(), 0, 0, 0, 0, loc)
	end := start.AddDate(0, 0, 1)
	until := now
	if until.After(end) {
		until = end
	}
	isToday := now.Before(end) && !now.Before(start)
	h := chart.HeartDay{
		Title:    "Heart rate · " + start.Format("Monday, Jan 2"),
		Subtitle: "Against your usual for each time of day, from the past 4 weeks",
		Start:    start, End: end, Now: until, SlotLen: heartSlot,
	}
	if y := now.AddDate(0, 0, -1); y.Year() == start.Year() && y.YearDay() == start.YearDay() {
		h.Title = "Heart rate yesterday · " + start.Format("Monday, Jan 2")
	}
	if isToday {
		h.Title = "Heart rate today"
	}

	readings, err := db.HeartRatesBetween(ctx, start, until.Add(time.Second))
	if err != nil {
		return h, err
	}
	past, err := db.HeartRatesBetween(ctx, start.AddDate(0, 0, -heartHistoryDays), start)
	if err != nil {
		return h, err
	}

	// the usual, per slot of the day
	slotOf := func(t time.Time) int {
		t = t.In(loc)
		return (t.Hour()*60 + t.Minute()) / int(heartSlot.Minutes())
	}
	nSlots := int(24 * time.Hour / heartSlot)
	bySlot := make([][]float64, nSlots)
	for _, r := range past {
		if r.Source == "workout" {
			continue
		}
		i := slotOf(r.Time)
		bySlot[i] = append(bySlot[i], float64(r.BPM))
	}
	type usual struct{ lo, mid, hi float64 }
	usualAt := map[int]usual{}
	for i, xs := range bySlot {
		if len(xs) < heartMinSlot {
			continue
		}
		sort.Float64s(xs)
		u := usual{quantile(xs, 0.25), quantile(xs, 0.5), quantile(xs, 0.75)}
		usualAt[i] = u
		slotStart := time.Date(start.Year(), start.Month(), start.Day(), 0, i*int(heartSlot.Minutes()), 0, 0, loc)
		h.Usual = append(h.Usual, chart.HeartSlot{Start: slotStart, Lo: u.lo, Hi: u.hi})
	}

	// sleep, from the periods ending today and tomorrow (tonight's)
	periods, err := db.SleepPeriodsBetween(ctx, start.Format(dayLayout), start.AddDate(0, 0, 1).Format(dayLayout))
	if err != nil {
		return h, err
	}
	for _, p := range periods {
		if p.Type == "rest" {
			continue
		}
		if sp, ok := spanIn(p.BedtimeStart, p.BedtimeEnd, "", start, end); ok {
			h.Asleep = append(h.Asleep, sp)
		}
	}
	// A reading is asleep if it falls in a sleep period. Oura's source
	// label isn't enough: readings taken during sleep needn't carry
	// "sleep", and counting them as awake drags the awake figures down.
	asleep := func(t time.Time, source string) bool {
		if source == "sleep" {
			return true
		}
		for _, sp := range h.Asleep {
			if !t.Before(sp.Start) && t.Before(sp.End) {
				return true
			}
		}
		return false
	}

	// today's readings, and the figures they give
	var (
		awakeSum, usualSum float64
		awakeN, usualN     int
		lowAsleep          = -1.0
		above              time.Duration
	)
	for k, r := range readings {
		t, v := r.Time.In(loc), float64(r.BPM)
		h.Readings = append(h.Readings, chart.HeartPoint{Time: t, BPM: v})
		if !h.HasPeak || v > h.Peak.BPM {
			h.Peak, h.HasPeak = chart.HeartPoint{Time: t, BPM: v}, true
		}
		if asleep(t, r.Source) {
			if lowAsleep < 0 || v < lowAsleep {
				lowAsleep = v
			}
			continue
		}
		awakeSum += v
		awakeN++
		u, ok := usualAt[slotOf(t)]
		if !ok {
			continue
		}
		usualSum += u.mid
		usualN++
		if v > u.hi {
			step := heartStep
			if k+1 < len(readings) {
				if g := readings[k+1].Time.Sub(r.Time); g < step {
					step = g
				}
			}
			above += step
		}
	}

	// the day's workouts and sessions
	workouts, err := db.WorkoutsBetween(ctx, start.Format(dayLayout), start.Format(dayLayout))
	if err != nil {
		return h, err
	}
	var active time.Duration
	for _, w := range workouts {
		name := w.Activity
		if w.Label != nil && *w.Label != "" {
			name = *w.Label
		}
		if sp, ok := spanIn(w.Start, w.End, activityName(name), start, end); ok {
			h.Activities = append(h.Activities, sp)
			active += sp.End.Sub(sp.Start)
		}
	}
	sessions, err := db.SessionsBetween(ctx, start.Format(dayLayout), start.Format(dayLayout))
	if err != nil {
		return h, err
	}
	for _, s := range sessions {
		if sp, ok := spanIn(s.Start, s.End, activityName(s.Type), start, end); ok {
			h.Activities = append(h.Activities, sp)
			active += sp.End.Sub(sp.Start)
		}
	}

	bpm := func(v float64) string { return fmt.Sprintf("%.0f bpm", v) }
	if len(h.Readings) == 0 {
		h.Stats = append(h.Stats, chart.StatLine{Label: "No readings yet", Value: "—"})
		return h, nil
	}
	last := h.Readings[len(h.Readings)-1]
	if isToday {
		h.Headline = fmt.Sprintf("%.0f bpm at %s", last.BPM, last.Time.Format("3:04 PM"))
		h.Stats = append(h.Stats, chart.StatLine{Label: "Latest",
			Value: fmt.Sprintf("%.0f · %s", last.BPM, last.Time.Format("3:04 PM"))})
	}
	if awakeN > 0 {
		avg := awakeSum / float64(awakeN)
		if !isToday {
			h.Headline = "awake average " + bpm(avg)
		}
		h.Stats = append(h.Stats, chart.StatLine{Label: "Awake average", Value: bpm(avg)})
		if usualN > 0 {
			h.Stats = append(h.Stats, chart.StatLine{Label: "Usual, same hours",
				Value: bpm(usualSum / float64(usualN))})
		}
	}
	if lowAsleep >= 0 {
		h.Stats = append(h.Stats, chart.StatLine{Label: "Lowest asleep", Value: bpm(lowAsleep)})
	}
	h.Stats = append(h.Stats, chart.StatLine{Label: "Peak",
		Value: fmt.Sprintf("%.0f · %s", h.Peak.BPM, h.Peak.Time.Format("3:04 PM"))})
	if len(h.Usual) > 0 {
		h.Stats = append(h.Stats, chart.StatLine{Label: "Above usual", Value: duration(above.Seconds())})
	}
	if len(h.Activities) > 0 {
		h.Stats = append(h.Stats, chart.StatLine{Label: "Workouts, sessions",
			Value: fmt.Sprintf("%d · %s", len(h.Activities), duration(active.Seconds()))})
	}
	return h, nil
}

// spanIn parses an RFC 3339 start and end and clips them to the window,
// reporting whether any of the span falls inside it.
func spanIn(startS, endS, label string, from, to time.Time) (chart.Span, bool) {
	s, err1 := time.Parse(time.RFC3339, startS)
	e, err2 := time.Parse(time.RFC3339, endS)
	if err1 != nil || err2 != nil || !e.After(from) || !s.Before(to) {
		return chart.Span{}, false
	}
	loc := from.Location()
	s, e = s.In(loc), e.In(loc)
	if s.Before(from) {
		s = from
	}
	if e.After(to) {
		e = to
	}
	return chart.Span{Start: s, End: e, Label: label}, true
}

// activityName words an Oura activity or session type: "cycling",
// "strength training", "breathing".
func activityName(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "_", " ")
	return strings.TrimSpace(s)
}
