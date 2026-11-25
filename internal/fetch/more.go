package fetch

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/oura"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// timeSeriesChunk is how much of a time-series collection (heart rate,
// battery) one request covers. Long ranges — the start-up back-fill — are
// split, since those collections hold a reading every few minutes and Oura
// limits how wide a range one request may ask for.
const timeSeriesChunk = 7 * 24 * time.Hour

// resilienceLevels numbers Oura's resilience levels, lowest first, so the
// level charts like any other metric.
var resilienceLevels = map[string]float64{"limited": 1, "adequate": 2, "solid": 3, "strong": 4, "exceptional": 5}

// fetchMore fetches every collection beyond the daily scores and sleep, and
// stores each as it arrives. Each is best effort: one that fails — a scope
// the login didn't grant, a ring that doesn't measure it, a brief outage —
// is reported in warnings and the rest carry on, so the core data never
// stops updating because of an extra. counts says how many rows each stored.
func fetchMore(ctx context.Context, c *oura.Client, db *store.DB, from, to string) (counts map[string]int, warnings []string) {
	counts = map[string]int{}
	try := func(name string, f func() (int, error)) {
		n, err := f()
		if err != nil {
			warnings = append(warnings, fmt.Sprintf("%s: %v", name, err))
			return
		}
		counts[name] = n
	}

	try("spo2", func() (int, error) {
		rows, err := c.SpO2(ctx, from, to)
		if err != nil {
			return 0, err
		}
		var s []store.Sample
		for _, r := range rows {
			if r.SpO2 != nil && r.SpO2.Average != nil {
				s = append(s, store.Sample{Metric: store.MetricSpO2, Day: r.Day, Value: *r.SpO2.Average})
			}
			if r.BreathingDisturbance != nil {
				s = append(s, store.Sample{Metric: store.MetricBreathingDI, Day: r.Day, Value: float64(*r.BreathingDisturbance)})
			}
		}
		return len(rows), db.Save(ctx, s)
	})
	try("cardiovascular age", func() (int, error) {
		rows, err := c.CardiovascularAge(ctx, from, to)
		if err != nil {
			return 0, err
		}
		var s []store.Sample
		for _, r := range rows {
			if r.VascularAge != nil {
				s = append(s, store.Sample{Metric: store.MetricCardioAge, Day: r.Day, Value: float64(*r.VascularAge)})
			}
			if r.PulseWaveVelocity != nil {
				s = append(s, store.Sample{Metric: store.MetricPulseWave, Day: r.Day, Value: *r.PulseWaveVelocity})
			}
		}
		return len(rows), db.Save(ctx, s)
	})
	try("resilience", func() (int, error) {
		rows, err := c.Resilience(ctx, from, to)
		if err != nil {
			return 0, err
		}
		var s []store.Sample
		for _, r := range rows {
			if v, ok := resilienceLevels[r.Level]; ok {
				s = append(s, store.Sample{Metric: store.MetricResilience, Day: r.Day, Value: v, Qualifier: r.Level})
			}
			s = append(s,
				store.Sample{Metric: store.MetricResSleep, Day: r.Day, Value: r.Contributors.SleepRecovery},
				store.Sample{Metric: store.MetricResDaytime, Day: r.Day, Value: r.Contributors.DaytimeRecovery},
				store.Sample{Metric: store.MetricResStress, Day: r.Day, Value: r.Contributors.Stress})
		}
		return len(rows), db.Save(ctx, s)
	})
	try("vo2 max", func() (int, error) {
		rows, err := c.VO2Max(ctx, from, to)
		if err != nil {
			return 0, err
		}
		s := make([]store.Sample, len(rows))
		for i, r := range rows {
			s[i] = store.Sample{Metric: store.MetricVO2Max, Day: r.Day, Value: r.VO2Max}
		}
		return len(rows), db.Save(ctx, s)
	})
	try("sleep time", func() (int, error) {
		rows, err := c.SleepTimes(ctx, from, to)
		if err != nil {
			return 0, err
		}
		out := make([]store.SleepTime, len(rows))
		for i, r := range rows {
			out[i] = store.SleepTime{Day: r.Day, Recommendation: r.Recommendation, Status: r.Status}
			if w := r.OptimalBedtime; w != nil {
				out[i].StartOffset, out[i].EndOffset, out[i].DayTZ = &w.StartOffset, &w.EndOffset, &w.DayTZ
			}
		}
		return len(rows), db.SaveSleepTimes(ctx, out)
	})
	try("workouts", func() (int, error) {
		rows, err := c.Workouts(ctx, from, to)
		if err != nil {
			return 0, err
		}
		out := make([]store.Workout, len(rows))
		for i, r := range rows {
			out[i] = store.Workout{ID: r.ID, Day: r.Day, Activity: r.Activity, Intensity: r.Intensity,
				Source: r.Source, Label: r.Label, Start: r.Start, End: r.End, Calories: r.Calories, Distance: r.Distance}
		}
		return len(rows), db.SaveWorkouts(ctx, out)
	})
	try("sessions", func() (int, error) {
		rows, err := c.Sessions(ctx, from, to)
		if err != nil {
			return 0, err
		}
		out := make([]store.Session, len(rows))
		for i, r := range rows {
			out[i] = store.Session{ID: r.ID, Day: r.Day, Type: r.Type, Mood: r.Mood, Start: r.Start, End: r.End,
				HeartRate: toSeries(r.HeartRate), HRV: toSeries(r.HRV)}
		}
		return len(rows), db.SaveSessions(ctx, out)
	})
	try("tags", func() (int, error) {
		rows, err := c.Tags(ctx, from, to)
		if err != nil {
			return 0, err
		}
		out := make([]store.Tag, len(rows))
		for i, r := range rows {
			out[i] = store.Tag{ID: r.ID, TypeCode: r.TypeCode, CustomName: r.CustomName, Comment: r.Comment,
				StartDay: r.StartDay, EndDay: r.EndDay, StartTime: r.StartTime, EndTime: r.EndTime}
		}
		return len(rows), db.SaveTags(ctx, out)
	})
	try("rest mode", func() (int, error) {
		rows, err := c.RestModePeriods(ctx, from, to)
		if err != nil {
			return 0, err
		}
		out := make([]store.RestModePeriod, len(rows))
		for i, r := range rows {
			out[i] = store.RestModePeriod{ID: r.ID, StartDay: r.StartDay, EndDay: r.EndDay,
				StartTime: r.StartTime, EndTime: r.EndTime}
			if len(r.Episodes) > 0 {
				e := string(r.Episodes)
				out[i].Episodes = &e
			}
		}
		return len(rows), db.SaveRestModePeriods(ctx, out)
	})

	start, end, err := instantRange(from, to)
	if err != nil {
		warnings = append(warnings, err.Error())
	} else {
		try("heart rate", func() (int, error) {
			n := 0
			for _, ch := range chunks(start, end, timeSeriesChunk) {
				rows, err := c.HeartRates(ctx, ch[0], ch[1])
				if err != nil {
					return n, err
				}
				out := make([]store.HeartRate, 0, len(rows))
				for _, r := range rows {
					t, err := time.Parse(time.RFC3339, r.Timestamp)
					if err != nil {
						return n, fmt.Errorf("bad timestamp %q: %w", r.Timestamp, err)
					}
					out = append(out, store.HeartRate{Time: t, BPM: r.BPM, Source: r.Source})
				}
				if err := db.SaveHeartRates(ctx, out); err != nil {
					return n, err
				}
				n += len(out)
			}
			return n, nil
		})
		try("ring battery", func() (int, error) {
			n := 0
			for _, ch := range chunks(start, end, timeSeriesChunk) {
				rows, err := c.BatteryLevels(ctx, ch[0], ch[1])
				if err != nil {
					return n, err
				}
				out := make([]store.BatteryLevel, 0, len(rows))
				for _, r := range rows {
					t, err := time.Parse(time.RFC3339, r.Timestamp)
					if err != nil {
						return n, fmt.Errorf("bad timestamp %q: %w", r.Timestamp, err)
					}
					out = append(out, store.BatteryLevel{Time: t, Level: r.Level, Charging: r.Charging, InCharger: r.InCharger})
				}
				if err := db.SaveBatteryLevels(ctx, out); err != nil {
					return n, err
				}
				n += len(out)
			}
			return n, nil
		})
	}

	try("ring configuration", func() (int, error) {
		rows, err := c.RingConfigurations(ctx)
		if err != nil {
			return 0, err
		}
		out := make([]store.RingConfiguration, len(rows))
		for i, r := range rows {
			out[i] = store.RingConfiguration{ID: r.ID, Color: r.Color, Design: r.Design, Firmware: r.Firmware,
				HardwareType: r.HardwareType, SetUpAt: r.SetUpAt, Size: r.Size}
		}
		return len(rows), db.SaveRingConfigurations(ctx, out)
	})
	try("personal info", func() (int, error) {
		p, err := c.Personal(ctx)
		if err != nil {
			return 0, err
		}
		return 1, db.SavePersonalInfo(ctx, store.PersonalInfo{ID: p.ID, Age: p.Age, Weight: p.Weight,
			Height: p.Height, BiologicalSex: p.BiologicalSex, Email: p.Email})
	})
	return counts, warnings
}

// toSeries converts Oura's sample series to the stored form.
func toSeries(s *oura.Sample) *store.Series {
	if s == nil {
		return nil
	}
	return &store.Series{Interval: s.Interval, Items: s.Items, Start: s.Timestamp}
}

// instantRange is the span the dates from..to cover, start of from to now
// (or the end of to, if that is sooner), in local time.
func instantRange(from, to string) (time.Time, time.Time, error) {
	f, err := time.ParseInLocation("2006-01-02", from, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("bad from date %q: %w", from, err)
	}
	t, err := time.ParseInLocation("2006-01-02", to, time.Local)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("bad to date %q: %w", to, err)
	}
	end := t.AddDate(0, 0, 1)
	if now := time.Now(); now.Before(end) {
		end = now
	}
	return f, end, nil
}

// chunks splits [start, end) into consecutive spans of at most size.
func chunks(start, end time.Time, size time.Duration) [][2]time.Time {
	var out [][2]time.Time
	for s := start; s.Before(end); s = s.Add(size) {
		e := s.Add(size)
		if e.After(end) {
			e = end
		}
		out = append(out, [2]time.Time{s, e})
	}
	return out
}

// summarize words what fetchMore stored: "spo2 7, heart rate 2,412, …".
func summarize(counts map[string]int) string {
	names := make([]string, 0, len(counts))
	for k := range counts {
		names = append(names, k)
	}
	sort.Strings(names)
	parts := make([]string, len(names))
	for i, k := range names {
		parts[i] = fmt.Sprintf("%s %d", k, counts[k])
	}
	return strings.Join(parts, ", ")
}
