package server

// The display schedule: which screen an e-ink panel shows, when.
//
// The day is split into dayparts, each starting at a clock time and running
// until the next one starts (the last wraps past midnight). A daypart lists
// screens to rotate through, each held for HoldMinutes. Which screen is up is
// computed from the clock alone — no state to lose on a restart, and every
// display agrees.
//
// On the 1st of each month, last month's report joins the rotation of every
// daypart that rotates (unless SkipMonthly is set), and on Sundays last
// week's review does (unless SkipWeekly is set). Single-screen dayparts keep
// their one screen: the morning's last night, the overnight brief that never
// changes in a dark room.

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"log"
	"net/http"
	"net/url"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Daypart is one stretch of the day and the screens it rotates through.
type Daypart struct {
	Start   string   `json:"start"` // "HH:MM", local time
	Screens []string `json:"screens"`
}

// Schedule is the whole day's plan.
type Schedule struct {
	Dayparts    []Daypart `json:"dayparts"`
	HoldMinutes int       `json:"hold_minutes"`
	// SkipMonthly turns off last month's report on the 1st. Stored as an
	// opt-out so schedules saved before the feature existed get it too.
	SkipMonthly bool `json:"skip_monthly_report,omitempty"`
	// SkipWeekly turns off last week's review on Sundays; an opt-out for the
	// same reason.
	SkipWeekly bool `json:"skip_weekly_review,omitempty"`
	// AsOf holds the displays on a date of interest, YYYY-MM-DD: every
	// screen is drawn as of that day, as with ?date=, until it's cleared.
	// Empty is today, live.
	AsOf string `json:"as_of,omitempty"`
}

// screenOption is a screen the schedule can show, with its display name.
type screenOption struct{ Name, Title string }

// scheduleScreens are the screens a schedule may use, in menu order.
var scheduleScreens = func() []screenOption {
	opts := []screenOption{
		{scrDaily, "Daily brief"},
		{scrDigest, "Daily digest"},
		{scrNight, "Last night"},
		{scrVitals, "Vital signs"},
		{scrDeviation, "WolfeSense · Signals"},
		{scrMoon, "WolfePhase"},
		{scrPhaseMonth, "WolfePhase · Month"},
		{scrSleepStages, "Sleep stages"},
		{scrSleepSchedule, "Sleep schedule"},
		{scrPayoff, "Bedtime payoff"},
		{scrStressMonth, "Stress calendar"},
		{scrScoreMonth, "Score calendar"},
		{scrWeeklyRhythm, "Weekly rhythm"},
		{scrSleepBreakdown, "WolfeSense · Sleep"},
		{scrReadinessParts, "WolfeSense · Readiness"},
		{scrBodySignals, "Body signals"},
		{scrEarlyWarning, "Early warning"},
		{scrFitness, "Fitness and sleep"},
		{scrOxygen, "Oxygen tide"},
		{scrHeartToday, "Heart rate today"},
		{scrHeartYesterday, "Heart rate yesterday"},
		{scrMonthReport, "This month's report"},
		{scrLastMonth, "Last month's report"},
		{scrLastScores, "Last month's score calendar"},
		{scrLastStress, "Last month's stress calendar"},
		{scrLastPhase, "Last month's WolfePhase"},
		{scrWeekReview, "Last week's review"},
		{scrThisWeek, "This week so far"},
		{scrWeekRadar, "WolfeSense · Week"},
		{scrHabitWeek, "Habits · Week"},
		{scrHabitEffects, "Habit effects"},
	}
	return append(opts, screenOption{scrTrends, "Trends"})
}()

func screenTitle(name string) string {
	for _, o := range scheduleScreens {
		if o.Name == name {
			return o.Title
		}
	}
	return name
}

// DefaultSchedule follows the day, each screen when it's most use:
//
//   - the morning, 5 to 8: today's brief, then the night's details, in two
//     rotations that both lead with the brief: the night as a moon and as a
//     hypnogram, and WolfeSense · Sleep; then the body against its normal
//     (the early warning, vital signs) and WolfeSense · Readiness, so a
//     drift toward illness is seen on waking;
//   - 8 to 2, progress: this week so far against your usual weeks, the
//     weekly rhythm and the trends; then last week's review, last night's
//     signals as a radar, and the longer-running ones (fitness, blood
//     oxygen);
//   - 2 to bedtime, encouragement toward a good night: when you sleep, how
//     the stages run, and the bedtime that pays off;
//   - and overnight, bedtime payoff alone: one unchanging screen, so a panel
//     in a dark room never flashes.
//
// The month's calendars aren't in the rotation: on the 1st each rotation
// leads with last month whole (reviewScreens), and on Sundays with last
// week's review. Screens hold 15 minutes, a TRMNL's usual wake interval.
func DefaultSchedule() Schedule {
	return Schedule{
		HoldMinutes: 15,
		Dayparts: []Daypart{
			{Start: "05:00", Screens: []string{scrDaily, scrMoon, scrNight, scrSleepBreakdown}},
			{Start: "06:30", Screens: []string{scrDaily, scrEarlyWarning, scrVitals, scrReadinessParts}},
			{Start: "08:00", Screens: []string{scrThisWeek, scrWeekRadar, scrWeeklyRhythm, scrTrends}},
			{Start: "11:00", Screens: []string{scrWeekReview, scrDeviation, scrFitness, scrOxygen}},
			{Start: "14:00", Screens: []string{scrSleepSchedule, scrSleepStages, scrPayoff}},
			{Start: "22:00", Screens: []string{scrPayoff}},
		},
	}
}

// monthlyScreens lead every rotation on the 1st: how last month panned out,
// as its report and its three calendars.
var monthlyScreens = []string{scrLastMonth, scrLastScores, scrLastStress, scrLastPhase}

const (
	maxScreensPerPart = 4
	// a minute up to half a day. Short holds are allowed, for a panel on
	// mains power, but each change wakes a TRMNL and fully refreshes its
	// screen, so the default holds 15 minutes, its usual wake interval.
	minHold, maxHold = 1, 720
	scheduleKey      = "display_schedule"
)

// migrateScreens maps screens from older versions onto current ones, so a
// saved schedule keeps working: the per-metric trend charts (line/<metric>)
// became the one trends screen. Duplicates that creates are dropped.
func migrateScreens(screens []string) []string {
	var out []string
	for _, s := range screens {
		if strings.HasPrefix(s, legacyLinePrefix) {
			s = scrTrends
		}
		if s == "wolfemoon" { // WolfePhase's first name
			s = scrMoon
		}
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	return out
}

// parseHM reads "HH:MM" as minutes after midnight.
func parseHM(s string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("%q is not a time like 07:30", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// normalize checks a schedule and returns it with dayparts sorted by start.
func (sc Schedule) normalize() (Schedule, error) {
	if sc.HoldMinutes < minHold || sc.HoldMinutes > maxHold {
		return sc, fmt.Errorf("hold time must be %d to %d minutes", minHold, maxHold)
	}
	if len(sc.Dayparts) == 0 {
		return sc, errors.New("the schedule needs at least one daypart")
	}
	if sc.AsOf != "" {
		if _, err := time.Parse("2006-01-02", sc.AsOf); err != nil {
			return sc, errors.New("the display's date must be YYYY-MM-DD")
		}
	}
	seen := map[int]bool{}
	parts := slices.Clone(sc.Dayparts)
	for i := range parts {
		parts[i].Screens = migrateScreens(parts[i].Screens)
	}
	for i, p := range parts {
		m, err := parseHM(p.Start)
		if err != nil {
			return sc, err
		}
		if seen[m] {
			return sc, fmt.Errorf("two dayparts start at %s", p.Start)
		}
		seen[m] = true
		if len(p.Screens) == 0 || len(p.Screens) > maxScreensPerPart {
			return sc, fmt.Errorf("the daypart at %s needs 1 to %d screens", p.Start, maxScreensPerPart)
		}
		for _, name := range p.Screens {
			if !slices.ContainsFunc(scheduleScreens, func(o screenOption) bool { return o.Name == name }) {
				return sc, fmt.Errorf("unknown screen %q", name)
			}
		}
		parts[i].Start = fmt.Sprintf("%02d:%02d", m/60, m%60)
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Start < parts[j].Start })
	sc.Dayparts = parts
	return sc, nil
}

// slot is what the schedule shows at a moment.
type slot struct {
	Screen string
	Part   int       // index into Dayparts
	Until  time.Time // when the screen next changes
	// Screens is the daypart's rotation as it runs now (reviews included),
	// and Index the place of Screen in it.
	Screens []string
	Index   int
}

// advanced is the screen n places on from the slot's: further round the
// daypart's rotation, or, in a daypart of one screen, through every screen
// the schedule offers, so a display can be stepped through by hand.
func (sl slot) advanced(n int) string {
	if n <= 0 {
		return sl.Screen
	}
	if len(sl.Screens) > 1 {
		return sl.Screens[(sl.Index+n)%len(sl.Screens)]
	}
	start := slices.IndexFunc(scheduleScreens, func(o screenOption) bool { return o.Name == sl.Screen })
	return scheduleScreens[(max(start, 0)+n)%len(scheduleScreens)].Name
}

// at says what a normalized schedule shows at t (local time). Within the
// active daypart, the screen is the one whose hold window t falls in,
// counted from the daypart's start.
func (sc Schedule) at(t time.Time) slot {
	const day = 24 * 60
	now := t.Hour()*60 + t.Minute()
	starts := make([]int, len(sc.Dayparts))
	for i, p := range sc.Dayparts {
		starts[i], _ = parseHM(p.Start)
	}
	part := len(starts) - 1 // before the first start: still in the last (wrapped) part
	for i, s := range starts {
		if s <= now {
			part = i
		}
	}
	elapsed := (now - starts[part] + day) % day
	partLen := (starts[(part+1)%len(starts)] - starts[part] + day) % day
	if partLen == 0 {
		partLen = day // a single daypart runs all day
	}
	screens := sc.Dayparts[part].Screens
	if len(screens) > 1 {
		// reviews first in line: on the 1st last month, on a Sunday last week;
		// on a Sunday the 1st, the month then the week
		if t.Weekday() == time.Sunday && !sc.SkipWeekly && !slices.Contains(screens, scrWeekReview) {
			screens = append([]string{scrWeekReview}, screens...)
		}
		if t.Day() == 1 && !sc.SkipMonthly {
			var lead []string
			for _, m := range monthlyScreens {
				if !slices.Contains(screens, m) {
					lead = append(lead, m)
				}
			}
			screens = append(lead, screens...)
		}
	}
	hold := sc.HoldMinutes
	idx := (elapsed / hold) % len(screens)

	next := partLen - elapsed // minutes to the next daypart
	if len(screens) > 1 {
		next = min(next, hold-elapsed%hold)
	}
	until := t.Truncate(time.Minute).Add(time.Duration(next) * time.Minute)
	return slot{Screen: screens[idx], Part: part, Until: until, Screens: screens, Index: idx}
}

// schedule is the saved schedule, or the default if none is saved or the
// saved one no longer validates.
func (s *server) schedule(ctx context.Context) Schedule {
	raw, ok, err := s.db.Setting(ctx, scheduleKey)
	if err != nil || !ok {
		sc, _ := DefaultSchedule().normalize()
		return sc
	}
	var sc Schedule
	if err := json.Unmarshal([]byte(raw), &sc); err == nil {
		if sc, err = sc.normalize(); err == nil {
			return sc
		}
	}
	log.Printf("schedule: saved schedule is invalid; using the default")
	sc, _ = DefaultSchedule().normalize()
	return sc
}

// scheduleFromForm reads the index page's schedule form: rows of start_N
// and screen_N_0..3, plus hold. Rows with no start or no screens are
// dropped, so clearing a row removes it and the blank rows add new ones.
func scheduleFromForm(form url.Values) (Schedule, error) {
	hold, err := strconv.Atoi(form.Get("hold"))
	if err != nil {
		return Schedule{}, errors.New("hold time must be a number of minutes")
	}
	sc := Schedule{HoldMinutes: hold, SkipMonthly: form.Get("monthly") != "1", SkipWeekly: form.Get("weekly") != "1",
		AsOf: strings.TrimSpace(form.Get("as_of"))}
	for i := 0; form.Has(fmt.Sprintf("start_%d", i)); i++ {
		start := strings.TrimSpace(form.Get(fmt.Sprintf("start_%d", i)))
		var screens []string
		for j := 0; j < maxScreensPerPart; j++ {
			if v := form.Get(fmt.Sprintf("screen_%d_%d", i, j)); v != "" {
				screens = append(screens, v)
			}
		}
		if start == "" || len(screens) == 0 {
			continue
		}
		sc.Dayparts = append(sc.Dayparts, Daypart{Start: start, Screens: screens})
	}
	return sc.normalize()
}

// saveSchedule handles the index page's schedule form.
func (s *server) saveSchedule(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	back := func(q string) { http.Redirect(w, r, "/?"+q+"#schedule", http.StatusSeeOther) }
	if r.PostForm.Get("reset") == "1" {
		if err := s.db.DeleteSetting(r.Context(), scheduleKey); err != nil {
			serverError(w, err)
			return
		}
		back("schedule=reset")
		return
	}
	sc, err := scheduleFromForm(r.PostForm)
	if err == nil && sc.AsOf != "" {
		if d, _ := time.Parse("2006-01-02", sc.AsOf); d.After(s.today()) {
			err = errors.New("the display's date can't be in the future")
		}
	}
	if err != nil {
		back("schedule_error=" + url.QueryEscape(err.Error()))
		return
	}
	raw, _ := json.Marshal(sc)
	if err := s.db.SetSetting(r.Context(), scheduleKey, string(raw)); err != nil {
		serverError(w, err)
		return
	}
	back("schedule=saved")
}

// scheduledImage renders what the schedule shows now at w x h. A screen
// that can't be drawn — last night when no sleep is stored — falls back to
// the daily brief rather than leaving a display blank.
func (s *server) scheduledImage(ctx context.Context, w, h int) (*image.Gray, string, slot, error) {
	return s.scheduledImageAdvanced(ctx, w, h, 0)
}

// scheduledImageAdvanced is scheduledImage stepped advance screens ahead
// (see slot.advanced), for the emulator's Next screen button; displays
// always take the schedule's own screen.
func (s *server) scheduledImageAdvanced(ctx context.Context, w, h, advance int) (*image.Gray, string, slot, error) {
	sl := s.schedule(ctx).at(s.now())
	img, screen, err := s.displayImage(ctx, sl.advanced(advance), "", w, h)
	return img, screen, sl, err
}

// displayImage draws screen as a display gets it: as of date (YYYY-MM-DD)
// if one is given, as for a day tapped on a week, else on the schedule's
// held date if it has one; falling back to the daily brief if the screen
// can't be drawn. It returns the screen actually drawn.
func (s *server) displayImage(ctx context.Context, screen, date string, w, h int) (*image.Gray, string, error) {
	var q url.Values
	if sc := s.schedule(ctx); sc.AsOf != "" { // held on a date of interest
		q = url.Values{"date": {sc.AsOf}}
	}
	if date != "" {
		q = url.Values{"date": {date}}
	}
	img, _, err := s.render(ctx, screen, q, w, h)
	if err != nil && screen != scrDaily {
		log.Printf("schedule: %s unavailable (%v); showing the daily brief", screen, err)
		screen = scrDaily
		img, _, err = s.render(ctx, screen, q, w, h)
	}
	return img, screen, err
}

// current serves whatever the schedule shows now, for any display;
// ?advance=N steps N screens ahead, as the emulator's Next screen does, and
// ?show=<screen> draws that screen instead, as the emulator does for a
// detail view tapped open, as of ?date= when that's given too.
func (s *server) current(w http.ResponseWriter, r *http.Request) {
	width, height, err := size(r)
	if err != nil {
		writeErr(w, badRequest(err))
		return
	}
	advance, err := qInt(r.URL.Query(), "advance", 0, 0, 1000)
	if err != nil {
		writeErr(w, err)
		return
	}
	var img *image.Gray
	var screen string
	ctx, zones := withZones(r.Context())
	if show := r.URL.Query().Get("show"); show != "" {
		if !knownScreen(show) {
			writeErr(w, badRequest(fmt.Errorf("show: no screen %q", show)))
			return
		}
		img, screen, err = s.displayImage(ctx, show, r.URL.Query().Get("date"), width, height)
	} else {
		img, screen, _, err = s.scheduledImageAdvanced(ctx, width, height, advance)
	}
	if err != nil {
		writeErr(w, err)
		return
	}
	// the parts a tap opens directly, for the emulator
	w.Header().Set("X-Zones", zonesHeader(screen, *zones, img.Bounds()))
	writeImage(w, r, img, "current-"+strings.ReplaceAll(screen, "/", "-"))
}
