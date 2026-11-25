package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"testing"
	"time"
)

func at(h, m int) time.Time { return time.Date(2026, 11, 19, h, m, 0, 0, time.Local) }

// fixedSchedule is a schedule fixed for the rotation tests, so they test
// the rotation, not whatever the default happens to be.
func fixedSchedule() Schedule {
	return Schedule{
		HoldMinutes: 60,
		Dayparts: []Daypart{
			{Start: "05:00", Screens: []string{scrNight}},
			{Start: "11:00", Screens: []string{scrDaily, scrSleepStages, scrStressMonth}},
			{Start: "18:00", Screens: []string{scrDaily, scrWeeklyRhythm, scrStressMonth}},
			{Start: "22:00", Screens: []string{scrDaily}},
		},
	}
}

// The default is valid, every screen in it real, none used twice but the
// daily brief (and overnight's one screen), every daypart long enough to
// show all its screens, and overnight one screen so the panel never
// flashes in the dark. The morning leads with the brief, and the month's
// calendars are left to the 1st.
func TestDefaultSchedule(t *testing.T) {
	sc, err := DefaultSchedule().normalize()
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for i, p := range sc.Dayparts {
		if i == len(sc.Dayparts)-1 {
			break // overnight repeats the evening's last word on purpose
		}
		start, _ := parseHM(p.Start)
		end, _ := parseHM(sc.Dayparts[(i+1)%len(sc.Dayparts)].Start)
		if long := (end - start + 24*60) % (24 * 60); long < len(p.Screens)*sc.HoldMinutes {
			t.Errorf("%s: %d screens don't fit in %d minutes", p.Start, len(p.Screens), long)
		}
		for _, name := range p.Screens {
			if seen[name] && name != scrDaily {
				t.Errorf("%s shown in two dayparts", name)
			}
			if slices.Contains([]string{scrScoreMonth, scrStressMonth, scrPhaseMonth, scrMonthReport}, name) {
				t.Errorf("%s is a monthly calendar: it belongs to the 1st", name)
			}
			seen[name] = true
		}
	}
	last := sc.Dayparts[len(sc.Dayparts)-1]
	if len(last.Screens) != 1 {
		t.Errorf("overnight rotates: %v", last.Screens)
	}
	for _, c := range []struct {
		h, m   int
		screen string
	}{{5, 0, scrDaily}, {5, 15, scrMoon}, {6, 30, scrDaily}, {8, 0, scrThisWeek}, {14, 0, scrSleepSchedule}, {23, 0, scrPayoff}, {4, 59, scrPayoff}} {
		if sl := sc.at(at(c.h, c.m)); sl.Screen != c.screen {
			t.Errorf("%02d:%02d: %s, want %s", c.h, c.m, sl.Screen, c.screen)
		}
	}
}

// Stepping ahead goes round the daypart's rotation, and in a daypart of
// one screen through every screen the schedule offers.
func TestSlotAdvanced(t *testing.T) {
	sc, _ := fixedSchedule().normalize()
	sl := sc.at(at(11, 0)) // daily, sleep stages, stress calendar
	for n, want := range []string{scrDaily, scrSleepStages, scrStressMonth, scrDaily} {
		if got := sl.advanced(n); got != want {
			t.Errorf("11:00 +%d: %s, want %s", n, got, want)
		}
	}
	one := sc.at(at(7, 0)) // last night alone
	i := slices.IndexFunc(scheduleScreens, func(o screenOption) bool { return o.Name == scrNight })
	if got := one.advanced(1); got != scheduleScreens[i+1].Name {
		t.Errorf("single-screen daypart +1: %s, want %s", got, scheduleScreens[i+1].Name)
	}
	if one.advanced(0) != scrNight {
		t.Error("no advance should be the schedule's screen")
	}
}

func TestScheduleAt(t *testing.T) {
	sc, err := fixedSchedule().normalize()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		t      time.Time
		screen string
		until  time.Time
	}{
		{at(7, 0), scrNight, at(11, 0)},                  // single-screen daypart: holds until the next
		{at(11, 0), scrDaily, at(12, 0)},                 // rotation starts at the daypart's start
		{at(12, 30), scrSleepStages, at(13, 0)},          // second hour
		{at(13, 59), scrStressMonth, at(14, 0)},          // third hour
		{at(14, 0), scrDaily, at(15, 0)},                 // wraps round the list
		{at(17, 30), scrDaily, at(18, 0)},                // 7th hour wraps to daily; held only until the next daypart
		{at(23, 0), scrDaily, at(5, 0).AddDate(0, 0, 1)}, // overnight: holds until 5 AM tomorrow
		{at(2, 0), scrDaily, at(5, 0)},                   // after midnight: still the 22:00 daypart
	}
	for _, c := range cases {
		sl := sc.at(c.t)
		if sl.Screen != c.screen || !sl.Until.Equal(c.until) {
			t.Errorf("%s: %s until %s, want %s until %s", c.t.Format("15:04"),
				sl.Screen, sl.Until.Format("Jan 2 15:04"), c.screen, c.until.Format("Jan 2 15:04"))
		}
	}
}

func TestSingleDaypartRunsAllDay(t *testing.T) {
	sc, err := Schedule{HoldMinutes: 30, Dayparts: []Daypart{{Start: "06:00", Screens: []string{scrDaily, scrNight}}}}.normalize()
	if err != nil {
		t.Fatal(err)
	}
	// 21h10m since 06:00 is 42 half-hour slots: even, so the first screen
	if sl := sc.at(at(3, 10)); sl.Screen != scrDaily || !sl.Until.Equal(at(3, 30)) {
		t.Errorf("03:10: %+v", sl)
	}
}

func TestScheduleValidation(t *testing.T) {
	bad := []Schedule{
		{HoldMinutes: 0, Dayparts: []Daypart{{Start: "05:00", Screens: []string{scrDaily}}}},
		{HoldMinutes: 721, Dayparts: []Daypart{{Start: "05:00", Screens: []string{scrDaily}}}},
		{HoldMinutes: 60},
		{HoldMinutes: 60, Dayparts: []Daypart{{Start: "25:00", Screens: []string{scrDaily}}}},
		{HoldMinutes: 60, Dayparts: []Daypart{{Start: "05:00", Screens: []string{"nope"}}}},
		{HoldMinutes: 60, Dayparts: []Daypart{{Start: "05:00", Screens: []string{scrDaily}}, {Start: "5:00", Screens: []string{scrNight}}}},
		{HoldMinutes: 60, Dayparts: []Daypart{{Start: "05:00"}}},
	}
	for i, sc := range bad {
		if _, err := sc.normalize(); err == nil {
			t.Errorf("case %d should be rejected: %+v", i, sc)
		}
	}
	// normalize sorts and pads times
	sc, err := Schedule{HoldMinutes: 60, Dayparts: []Daypart{
		{Start: "18:00", Screens: []string{scrDaily}}, {Start: "7:05", Screens: []string{scrNight}}}}.normalize()
	if err != nil || sc.Dayparts[0].Start != "07:05" {
		t.Errorf("normalize: %+v %v", sc, err)
	}
}

// The form drops empty rows and keeps screen order.
func TestScheduleFromForm(t *testing.T) {
	f := url.Values{
		"hold":    {"45"},
		"start_0": {"06:00"}, "screen_0_0": {scrNight},
		"start_1": {"12:00"}, "screen_1_0": {scrStressMonth}, "screen_1_1": {""}, "screen_1_2": {scrDaily},
		"start_2": {""}, "screen_2_0": {scrDaily}, // no time: dropped
		"start_3": {"20:00"}, // no screens: dropped
	}
	sc, err := scheduleFromForm(f)
	if err != nil {
		t.Fatal(err)
	}
	if sc.HoldMinutes != 45 || len(sc.Dayparts) != 2 || strings.Join(sc.Dayparts[1].Screens, ",") != scrStressMonth+","+scrDaily {
		t.Errorf("parsed %+v", sc)
	}
}

// Saving through the page stores the schedule and the display follows it;
// a bad form is reported, not saved; reset returns to the default.
func TestSaveScheduleRoundTrip(t *testing.T) {
	s, h := trmnlServer(t, TRMNLConfig{}, 14, true)
	post := func(f url.Values) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", "/schedule", strings.NewReader(f.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	rec := post(url.Values{"hold": {"60"}, "start_0": {"00:00"}, "screen_0_0": {scrWeeklyRhythm}})
	if rec.Code != http.StatusSeeOther || !strings.Contains(rec.Header().Get("Location"), "schedule=saved") {
		t.Fatalf("save: %d %v", rec.Code, rec.Header())
	}
	if got := s.schedule(context.Background()).at(s.now()).Screen; got != scrWeeklyRhythm {
		t.Errorf("after save the schedule shows %s", got)
	}
	d := call(t, h, "GET", "/api/display", map[string]string{"ID": mac, "Access-Token": "k"}, "")
	if !strings.HasPrefix(d["filename"].(string), "weekly-r") {
		t.Errorf("display showed %v, want the weekly rhythm", d["filename"])
	}
	if rec := post(url.Values{"hold": {"60"}, "start_0": {"99:00"}, "screen_0_0": {scrDaily}}); !strings.Contains(rec.Header().Get("Location"), "schedule_error=") {
		t.Errorf("bad form not reported: %v", rec.Header())
	}
	if got := s.schedule(context.Background()).at(s.now()).Screen; got != scrWeeklyRhythm {
		t.Errorf("a rejected form changed the schedule to %s", got)
	}
	post(url.Values{"reset": {"1"}})
	if got := s.schedule(context.Background()).at(s.now()).Screen; got != scrSleepSchedule {
		t.Errorf("after reset: %s, want the default's sleep schedule at 14:00", got)
	}
}

// The device sleeps its usual interval, cut short to wake just after a
// scheduled change, never under a minute.
func TestRefreshFor(t *testing.T) {
	if got := refreshFor(15*time.Minute, time.Hour); got != 900 {
		t.Errorf("far from a change: %d", got)
	}
	if got := refreshFor(15*time.Minute, 5*time.Minute); got != 330 {
		t.Errorf("change in 5m: %d, want 330", got)
	}
	if got := refreshFor(15*time.Minute, 0); got != 60 {
		t.Errorf("change now: %d, want the 60s floor", got)
	}
}

func TestCurrentFallsBackToDaily(t *testing.T) {
	s, h := trmnlServer(t, TRMNLConfig{}, 5, false) // WolfePhase's turn, but no night stored
	s.now = func() time.Time { return time.Date(2026, 11, 19, 5, 15, 0, 0, time.Local) }
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/current.png?download=1", nil))
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Disposition"), "current-daily") {
		t.Errorf("current: %d %v", rec.Code, rec.Header().Get("Content-Disposition"))
	}
}

func dec1(h, m int) time.Time { return time.Date(2026, 12, 1, h, m, 0, 0, time.Local) }

// On the 1st, last month's report and calendars lead each rotating
// daypart; single-screen dayparts and other days are untouched.
func TestMonthlyReportOnTheFirst(t *testing.T) {
	sc, _ := fixedSchedule().normalize()
	cases := []struct {
		t      time.Time
		screen string
		until  time.Time
	}{
		{dec1(7, 0), scrNight, dec1(11, 0)},       // the morning keeps last night
		{dec1(11, 0), scrLastMonth, dec1(12, 0)},  // first in the rotation
		{dec1(12, 0), scrLastScores, dec1(13, 0)}, // then the calendars
		{dec1(13, 0), scrLastStress, dec1(14, 0)},
		{dec1(14, 30), scrLastPhase, dec1(15, 0)},
		{dec1(15, 0), scrDaily, dec1(16, 0)}, // then the usual screens
		{dec1(17, 0), scrStressMonth, dec1(18, 0)},
		{dec1(18, 0), scrLastMonth, dec1(19, 0)},                                // the evening daypart too
		{dec1(23, 0), scrDaily, time.Date(2026, 12, 2, 5, 0, 0, 0, time.Local)}, // overnight never changes
		{at(11, 0), scrDaily, at(12, 0)},                                        // Nov 19: an ordinary day
	}
	for _, c := range cases {
		sl := sc.at(c.t)
		if sl.Screen != c.screen || !sl.Until.Equal(c.until) {
			t.Errorf("%s: %s until %s, want %s until %s", c.t.Format("Jan 2 15:04"),
				sl.Screen, sl.Until.Format("Jan 2 15:04"), c.screen, c.until.Format("Jan 2 15:04"))
		}
	}
	sc.SkipMonthly = true
	if sl := sc.at(dec1(11, 0)); sl.Screen != scrDaily {
		t.Errorf("with the report turned off: %s", sl.Screen)
	}
}

func TestMonthlyCheckbox(t *testing.T) {
	base := url.Values{"hold": {"60"}, "start_0": {"06:00"}, "screen_0_0": {scrDaily}}
	off, _ := scheduleFromForm(base)
	base.Set("monthly", "1")
	on, _ := scheduleFromForm(base)
	if !off.SkipMonthly || on.SkipMonthly {
		t.Errorf("checkbox: unticked skip=%v, ticked skip=%v", off.SkipMonthly, on.SkipMonthly)
	}
}

// On Dec 1 a display gets the November report, and "last month" renders
// the month just ended.
func TestLastMonthOnDisplay(t *testing.T) {
	s, h := trmnlServer(t, TRMNLConfig{}, 11, true)
	s.now = func() time.Time { return dec1(8, 5) }
	d := call(t, h, "GET", "/api/display", map[string]string{"ID": mac, "Access-Token": "k"}, "")
	if name, _ := d["filename"].(string); !strings.HasPrefix(name, "month-re") || len(name) > 31 {
		t.Errorf("Dec 1, 8:05: showed %v, want last month's report", d["filename"])
	}
	_, name, err := s.render(context.Background(), scrLastMonth, nil, 800, 480)
	if err != nil || name != "month-2026-11" {
		t.Errorf("last month rendered %q, %v; want month-2026-11", name, err)
	}
	for screen, want := range map[string]string{scrLastScores: "scores-2026-11", scrLastStress: "stress-2026-11", scrLastPhase: "wolfephase-2026-11"} {
		if _, name, err := s.render(context.Background(), screen, nil, 800, 480); err != nil || name != want {
			t.Errorf("%s rendered %q, %v; want %s", screen, name, err, want)
		}
	}
}

// A schedule saved with the retired trend charts still loads: each becomes
// the trends screen, without duplicates.
func TestScheduleMigratesLineCharts(t *testing.T) {
	sc, err := Schedule{HoldMinutes: 60, Dayparts: []Daypart{
		{Start: "08:00", Screens: []string{"line/sleep", scrDaily, "line/steps"}},
	}}.normalize()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sc.Dayparts[0].Screens, ","); got != scrTrends+","+scrDaily {
		t.Errorf("migrated screens = %s", got)
	}
}

// A schedule saved with WolfePhase's first name still loads, as WolfePhase.
func TestScheduleMigratesWolfeMoon(t *testing.T) {
	sc, err := Schedule{HoldMinutes: 60, Dayparts: []Daypart{
		{Start: "08:00", Screens: []string{"wolfemoon", scrDaily}},
	}}.normalize()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(sc.Dayparts[0].Screens, ","); got != scrMoon+","+scrDaily {
		t.Errorf("migrated screens = %s", got)
	}
}

// On Sundays last week's review leads each rotating daypart; on a Sunday
// that is also the 1st, the month's report and calendars come first, then
// the week's.
func TestWeeklyReviewOnSundays(t *testing.T) {
	sc, _ := fixedSchedule().normalize()
	mon := func(y int, m time.Month, d, h int) time.Time { return time.Date(y, m, d, h, 0, 0, 0, time.Local) }
	if sl := sc.at(mon(2026, 11, 22, 11)); sl.Screen != scrWeekReview {
		t.Errorf("Sunday 11:00: %s, want last week's review", sl.Screen)
	}
	if sl := sc.at(mon(2026, 11, 22, 7)); sl.Screen != scrNight {
		t.Errorf("Sunday morning: %s, want last night (single-screen daypart)", sl.Screen)
	}
	if sl := sc.at(mon(2026, 11, 23, 11)); sl.Screen != scrDaily {
		t.Errorf("Monday 11:00: %s", sl.Screen)
	}
	// Nov 1, 2026 is a Sunday and the 1st
	if a, b := sc.at(mon(2026, 11, 1, 11)), sc.at(mon(2026, 11, 1, 15)); a.Screen != scrLastMonth || b.Screen != scrWeekReview {
		t.Errorf("Sunday the 1st: %s then %s, want month then week", a.Screen, b.Screen)
	}
	sc.SkipWeekly = true
	if sl := sc.at(mon(2026, 11, 22, 11)); sl.Screen != scrDaily {
		t.Errorf("weekly off: %s", sl.Screen)
	}
	f := url.Values{"hold": {"60"}, "start_0": {"06:00"}, "screen_0_0": {scrDaily}}
	if off, _ := scheduleFromForm(f); !off.SkipWeekly {
		t.Error("unticked weekly box should skip")
	}
}

// The displays can be held on a date of interest: the form stores it (a
// future or malformed date is refused), the display draws as of it, and
// clearing it returns them to live.
func TestScheduleHeldDate(t *testing.T) {
	s, h := trmnlServer(t, TRMNLConfig{}, 14, true)
	ctx := context.Background()
	post := func(asOf string) *httptest.ResponseRecorder {
		f := url.Values{"hold": {"60"}, "start_0": {"00:00"}, "screen_0_0": {scrDaily}, "as_of": {asOf}}
		req := httptest.NewRequest("POST", "/schedule", strings.NewReader(f.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	held := s.today().AddDate(0, 0, -7).Format("2006-01-02")
	if rec := post(held); !strings.Contains(rec.Header().Get("Location"), "schedule=saved") {
		t.Fatalf("save held date: %v", rec.Header())
	}
	if got := s.schedule(ctx).AsOf; got != held {
		t.Fatalf("held date %q, want %q", got, held)
	}
	img, _, _, err := s.scheduledImage(ctx, 800, 480)
	if err != nil {
		t.Fatal(err)
	}
	want, _, _ := s.render(ctx, scrDaily, url.Values{"date": {held}}, 800, 480)
	live, _, _ := s.render(ctx, scrDaily, nil, 800, 480)
	if mastheadRows(img) != mastheadRows(want) || mastheadRows(img) == mastheadRows(live) {
		t.Error("the display should be drawn as of the held date")
	}
	for _, bad := range []string{s.today().AddDate(0, 0, 1).Format("2006-01-02"), "last week"} {
		if rec := post(bad); !strings.Contains(rec.Header().Get("Location"), "schedule_error=") {
			t.Errorf("date %q not refused: %v", bad, rec.Header())
		}
	}
	if got := s.schedule(ctx).AsOf; got != held {
		t.Errorf("a refused date changed the held date to %q", got)
	}
	post("")
	if got := s.schedule(ctx).AsOf; got != "" {
		t.Errorf("clearing left %q", got)
	}
}
