package dashboard

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/habits"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// nightHabits is what was logged before one night: each habit's total, and
// the time of its last log.
type nightHabits struct {
	count map[string]float64
	last  map[string]time.Time
}

// habitNights gathers the habit log and Oura's own tags by the night they
// come before (habits.Night), for the nights waking from from to to,
// inclusive: wake-up day → what was logged.
func habitNights(ctx context.Context, db *store.DB, from, to time.Time, loc *time.Location) (map[string]*nightHabits, error) {
	out := map[string]*nightHabits{}
	beds, usual, err := bedtimes(ctx, db, from, to, loc)
	if err != nil {
		return nil, err
	}
	add := func(night time.Time, name string, n float64, at time.Time) {
		if night.Before(from) || night.After(to) {
			return
		}
		// a timed habit is judged against that night's bedtime, or the
		// usual one for a night not slept yet: late counts as its late
		// form, and the ordinary form of a quiet habit (a meal) is skipped
		if h := habits.ByName(name); h.LateWithin > 0 {
			bed, ok := beds[night.Format(dayLayout)]
			if !ok {
				bed = usual(night)
			}
			switch {
			case h.IsLate(at, bed):
				name = h.LateName
			case h.Quiet:
				return
			}
		}
		k := night.Format(dayLayout)
		nh := out[k]
		if nh == nil {
			nh = &nightHabits{count: map[string]float64{}, last: map[string]time.Time{}}
			out[k] = nh
		}
		h := habits.ByName(name)
		if h.Counted {
			nh.count[h.Name] += n
		} else {
			nh.count[h.Name] = 1
		}
		if at.After(nh.last[h.Name]) {
			nh.last[h.Name] = at
		}
	}

	// a log's night is the day after its day, and its day starts at 4 AM:
	// the logs for nights from..to run from 4 AM the day before from
	start := time.Date(from.Year(), from.Month(), from.Day()-1, 4, 0, 0, 0, loc)
	end := time.Date(to.Year(), to.Month(), to.Day(), 4, 0, 0, 0, loc)
	logs, err := db.HabitLogsBetween(ctx, start, end, loc)
	if err != nil {
		return nil, err
	}
	for _, l := range logs {
		add(habits.Night(l.At), l.Habit, l.Count, l.At)
	}

	tags, err := db.TagsBetween(ctx, from.AddDate(0, 0, -1).Format(dayLayout), to.Format(dayLayout))
	if err != nil {
		return nil, err
	}
	for _, t := range tags {
		at, err := time.Parse(time.RFC3339, t.StartTime)
		if err != nil {
			continue
		}
		name := tagName(t)
		if h := habits.ByName(name); h.LateWithin > 0 && t.TypeCode != nil && strings.Contains(*t.TypeCode, "late") {
			name = h.LateName // Oura's "late meal" says it was late
		}
		if name != "" {
			add(habits.Night(at.In(loc)), name, 1, at.In(loc))
		}
	}
	return out, nil
}

// bedtimes reads when each night from from to to began, by wake-up day,
// and gives the usual bedtime: the average clock time of the two weeks of
// nights before to, placed on the evening before a given wake-up day.
func bedtimes(ctx context.Context, db *store.DB, from, to time.Time, loc *time.Location) (map[string]time.Time, func(time.Time) time.Time, error) {
	periods, err := db.SleepPeriodsBetween(ctx, from.AddDate(0, 0, -14).Format(dayLayout), to.Format(dayLayout))
	if err != nil {
		return nil, nil, err
	}
	byDay := map[string][]store.SleepPeriod{}
	for _, p := range periods {
		byDay[p.Day] = append(byDay[p.Day], p)
	}
	beds := map[string]time.Time{}
	var clock []float64 // minutes after noon, so midnight doesn't split them
	for day, ps := range byDay {
		p, ok := mainPeriod(ps)
		if !ok {
			continue
		}
		t, err := time.Parse(time.RFC3339, p.BedtimeStart)
		if err != nil {
			continue
		}
		t = t.In(loc)
		beds[day] = t
		if d, _ := time.Parse(dayLayout, day); !d.Before(to.AddDate(0, 0, -14)) {
			clock = append(clock, float64((t.Hour()+12)%24*60+t.Minute()))
		}
	}
	avg := 22.5*60 - 12*60 // 10:30 PM until there are nights to go by
	if len(clock) > 0 {
		avg = mean(clock)
	}
	usual := func(wake time.Time) time.Time {
		eve := wake.AddDate(0, 0, -1)
		return time.Date(eve.Year(), eve.Month(), eve.Day(), 12, 0, 0, 0, loc).Add(time.Duration(avg) * time.Minute)
	}
	return beds, usual, nil
}

// UsualBedtime is the bedtime to expect tonight, for a log at now: the
// average of the last two weeks' bedtimes, on the evening of now's day.
func UsualBedtime(ctx context.Context, db *store.DB, now time.Time) (time.Time, error) {
	night := habits.Night(now)
	_, usual, err := bedtimes(ctx, db, night.AddDate(0, 0, -1), night, now.Location())
	if err != nil {
		return time.Time{}, err
	}
	return usual(night), nil
}

// tagName is the habit an Oura tag counts as: the catalog's, for a tag that
// means one of its habits; a custom tag's own name; or another of Oura's
// tags named from its code ("tag_generic_nap" → "Nap").
func tagName(t store.Tag) string {
	code := ""
	if t.TypeCode != nil {
		code = *t.TypeCode
	}
	if h, ok := habits.ForOuraTag(code); ok {
		return h.Name
	}
	if t.CustomName != nil && strings.TrimSpace(*t.CustomName) != "" {
		return strings.TrimSpace(*t.CustomName)
	}
	for _, p := range []string{"tag_generic_", "tag_sleep_", "tag_"} {
		code = strings.TrimPrefix(code, p)
	}
	if code == "" || code == "custom" {
		return ""
	}
	name := strings.ReplaceAll(code, "_", " ")
	return strings.ToUpper(name[:1]) + name[1:]
}

// habitOrder sorts habit names by the catalog's order, others after by name.
func habitOrder(names []string) {
	pos := func(n string) int {
		for i, h := range habits.Catalog {
			if h.Name == n || (h.LateName != "" && h.LateName == n) {
				return i
			}
		}
		return len(habits.Catalog)
	}
	sort.Slice(names, func(i, j int) bool {
		pi, pj := pos(names[i]), pos(names[j])
		if pi != pj {
			return pi < pj
		}
		return names[i] < names[j]
	})
}

// nightStats are the figures of one night the habit charts compare.
type nightStats struct {
	score, hrv, lowHR, deep, asleep float64
	hasScore, hasHRV, hasLowHR      bool
	hasDeep, hasAsleep              bool
}

// nightStatsBetween reads each night's sleep score and its main period's
// figures for the nights waking from from to to: wake-up day → figures.
func nightStatsBetween(ctx context.Context, db *store.DB, from, to time.Time) (map[string]nightStats, error) {
	out := map[string]nightStats{}
	f, t := from.Format(dayLayout), to.Format(dayLayout)
	scores, err := db.Range(ctx, store.MetricSleep, int(to.Sub(from).Hours()/24)+2, f, t)
	if err != nil {
		return nil, err
	}
	for _, s := range scores {
		ns := out[s.Day]
		ns.score, ns.hasScore = s.Value, true
		out[s.Day] = ns
	}
	periods, err := db.SleepPeriodsBetween(ctx, f, t)
	if err != nil {
		return nil, err
	}
	byDay := map[string][]store.SleepPeriod{}
	for _, p := range periods {
		byDay[p.Day] = append(byDay[p.Day], p)
	}
	for day, ps := range byDay {
		p, ok := mainPeriod(ps)
		if !ok {
			continue
		}
		ns := out[day]
		if p.AverageHRV != nil && *p.AverageHRV > 0 {
			ns.hrv, ns.hasHRV = float64(*p.AverageHRV), true
		}
		if p.LowestHR != nil && *p.LowestHR > 0 {
			ns.lowHR, ns.hasLowHR = float64(*p.LowestHR), true
		}
		if p.DeepSleep != nil {
			ns.deep, ns.hasDeep = float64(*p.DeepSleep)/60, true
		}
		if p.TotalSleep != nil {
			ns.asleep, ns.hasAsleep = float64(*p.TotalSleep)/60, true
		}
		out[day] = ns
	}
	return out, nil
}

// HabitWeek builds the habits week for the week starting start (a Sunday):
// each day's habits and the night that followed, the best and toughest of
// those nights by sleep score, and the week's totals against the week
// before.
func HabitWeek(ctx context.Context, db *store.DB, start, today time.Time, loc *time.Location) (chart.HabitWeek, error) {
	start, today = date(start), date(today)
	end := start.AddDate(0, 0, 6)
	w := chart.HabitWeek{Title: "Habits · Week", Best: -1, Worst: -1,
		Subtitle: start.Format("Jan 2") + " – " + end.Format("Jan 2") + " · what you logged each day, and the night after"}
	if !today.After(end) {
		w.Subtitle += " · so far"
	}
	// a day's habits come before the night waking the next day
	logs, err := habitNights(ctx, db, start.AddDate(0, 0, -6), end.AddDate(0, 0, 1), loc)
	if err != nil {
		return w, err
	}
	nights, err := nightStatsBetween(ctx, db, start.AddDate(0, 0, 1), end.AddDate(0, 0, 1))
	if err != nil {
		return w, err
	}
	seen := map[string]bool{}
	for i := range w.Days {
		day := start.AddDate(0, 0, i)
		wake := day.AddDate(0, 0, 1)
		d := chart.HabitDay{Day: day, Future: day.After(today), Tonight: !day.After(today) && wake.After(today)}
		if nh := logs[wake.Format(dayLayout)]; nh != nil {
			var names []string
			for n := range nh.count {
				names = append(names, n)
			}
			habitOrder(names)
			for _, n := range names {
				h := habits.ByName(n)
				b := chart.HabitBadge{Code: h.Code, Name: h.Name, Count: nh.count[n], Counted: h.Counted, Late: h.Late}
				if h.Counted {
					b.Last = strings.Replace(nh.last[n].Format("3:04 PM"), ":00", "", 1)
				}
				d.Badges = append(d.Badges, b)
				if !seen[n] {
					seen[n] = true
					w.Key = append(w.Key, b)
				}
			}
		}
		if ns, ok := nights[wake.Format(dayLayout)]; ok && !d.Tonight && !d.Future {
			d.HasNight = true
			if ns.hasScore {
				d.Score, d.ScoreVal, d.HasScoreValue = score(ns.score), ns.score, true
			}
			if ns.hasAsleep {
				d.Asleep = chart.Duration(ns.asleep)
			}
			if ns.hasDeep {
				d.Deep = chart.Duration(ns.deep)
			}
			if ns.hasHRV {
				d.HRV = fmt.Sprintf("%.0f ms", ns.hrv)
			}
		}
		w.Days[i] = d
	}
	// the best and toughest of the nights that followed, with three to compare
	n := 0
	for i, d := range w.Days {
		if !d.HasScoreValue {
			continue
		}
		n++
		if w.Best < 0 || d.ScoreVal > w.Days[w.Best].ScoreVal {
			w.Best = i
		}
		if w.Worst < 0 || d.ScoreVal < w.Days[w.Worst].ScoreVal {
			w.Worst = i
		}
	}
	if n < 3 || w.Best == w.Worst {
		w.Best, w.Worst = -1, -1
	}

	// the week's totals against the week before, the most logged first
	total := func(from time.Time) map[string]float64 {
		t := map[string]float64{}
		for i := 0; i < 7; i++ {
			if nh := logs[from.AddDate(0, 0, i+1).Format(dayLayout)]; nh != nil {
				for k, v := range nh.count {
					t[k] += v
				}
			}
		}
		return t
	}
	this, last := total(start), total(start.AddDate(0, 0, -7))
	var names []string
	for k := range this {
		names = append(names, k)
	}
	for k := range last {
		if _, ok := this[k]; !ok {
			names = append(names, k)
		}
	}
	habitOrder(names)
	sort.SliceStable(names, func(i, j int) bool { return this[names[i]] > this[names[j]] })
	for _, k := range names {
		h := habits.ByName(k)
		value := h.Amount(this[k])
		if !h.Counted {
			value = plural(int(this[k]), fmt.Sprintf("%.0f day", this[k]), fmt.Sprintf("%.0f days", this[k]))
		}
		change := "none last week"
		if d := this[k] - last[k]; last[k] > 0 || this[k] == 0 {
			switch {
			case d == 0:
				change = "same as last week"
			default:
				change = signedNum(d) + " vs last week"
			}
		}
		w.Stats = append(w.Stats, chart.StatLine3{Label: h.Name, Value: shortAmount(h, this[k], value), Change: change})
	}
	logged := 0
	for _, d := range w.Days {
		if len(d.Badges) > 0 {
			logged++
		}
	}
	w.Headline = plural(logged, "1 day logged", fmt.Sprintf("%d days logged", logged))
	return w, nil
}

// shortAmount is a week's total for the strip: "6", "3 days".
func shortAmount(h habits.Habit, n float64, long string) string {
	if !h.Counted {
		return long
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

func signedNum(d float64) string {
	s := strconv.FormatFloat(math.Abs(d), 'f', -1, 64)
	if d < 0 {
		return "−" + s
	}
	return "+" + s
}

// effectMin is how many nights a habit needs, with it and without it,
// before its effect is shown.
const effectMin = 5

// HabitEffects builds the habit effects over the nights of the last days:
// for each habit logged in them, the nights after it against the nights
// without, on sleep score, HRV, lowest heart rate and deep sleep.
func HabitEffects(ctx context.Context, db *store.DB, days int, today time.Time, loc *time.Location) (chart.HabitEffects, error) {
	today = date(today)
	from := today.AddDate(0, 0, -days+1)
	e := chart.HabitEffects{
		Title:     "Habit effects",
		Subtitle:  fmt.Sprintf("The nights after each habit against the nights without, last %d days", days),
		MinNights: effectMin,
		Metrics: [4]chart.EffectMetric{
			{Label: "Sleep score", Unit: "points", Full: 10},
			{Label: "HRV", Unit: "ms", Full: 12},
			{Label: "Lowest HR", Unit: "bpm", Full: 5, LowerBetter: true},
			{Label: "Deep sleep", Unit: "min", Full: 30},
		},
		PanelHead: fmt.Sprintf("Last %d days", days),
		LeadLabel: "Nights logged",
	}
	logs, err := habitNights(ctx, db, from, today, loc)
	if err != nil {
		return e, err
	}
	nights, err := nightStatsBetween(ctx, db, from, today)
	if err != nil {
		return e, err
	}
	counts := map[string]int{}
	for _, nh := range logs {
		for k := range nh.count {
			counts[k]++
		}
	}
	e.LeadValue = fmt.Sprint(len(logs))
	e.PanelNote = plural(len(nights), "1 night", fmt.Sprintf("%d nights", len(nights)))
	var names []string
	for k := range counts {
		names = append(names, k)
	}
	habitOrder(names)
	sort.SliceStable(names, func(i, j int) bool { return counts[names[i]] > counts[names[j]] })

	metric := func(ns nightStats, j int) (float64, bool) {
		switch j {
		case 0:
			return ns.score, ns.hasScore
		case 1:
			return ns.hrv, ns.hasHRV
		case 2:
			return ns.lowHR, ns.hasLowHR
		}
		return ns.deep, ns.hasDeep
	}
	var clearest *chart.HabitEffect
	for _, k := range names {
		h := habits.ByName(k)
		row := chart.HabitEffect{Code: h.Code, Name: h.Name, Late: h.Late}
		var with, without [4][]float64
		for day, ns := range nights {
			had := logs[day] != nil && logs[day].count[k] > 0
			if had {
				row.Nights++
			} else {
				row.Without++
			}
			for j := 0; j < 4; j++ {
				if v, ok := metric(ns, j); ok {
					if had {
						with[j] = append(with[j], v)
					} else {
						without[j] = append(without[j], v)
					}
				}
			}
		}
		row.Enough = row.Nights >= effectMin && row.Without >= effectMin
		for j := 0; j < 4; j++ {
			if len(with[j]) > 0 && len(without[j]) > 0 {
				row.Diff[j], row.Has[j] = mean(with[j])-mean(without[j]), true
			}
		}
		e.Rows = append(e.Rows, row)
		if row.Enough && row.Has[0] && (clearest == nil || math.Abs(row.Diff[0]) > math.Abs(clearest.Diff[0])) {
			r := row
			clearest = &r
		}
	}
	if len(e.Rows) > 8 {
		e.Rows = e.Rows[:8]
	}
	for i, k := range names {
		if i >= 5 {
			break
		}
		e.Panel = append(e.Panel, chart.PanelRow{Label: k, Value: fmt.Sprint(counts[k]), Note: plural(counts[k], "night", "nights")})
	}
	switch {
	case len(names) == 0:
		e.Headline = "nothing logged yet"
		e.Conclusion = "Log a habit with the Siri shortcut; its effect shows once it has a few nights."
	case clearest != nil && math.Abs(clearest.Diff[0]) < 2:
		e.Headline = "no clear effect yet"
		e.Conclusion = "No habit has moved your sleep score by 2 points or more yet."
	case clearest == nil:
		e.Headline = "gathering nights"
		e.Conclusion = fmt.Sprintf("Each habit needs %d nights with it and %d without before its effect shows.", effectMin, effectMin)
	default:
		word := "better"
		if clearest.Diff[0] < 0 {
			word = "worse"
		}
		e.Headline = fmt.Sprintf("%s: sleep score %s", strings.ToLower(clearest.Name), signedNum(math.Round(clearest.Diff[0])))
		e.Conclusion = fmt.Sprintf("Nights after %s scored %.0f points %s than nights without.", strings.ToLower(clearest.Name), math.Abs(clearest.Diff[0]), word)
	}
	return e, nil
}
