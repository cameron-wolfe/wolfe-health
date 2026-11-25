// Package fetch pulls Oura data into the database: one call fetches every
// collection over a date range and stores it. The one-shot command and the
// daemon's schedule both go through Run.
package fetch

import (
	"context"
	"fmt"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/oura"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// Result counts what one Run stored.
type Result struct {
	From, To string // the inclusive date range fetched
	Samples  int    // daily metric values
	Nights   int    // nights of sleep contributors
	Periods  int    // sleep periods

	More     map[string]int // rows stored from each further collection
	Warnings []string       // further collections that failed; the rest went ahead
}

func (r Result) String() string {
	s := fmt.Sprintf("%s to %s: %d samples, %d nights of contributors, %d sleep periods",
		r.From, r.To, r.Samples, r.Nights, r.Periods)
	if len(r.More) > 0 {
		s += "; " + summarize(r.More)
	}
	for _, w := range r.Warnings {
		s += "; skipped " + w
	}
	return s
}

// activityEnd is the end date to ask daily_activity for, to cover through
// to. Unlike the other daily collections, daily_activity leaves out its end
// date, so asking through to itself never returns today's day in progress.
func activityEnd(to string) string {
	t, err := time.Parse("2006-01-02", to)
	if err != nil {
		return to
	}
	return oura.FormatDate(t.AddDate(0, 0, 1))
}

// Run fetches the trailing days (ending today) of every collection and
// stores them. Each table is written in its own transaction, so a failure
// partway leaves the earlier tables updated and the later ones as they were;
// the next run fills them in, since every write is an upsert.
func Run(ctx context.Context, c *oura.Client, db *store.DB, days int) (Result, error) {
	end := time.Now()
	r := Result{From: oura.FormatDate(end.AddDate(0, 0, -days)), To: oura.FormatDate(end)}

	samples, err := fetchDayMetrics(ctx, c, r.From, r.To)
	if err != nil {
		return r, err
	}
	if err := db.Save(ctx, samples); err != nil {
		return r, err
	}
	r.Samples = len(samples)

	nights, err := fetchSleepContributors(ctx, c, r.From, r.To)
	if err != nil {
		return r, err
	}
	if err := db.SaveSleepContributors(ctx, nights); err != nil {
		return r, err
	}
	r.Nights = len(nights)

	periods, err := fetchSleepPeriods(ctx, c, r.From, r.To)
	if err != nil {
		return r, err
	}
	if err := db.SaveSleepPeriods(ctx, periods); err != nil {
		return r, err
	}
	r.Periods = len(periods)

	r.More, r.Warnings = fetchMore(ctx, c, db, r.From, r.To)
	return r, nil
}

// fetchSleepContributors pulls daily_sleep and keeps the contributor breakdown
// that fetchDayMetrics throws away.
//
// This re-requests the same collection fetchDayMetrics already fetched. That is
// one extra HTTP call per run against a 5000-per-5-minutes rate limit, bought
// in exchange for each function owning one table — worth revisiting only if the
// two tables ever need to be written from a single consistent snapshot.
func fetchSleepContributors(ctx context.Context, c *oura.Client,
	from string, to string) ([]store.SleepContributors, error) {

	sleep, err := c.Sleep(ctx, from, to)
	if err != nil {
		return nil, err
	}

	// one row per night, sized up front since we know the count
	nights := make([]store.SleepContributors, 0, len(sleep))
	for _, d := range sleep {
		// copy the pointers straight across - a nil contributor stays nil and
		// lands as NULL, rather than becoming a zero that reads as a real score
		nights = append(nights, store.SleepContributors{
			Day:         d.Day,
			DeepSleep:   d.Contributors.DeepSleep,
			Efficiency:  d.Contributors.Efficiency,
			Latency:     d.Contributors.Latency,
			RemSleep:    d.Contributors.RemSleep,
			Restfulness: d.Contributors.Restfulness,
			Timing:      d.Contributors.Timing,
			TotalSleep:  d.Contributors.TotalSleep,
		})
	}
	return nights, nil
}

// fetchSleepPeriods pulls the sleep collection: one record per period of
// sleep, with the stage-by-stage breakdown and heart rate the night chart
// draws. The fields copy across one for one.
func fetchSleepPeriods(ctx context.Context, c *oura.Client, from, to string) ([]store.SleepPeriod, error) {
	periods, err := c.SleepPeriods(ctx, from, to)
	if err != nil {
		return nil, err
	}
	series := func(s *oura.Sample) *store.Series {
		if s == nil {
			return nil
		}
		return &store.Series{Interval: s.Interval, Items: s.Items, Start: s.Timestamp}
	}
	out := make([]store.SleepPeriod, 0, len(periods))
	for _, p := range periods {
		out = append(out, store.SleepPeriod{
			ID: p.ID, Day: p.Day, Type: p.Type,
			BedtimeStart: p.BedtimeStart, BedtimeEnd: p.BedtimeEnd,
			TimeInBed: p.TimeInBed, TotalSleep: p.TotalSleep,
			DeepSleep: p.DeepSleep, LightSleep: p.LightSleep, RemSleep: p.RemSleep,
			AwakeTime: p.AwakeTime, Efficiency: p.Efficiency, Latency: p.Latency,
			LowestHR: p.LowestHR, AverageHRV: p.AverageHRV, RestlessCount: p.RestlessCount,
			AverageHR: p.AverageHR, AverageBreath: p.AverageBreath,
			Phases5Min: p.Phases5Min, Phases30Sec: p.Phases30Sec, Movement30s: p.Movement30s,
			HeartRate: series(p.HeartRate), HRV: series(p.HRV),
		})
	}
	return out, nil
}

// fetchDayMetrics pulls every daily collection and flattens it into samples.
func fetchDayMetrics(ctx context.Context, c *oura.Client, from string, to string) ([]store.Sample, error) {

	// array of sample structs. we create a new samples for each day which we fill with the days data
	var samples []store.Sample

	// type sleep calls the api with endpoint daily_sleep
	sleep, err := c.Sleep(ctx, from, to)
	if err != nil {
		return nil, err
	}
	for _, d := range sleep {
		// append sleep struct to day's samples
		samples = append(samples, store.Sample{
			Metric: store.MetricSleep, Day: d.Day, Value: float64(d.Score),
		})
	}

	// type readiness calls the api with endpoint daily_readiness
	readiness, err := c.Readiness(ctx, from, to)
	if err != nil {
		return nil, err
	}
	for _, d := range readiness {
		// append readiness struct to day's samples
		samples = append(samples,
			store.Sample{
				Metric: store.MetricReadiness, Day: d.Day, Value: float64(d.Score),
			})
		// temperature deviation rides along with readiness; skip nulls
		// rather than storing a zero that would read as "exactly normal"
		if d.TemperatureDev != nil {
			samples = append(samples, store.Sample{
				Metric: store.MetricTempDev, Day: d.Day, Value: *d.TemperatureDev,
			})
		}
		// the parts behind the score, for the readiness radar; a null part
		// is left out, not stored as a zero
		for _, part := range store.ReadinessParts {
			if v := d.Contributors[part]; v != nil {
				samples = append(samples, store.Sample{
					Metric: store.MetricReadinessPart + part, Day: d.Day, Value: float64(*v),
				})
			}
		}
	}

	// type stress calls the api with endpoint daily_stress
	stress, err := c.Stress(ctx, from, to)
	if err != nil {
		return nil, err
	}
	for _, d := range stress {
		// Recovery time comes in the same record; like stress, null means
		// "couldn't compute", not zero, so nulls are skipped.
		if d.RecoveryHigh != nil {
			samples = append(samples, store.Sample{
				Metric: store.MetricRecovery, Day: d.Day, Value: float64(*d.RecoveryHigh),
			})
		}
		// Oura returns null on days it could not compute stress. Skip those
		// rather than storing a zero, which would read as a genuine calm day.
		if d.StressHigh == nil {
			continue
		}
		// append stress struct to day's samples. DaySummary qualifies the
		// number: "restored", "normal" or "stressful".
		samples = append(samples,
			store.Sample{
				Metric: store.MetricStress, Day: d.Day, Value: float64(*d.StressHigh),
				Qualifier: d.DaySummary,
			})
	}

	activity, err := c.Activity(ctx, from, activityEnd(to))
	if err != nil {
		return nil, err
	}
	for _, d := range activity {
		// append activity structs to day's samples
		samples = append(samples,
			store.Sample{
				Metric: store.MetricActivity, Day: d.Day, Value: float64(d.Score),
			},
			store.Sample{
				Metric: store.MetricSteps, Day: d.Day, Value: float64(d.Steps),
			},
			store.Sample{
				Metric: store.MetricActiveCal, Day: d.Day, Value: float64(d.ActiveCal),
			},
			store.Sample{
				Metric: store.MetricTotalCal, Day: d.Day, Value: float64(d.TotalCal),
			},
		)
		if d.TargetCal > 0 { // the goal, and how far off it is
			samples = append(samples,
				store.Sample{Metric: store.MetricGoalCal, Day: d.Day, Value: float64(d.TargetCal)},
				store.Sample{Metric: store.MetricGoalLeft, Day: d.Day, Value: float64(d.MetersToGo)},
			)
		}
	}

	return samples, nil
}

// RetryDelays is how long to wait after the first, second, third failure in
// a row, and so on: soon at first, so a short outage costs minutes rather
// than a whole interval, then backing off; the last repeats. A boot is the
// common case: a board with no battery clock (most ARM boards, a Raspberry Pi)
// starts with the wrong time, Oura's certificate fails to verify until the
// clock is set from the network, and the retry a minute later succeeds.
var RetryDelays = []time.Duration{time.Minute, 2 * time.Minute, 5 * time.Minute, 15 * time.Minute, 30 * time.Minute, time.Hour}

// RetryDelay is how long to wait after the given number of failures in a row
// (1 or more): delays[failures-1], the last repeating, never more than
// most.
func RetryDelay(delays []time.Duration, failures int, most time.Duration) time.Duration {
	if failures < 1 || len(delays) == 0 {
		return most
	}
	return min(delays[min(failures, len(delays))-1], most)
}

// Every calls fn now and then once per interval until ctx is cancelled. When
// fn fails it's called again sooner, after RetryDelay of retries for the
// failures in a row, never later than the interval, and back on the interval
// once it succeeds. fn is never called concurrently with itself: the next
// wait starts when a run ends. next, if non-nil, is told when the following
// run is due, for status display.
func Every(ctx context.Context, interval time.Duration, retries []time.Duration, fn func(context.Context) error, next func(time.Time)) {
	failures := 0
	for {
		wait := interval
		if err := fn(ctx); err != nil {
			failures++
			wait = RetryDelay(retries, failures, interval)
		} else {
			failures = 0
		}
		if next != nil {
			next(time.Now().Add(wait))
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return
		case <-t.C:
		}
	}
}
