package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// A shortcut gets the menu without a token, logs a habit by name or
// synonym with the token, as JSON or a form, hears a sentence back, and
// can undo; without the token nothing is stored. The index page shows the
// token and the logs, and removes one.
func TestHabitLog(t *testing.T) {
	s, h := trmnlServer(t, TRMNLConfig{}, 14, false)
	ctx := context.Background()
	do := func(method, target, body, ct string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, target, strings.NewReader(body))
		if ct != "" {
			req.Header.Set("Content-Type", ct)
		}
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	menu := do("GET", "/log/habits", "", "", nil)
	if menu.Code != 200 || menu.Body.String() != "Caffeine\nAlcohol\nMeal\nSweets\nWorkout\nScreens in bed\nCannabis\nLate night out\nMelatonin\nCold plunge\nStressful day\nSick\nTravel\n" {
		t.Fatalf("menu: %d %q", menu.Code, menu.Body)
	}
	tok, err := s.habitToken(ctx)
	if err != nil || len(tok) != 32 {
		t.Fatalf("token %q, %v", tok, err)
	}
	if again, _ := s.habitToken(ctx); again != tok {
		t.Error("the token changed")
	}

	if rec := do("POST", "/log", `{"habit": "caffeine"}`, "application/json", nil); rec.Code != 401 {
		t.Errorf("no token: %d", rec.Code)
	}
	if rec := do("POST", "/log", `{"habit": "caffeine"}`, "application/json", map[string]string{"X-Log-Token": "wrong"}); rec.Code != 401 {
		t.Errorf("wrong token: %d", rec.Code)
	}
	if rec := do("POST", "/log", `{"habit": "caffeine", "token": " `+tok+`\n"}`, "application/json", nil); rec.Code != 200 {
		t.Errorf("a pasted token with space around it: %d", rec.Code)
	}
	s.db.DeleteHabitLog(ctx, 1)
	auth := map[string]string{"X-Log-Token": tok}
	rec := do("POST", "/log", `{"habit": "Caffeine"}`, "application/json", auth)
	if rec.Code != 200 || rec.Body.String() != "Logged 1 double shot of espresso at 2:00 PM, about 130 mg of caffeine.\n" {
		t.Errorf("caffeine: %d %q", rec.Code, rec.Body)
	}
	rec = do("POST", "/log", `{"habit": "caffeine"}`, "application/json", auth)
	if !strings.Contains(rec.Body.String(), "That's 2 today.") {
		t.Errorf("second shot: %q", rec.Body)
	}
	form := url.Values{"habit": {"Alcohol"}, "count": {"2"}, "token": {tok}}
	rec = do("POST", "/log", form.Encode(), "application/x-www-form-urlencoded", nil)
	if rec.Code != 200 || !strings.HasPrefix(rec.Body.String(), "Logged 2 drinks at 2:00 PM.") {
		t.Errorf("form, two drinks: %d %q", rec.Code, rec.Body)
	}
	rec = do("POST", "/log", `{"habit": "Travel", "count": 3}`, "application/json", auth)
	if rec.Body.String() != "Logged travel at 2:00 PM.\n" {
		t.Errorf("travel, which isn't counted: %q", rec.Body)
	}
	do("POST", "/log/undo", "", "", auth)
	rec = do("POST", "/log", `{"habit": "Meal"}`, "application/json", auth) // 2 PM: hours before a usual bedtime
	if rec.Body.String() != "Logged meal at 2:00 PM.\n" {
		t.Errorf("an afternoon meal: %q", rec.Body)
	}
	do("POST", "/log/undo", "", "", auth)
	late := s.now
	s.now = func() time.Time { return time.Date(2026, 11, 19, 21, 0, 0, 0, time.Local) }
	rec = do("POST", "/log", `{"habit": " meal "}`, "application/json", auth)
	if !strings.HasPrefix(rec.Body.String(), "Logged meal at 9:00 PM, within 3 hours of your usual ") {
		t.Errorf("a 9 PM meal: %q", rec.Body)
	}
	do("POST", "/log/undo", "", "", auth)
	s.now = late
	for _, bad := range []string{`{"habit": "kale"}`, `{"habit": "caffeine", "count": 50}`, `not json`} {
		if rec := do("POST", "/log", bad, "application/json", auth); rec.Code != 400 || !strings.HasPrefix(rec.Body.String(), "Not logged") {
			t.Errorf("%s: %d %q", bad, rec.Code, rec.Body)
		}
	}

	rec = do("POST", "/log/undo", "", "", auth)
	if rec.Body.String() != "Removed 2 drinks, logged at 2:00 PM.\n" {
		t.Errorf("undo: %q", rec.Body)
	}
	now := s.now()
	logs, _ := s.db.HabitLogsBetween(ctx, now.Add(-time.Hour), now.Add(time.Hour), time.Local)
	if len(logs) != 2 || logs[0].Habit != "Caffeine" || logs[0].Source != "shortcut" {
		t.Fatalf("stored: %+v", logs)
	}

	page := do("GET", "/habits", "", "", nil)
	if body := page.Body.String(); !strings.Contains(body, tok) || !strings.Contains(body, "Caffeine · 1 double shot of espresso") {
		t.Error("the index page doesn't show the token and the logs")
	}
	rec = do("POST", "/log/delete", url.Values{"id": {fmt.Sprint(logs[0].ID)}}.Encode(), "application/x-www-form-urlencoded", nil)
	if rec.Code != 303 {
		t.Errorf("delete: %d", rec.Code)
	}
	if logs, _ := s.db.HabitLogsBetween(ctx, now.Add(-time.Hour), now.Add(time.Hour), time.Local); len(logs) != 1 {
		t.Errorf("after delete: %d logs", len(logs))
	}
}

// The two habit charts render, empty or not.
func TestHabitCharts(t *testing.T) {
	h := testServer(t)
	for _, u := range []string{"/habits-week.png", "/habits-week.png?week=2026-11-10", "/habit-effects.png", "/habit-effects.png?days=30"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", u, nil))
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
			t.Errorf("%s: %d", u, rec.Code)
		}
	}
	if screenTitle(scrHabitWeek) != "Habits · Week" || screenTitle(scrHabitEffects) != "Habit effects" {
		t.Error("the habit charts aren't in the schedule menu")
	}
}

// A log can be dated after the fact: a time of day is today, or yesterday
// when it would be later than now; a full date and time is taken as is; the
// future and anything over a week old are refused.
func TestParseLogTime(t *testing.T) {
	loc := time.FixedZone("PST", -8*3600)
	now := time.Date(2026, 11, 21, 11, 0, 0, 0, loc)
	for in, want := range map[string]time.Time{
		"":                          now,
		"8:30 AM":                   time.Date(2026, 11, 21, 8, 30, 0, 0, loc),
		"8:30am":                    time.Date(2026, 11, 21, 8, 30, 0, 0, loc),
		"7 AM":                      time.Date(2026, 11, 21, 7, 0, 0, 0, loc),
		"08:15":                     time.Date(2026, 11, 21, 8, 15, 0, 0, loc),
		"10:30 PM":                  time.Date(2026, 11, 20, 22, 30, 0, 0, loc), // later than now: last night
		"2026-11-20T21:00":          time.Date(2026, 11, 20, 21, 0, 0, 0, loc),
		"2026-11-21T08:30:00-08:00": time.Date(2026, 11, 21, 8, 30, 0, 0, loc),
		"2026-11-21T16:30:00Z":      time.Date(2026, 11, 21, 8, 30, 0, 0, loc),
	} {
		got, err := parseLogTime(in, now)
		if err != nil || !got.Equal(want) {
			t.Errorf("%q = %s, %v; want %s", in, got.Format(time.RFC3339), err, want.Format(time.RFC3339))
		}
	}
	for _, bad := range []string{"soon", "2026-11-21T13:00", "2026-11-10T08:00", "25:00"} {
		if _, err := parseLogTime(bad, now); err == nil {
			t.Errorf("%q should be refused", bad)
		}
	}
}

// The shortcut can backdate a log, and the index page can move one's time
// and change its count.
func TestBackdateAndEdit(t *testing.T) {
	s, h := trmnlServer(t, TRMNLConfig{}, 11, false) // Nov 19, 11 AM
	ctx := context.Background()
	tok, _ := s.habitToken(ctx)
	post := func(target, body, ct string, hdr map[string]string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("POST", target, strings.NewReader(body))
		req.Header.Set("Content-Type", ct)
		for k, v := range hdr {
			req.Header.Set(k, v)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	auth := map[string]string{"X-Log-Token": tok}
	rec := post("/log", `{"habit": "Caffeine", "at": "8:30 AM"}`, "application/json", auth)
	if rec.Body.String() != "Logged 1 double shot of espresso at 8:30 AM, about 130 mg of caffeine.\n" {
		t.Errorf("backdated: %q", rec.Body)
	}
	rec = post("/log", `{"habit": "Alcohol", "at": "2026-11-18T20:00"}`, "application/json", auth)
	if !strings.HasPrefix(rec.Body.String(), "Logged 1 drink at 8:00 PM Wednesday") {
		t.Errorf("another day: %q", rec.Body)
	}
	if rec := post("/log", `{"habit": "Caffeine", "at": "3 PM"}`, "application/json", auth); rec.Code != 200 {
		t.Errorf("3 PM said at 11 AM is yesterday's: %d %q", rec.Code, rec.Body)
	}
	if rec := post("/log", `{"habit": "Caffeine", "at": "2026-11-01T08:00"}`, "application/json", auth); rec.Code != 400 {
		t.Errorf("weeks ago: %d", rec.Code)
	}

	now := s.now()
	logs, _ := s.db.HabitLogsBetween(ctx, now.AddDate(0, 0, -3), now.Add(time.Hour), time.Local)
	if len(logs) != 3 {
		t.Fatalf("logs: %+v", logs)
	}
	alc := logs[0] // Wednesday's drink, the earliest
	form := url.Values{"id": {fmt.Sprint(alc.ID)}, "at": {"2026-11-18T21:15"}, "count": {"2"}}
	if rec := post("/log/edit", form.Encode(), "application/x-www-form-urlencoded", nil); rec.Code != 303 {
		t.Fatalf("edit: %d %q", rec.Code, rec.Body)
	}
	got, _, _ := s.db.HabitLogByID(ctx, alc.ID, time.Local)
	if got.At.Format("Jan 2 15:04") != "Nov 18 21:15" || got.Count != 2 {
		t.Errorf("after edit: %s ×%v", got.At.Format("Jan 2 15:04"), got.Count)
	}
	form.Set("at", "2026-11-25T08:00")
	if rec := post("/log/edit", form.Encode(), "application/x-www-form-urlencoded", nil); rec.Code != 400 {
		t.Errorf("moving a log into the future: %d", rec.Code)
	}
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest("GET", "/habits", nil))
	if !strings.Contains(page.Body.String(), `value="2026-11-18T21:15"`) {
		t.Error("the index page has no time picker with the log's time")
	}
}

// The index page's download is every log as CSV, to save.
func TestExportHabits(t *testing.T) {
	s, h := trmnlServer(t, TRMNLConfig{}, 14, false)
	ctx := context.Background()
	s.db.AddHabitLog(ctx, store.HabitLog{At: s.now().Add(-30 * 24 * time.Hour), Habit: "Alcohol", Count: 2, Source: "shortcut"})
	s.db.AddHabitLog(ctx, store.HabitLog{At: s.now(), Habit: "Caffeine", Count: 1, Source: "shortcut"})
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/log/export.csv", nil))
	body := rec.Body.String()
	if rec.Code != 200 || !strings.Contains(rec.Header().Get("Content-Disposition"), `filename="habit-log-2026-11-19.csv"`) ||
		strings.Count(body, "\n") != 3 || !strings.Contains(body, ",Alcohol,2,") {
		t.Errorf("export: %d %v %q", rec.Code, rec.Header(), body)
	}
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest("GET", "/habits", nil))
	if !strings.Contains(page.Body.String(), `href="/log/export.csv"`) {
		t.Error("the index page has no download link")
	}
}

// The index page lists this week's logs, Sunday to Saturday: Thursday
// Nov 19's page has Sunday's first log but not Saturday's last.
func TestRecentHabitsThisWeek(t *testing.T) {
	s, h := trmnlServer(t, TRMNLConfig{}, 14, false)
	ctx := context.Background()
	loc := s.now().Location()
	s.db.AddHabitLog(ctx, store.HabitLog{At: time.Date(2026, 11, 14, 23, 30, 0, 0, loc), Habit: "Alcohol", Count: 1, Source: "shortcut"})
	s.db.AddHabitLog(ctx, store.HabitLog{At: time.Date(2026, 11, 15, 0, 30, 0, 0, loc), Habit: "Caffeine", Count: 1, Source: "shortcut"})
	page := httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest("GET", "/habits", nil))
	body := page.Body.String()
	if !strings.Contains(body, `value="2026-11-15T00:30"`) {
		t.Error("Sunday's log is missing")
	}
	if strings.Contains(body, `value="2026-11-14T23:30"`) {
		t.Error("last Saturday's log is still listed")
	}

	// each day's logs sit under one header, newest day first
	s.db.AddHabitLog(ctx, store.HabitLog{At: time.Date(2026, 11, 17, 9, 0, 0, 0, loc), Habit: "Caffeine", Count: 1, Source: "shortcut"})
	s.db.AddHabitLog(ctx, store.HabitLog{At: time.Date(2026, 11, 17, 21, 0, 0, 0, loc), Habit: "Alcohol", Count: 1, Source: "shortcut"})
	page = httptest.NewRecorder()
	h.ServeHTTP(page, httptest.NewRequest("GET", "/habits", nil))
	body = page.Body.String()
	tue, sun := strings.Index(body, ">Tuesday, Nov 17</th>"), strings.Index(body, ">Sunday, Nov 15</th>")
	if tue < 0 || sun < tue || strings.Count(body, ">Tuesday, Nov 17</th>") != 1 {
		t.Errorf("day headers: Tuesday at %d, Sunday at %d", tue, sun)
	}
	if late, early := strings.Index(body, `value="2026-11-17T21:00"`), strings.Index(body, `value="2026-11-17T09:00"`); !(tue < late && late < early && early < sun) {
		t.Error("Tuesday's logs aren't under its header, newest first")
	}
}

// The spoken summary is plain text, never cached, and takes ?date=.
func TestSummaryRoute(t *testing.T) {
	h := testServer(t)
	for _, u := range []string{"/summary.txt", "/summary.txt?date=2026-11-18"} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", u, nil))
		if rec.Code != 200 || rec.Header().Get("Cache-Control") != "no-store" || !strings.Contains(rec.Body.String(), "Sleep score") {
			t.Errorf("%s: %d %q", u, rec.Code, rec.Body)
		}
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/summary.txt?date=nope", nil))
	if rec.Code != 400 {
		t.Errorf("bad date: %d", rec.Code)
	}
}
