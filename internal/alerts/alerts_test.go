package alerts

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// morning is 9 AM on Thursday, Nov 19: last night is the 19th's.
var morning = time.Date(2026, 11, 19, 9, 0, 0, 0, time.Local)

func ptr[T any](v T) *T { return &v }

// testDB holds 40 ordinary nights ending last night, with temperature,
// HRV and lowest heart rate overridden by nights ago (0 = last night).
func testDB(t *testing.T, temps map[int]float64, hrv, hr map[int]int) *store.DB {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	var samples []store.Sample
	var periods []store.SleepPeriod
	for i := 0; i < 40; i++ {
		day := time.Date(2026, 11, 19-i, 0, 0, 0, 0, time.UTC).Format("2006-01-02")
		temp := []float64{-0.1, 0, 0.1}[i%3]
		if v, ok := temps[i]; ok {
			temp = v
		}
		samples = append(samples, store.Sample{Metric: store.MetricTempDev, Day: day, Value: temp})
		h, r := 50+i%3, 50+i%2
		if v, ok := hrv[i]; ok {
			h = v
		}
		if v, ok := hr[i]; ok {
			r = v
		}
		periods = append(periods, store.SleepPeriod{ID: "p" + day, Day: day, Type: "long_sleep",
			BedtimeStart: day + "T00:00:00-08:00", BedtimeEnd: day + "T07:00:00-08:00", TimeInBed: 25200,
			AverageHRV: ptr(h), LowestHR: ptr(r)})
	}
	ctx := context.Background()
	if err := db.Save(ctx, samples); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveSleepPeriods(ctx, periods); err != nil {
		t.Fatal(err)
	}
	return db
}

// sick is two nights of a warmer body, a faster heart and lower HRV.
func sick(t *testing.T) *store.DB {
	return testDB(t, map[int]float64{0: 0.6, 1: 0.5}, map[int]int{0: 30, 1: 32}, map[int]int{0: 60, 1: 58})
}

// fresh is a fetch that worked just now.
var fresh = FetchState{LastSuccess: morning.Add(-time.Hour)}

// Two nights with three signals up is an early warning, naming each
// signal's reading and what was logged before last night; ordinary
// nights are nothing.
func TestIllness(t *testing.T) {
	db := sick(t)
	ctx := context.Background()
	if _, err := db.AddHabitLog(ctx, store.HabitLog{At: time.Date(2026, 11, 18, 20, 0, 0, 0, time.Local), Habit: "Alcohol", Count: 1, Source: "shortcut"}); err != nil {
		t.Fatal(err)
	}
	got, err := Rules(ctx, db, fresh, morning, DefaultSettings())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("alerts %+v", got)
	}
	a := got[0]
	if a.Key != "illness:2026-11-19" || a.Title != "Early warning: 3 signals up" || a.Level != "time-sensitive" ||
		!strings.HasPrefix(a.Message, "Temperature +0.60 °C, lowest HR 60 bpm and HRV 30 ms beyond your normal, 2 nights running.") ||
		!strings.Contains(a.Message, "Logged before last night: alcohol.") || a.Path != "/emulator?show=early-warning" {
		t.Errorf("alert %+v", a)
	}

	if got, _ := Rules(ctx, testDB(t, nil, nil, nil), fresh, morning, DefaultSettings()); len(got) != 0 {
		t.Errorf("ordinary nights: %+v", got)
	}
	// one night alone isn't a run
	one := testDB(t, map[int]float64{0: 0.6}, map[int]int{0: 30}, map[int]int{0: 60})
	if got, _ := Rules(ctx, one, fresh, morning, DefaultSettings()); len(got) != 0 {
		t.Errorf("one night: %+v", got)
	}
}

// No data for 13 hours is an alert, worded for the login when that's the
// error; the ring not syncing is one when fetches work.
func TestStaleData(t *testing.T) {
	ctx := context.Background()
	db := testDB(t, nil, nil, nil)
	login := FetchState{LastSuccess: morning.Add(-20 * time.Hour),
		LastError: "refresh token (if this persists, run `wolfe-health auth` again): 400 invalid_grant"}
	got, _ := Rules(ctx, db, login, morning, DefaultSettings())
	if len(got) != 1 || got[0].Key != "fetch:2026-11-19" || !strings.Contains(got[0].Message, "make auth") {
		t.Errorf("login: %+v", got)
	}
	network := FetchState{LastSuccess: morning.Add(-14 * time.Hour), LastError: "dial tcp: no route to host"}
	if got, _ := Rules(ctx, db, network, morning, DefaultSettings()); len(got) != 1 || !strings.Contains(got[0].Message, "no route to host") {
		t.Errorf("network: %+v", got)
	}
	if got, _ := Rules(ctx, db, FetchState{LastSuccess: morning.Add(-2 * time.Hour), LastError: "x"}, morning, DefaultSettings()); len(got) != 0 {
		t.Errorf("a recent success: %+v", got)
	}

	// the ring: its last reading a day ago, while fetches work
	if err := db.SaveHeartRates(ctx, []store.HeartRate{{Time: morning.Add(-24 * time.Hour), BPM: 60, Source: "awake"}}); err != nil {
		t.Fatal(err)
	}
	if err := db.SaveBatteryLevels(ctx, []store.BatteryLevel{{Time: morning.Add(-25 * time.Hour), Level: 12}}); err != nil {
		t.Fatal(err)
	}
	got, _ = Rules(ctx, db, fresh, morning, DefaultSettings())
	if len(got) != 1 || got[0].Key != "sync:2026-11-19" || !strings.Contains(got[0].Message, "Wed 9:00 AM") ||
		!strings.Contains(got[0].Message, "12%") {
		t.Errorf("sync: %+v", got)
	}
	if err := db.SaveHeartRates(ctx, []store.HeartRate{{Time: morning.Add(-time.Hour), BPM: 60, Source: "awake"}}); err != nil {
		t.Fatal(err)
	}
	if got, _ := Rules(ctx, db, fresh, morning, DefaultSettings()); len(got) != 0 {
		t.Errorf("synced an hour ago: %+v", got)
	}
}

// phone records what it's sent, failing when told to.
type phone struct {
	got  []Alert
	link []string
	fail bool
}

func (p *phone) Send(_ context.Context, a Alert, link string) error {
	if p.fail {
		return errors.New("unreachable")
	}
	p.got, p.link = append(p.got, a), append(p.link, link)
	return nil
}
func (p *phone) Target() string { return "test phone" }

// An alert goes out once, with its link; not in quiet hours (it waits for
// morning); not past the day's cap; and a failed send is tried again.
func TestEngine(t *testing.T) {
	ctx := context.Background()
	p := &phone{}
	e := NewEngine(sick(t), p, "http://box:8090/")
	clock := morning
	e.Now = func() time.Time { return clock }
	stale := FetchState{LastSuccess: morning.Add(-20 * time.Hour), LastError: "got 401"}

	clock = time.Date(2026, 11, 19, 6, 30, 0, 0, time.Local) // quiet until 7
	if sent, _ := e.Check(ctx, stale); len(sent) != 0 || len(p.got) != 0 {
		t.Fatalf("quiet hours sent %+v", sent)
	}
	e.MaxPerDay = 1
	clock = morning
	sent, err := e.Check(ctx, stale)
	if err != nil || len(sent) != 1 || sent[0].Key != "illness:2026-11-19" ||
		p.link[0] != "http://box:8090/emulator?show=early-warning" {
		t.Fatalf("morning: sent %+v, links %v, %v", sent, p.link, err)
	}
	if sent, _ := e.Check(ctx, stale); len(sent) != 0 {
		t.Errorf("over the cap, sent %+v", sent)
	}
	e.MaxPerDay = 5
	if sent, _ := e.Check(ctx, stale); len(sent) != 1 || sent[0].Key != "fetch:2026-11-19" {
		t.Errorf("under the cap: sent %+v", sent)
	}
	if sent, _ := e.Check(ctx, stale); len(sent) != 0 {
		t.Errorf("sent twice: %+v", sent)
	}

	// a failed send is reported, not recorded, and goes out next time
	clock = morning.AddDate(0, 0, 1)
	p.fail = true
	if _, err := e.Check(ctx, stale); err == nil {
		t.Error("a failed send wasn't reported")
	}
	p.fail = false
	if sent, _ := e.Check(ctx, stale); len(sent) != 1 || sent[0].Key != "fetch:2026-11-20" {
		t.Errorf("retry: sent %+v", sent)
	}
	recent, err := e.DB.RecentAlerts(ctx, 10, time.Local)
	if err != nil || len(recent) != 3 || recent[0].Key != "fetch:2026-11-20" {
		t.Errorf("recorded %+v, %v", recent, err)
	}
	if err := e.Test(ctx); err != nil || p.got[len(p.got)-1].Title != "Wolfe Health test" {
		t.Errorf("test alert: %v", err)
	}
}

// Home Assistant gets the notify action: the token, the title and
// message, the tap's link and the interruption level.
func TestHomeAssistant(t *testing.T) {
	var gotPath, gotAuth, gotBody string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotAuth = r.URL.Path, r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		if r.Header.Get("Authorization") != "Bearer tok" {
			http.Error(w, "401: Unauthorized", http.StatusUnauthorized)
		}
	}))
	defer ts.Close()
	if NewHomeAssistant(ts.URL, "", "x") != nil {
		t.Error("no token should be no notifier")
	}
	ha := NewHomeAssistant(ts.URL+"/", "tok", "mobile_app_phone")
	err := ha.Send(context.Background(), Alert{Title: "T", Message: "M", Level: "time-sensitive"}, "http://box/x")
	if err != nil || gotPath != "/api/services/notify/mobile_app_phone" || gotAuth != "Bearer tok" ||
		!strings.Contains(gotBody, `"url":"http://box/x"`) || !strings.Contains(gotBody, `"interruption-level":"time-sensitive"`) ||
		!strings.Contains(gotBody, `"title":"T"`) {
		t.Errorf("sent %s %s %s, %v", gotPath, gotAuth, gotBody, err)
	}
	ha.Token = "wrong"
	if err := ha.Send(context.Background(), Alert{Title: "T"}, ""); err == nil || !strings.Contains(err.Error(), "401") {
		t.Errorf("a refused token: %v", err)
	}
}

// The morning summary goes out from 8, once last night has synced; if it
// hasn't by 11, it goes out saying so.
func TestMorningSummary(t *testing.T) {
	ctx := context.Background()
	at := func(h, m int) time.Time { return time.Date(2026, 11, 19, h, m, 0, 0, time.Local) }
	summary := func(db *store.DB, now time.Time) []Alert {
		t.Helper()
		got, err := Rules(ctx, db, FetchState{LastSuccess: now.Add(-time.Hour)}, now, DefaultSettings())
		if err != nil {
			t.Fatal(err)
		}
		var out []Alert
		for _, a := range got {
			if strings.HasPrefix(a.Key, "summary:") {
				out = append(out, a)
			}
		}
		return out
	}
	synced := testDB(t, nil, nil, nil)
	if err := synced.Save(ctx, []store.Sample{{Metric: store.MetricSleep, Day: "2026-11-19", Value: 92}}); err != nil {
		t.Fatal(err)
	}
	if got := summary(synced, at(7, 59)); len(got) != 0 {
		t.Errorf("7:59: %+v", got)
	}
	got := summary(synced, at(8, 0))
	if len(got) != 1 || got[0].Key != "summary:2026-11-19" || !strings.HasPrefix(got[0].Title, "Sleep 92") ||
		got[0].Path != "/emulator?show=daily" {
		t.Fatalf("8:00: %+v", got)
	}

	unsynced := testDB(t, nil, nil, nil) // no sleep score for last night
	if got := summary(unsynced, at(10, 55)); len(got) != 0 {
		t.Errorf("10:55, still waiting: %+v", got)
	}
	if got := summary(unsynced, at(11, 0)); len(got) != 1 || !strings.Contains(got[0].Message, "hasn't synced") {
		t.Errorf("11:00: %+v", got)
	}
}

// Quiet hours read as a span of whole hours, or off; the page words them
// with the cap.
func TestParseQuiet(t *testing.T) {
	for in, want := range map[string][2]int{"22-7": {22, 7}, "22:00-07:00": {22, 7}, " 1-6 ": {1, 6}, "off": {0, 0}} {
		from, to, err := ParseQuiet(in)
		if err != nil || from != want[0] || to != want[1] {
			t.Errorf("%q: %d-%d, %v", in, from, to, err)
		}
	}
	for _, bad := range []string{"", "22", "22-25", "7-7", "10pm-7am", "22:30-7"} {
		if _, _, err := ParseQuiet(bad); err == nil {
			t.Errorf("%q was accepted", bad)
		}
	}
	e := NewEngine(nil, &phone{}, "")
	if got := e.Limits(); got != "never between 10 PM and 7 AM (held until then), at most 10 a day" {
		t.Errorf("limits %q", got)
	}
	e.QuietFrom, e.QuietTo, e.MaxPerDay = 0, 0, 3
	if got := e.Limits(); got != "at any hour, at most 3 a day" || e.quiet(time.Date(2026, 1, 1, 3, 0, 0, 0, time.Local)) {
		t.Errorf("off: %q", got)
	}
}

// The test buttons' sends don't use up the day's cap.
func TestButtonsDontCount(t *testing.T) {
	ctx := context.Background()
	p := &phone{}
	e := NewEngine(sick(t), p, "")
	e.Now = func() time.Time { return morning }
	e.MaxPerDay = 1
	for i := 0; i < 3; i++ {
		if err := e.Test(ctx); err != nil {
			t.Fatal(err)
		}
	}
	if err := e.SendSummary(ctx); err != nil {
		t.Fatal(err)
	}
	if sent, err := e.Check(ctx, fresh); err != nil || len(sent) != 1 || sent[0].Key != "illness:2026-11-19" {
		t.Errorf("after four button sends, the rules sent %+v, %v", sent, err)
	}
}

// Settings come from the environment, from the defaults; a bad one is an
// error naming its setting.
func TestLoadSettings(t *testing.T) {
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	s, err := LoadSettings(env(nil))
	if err != nil || s.SummaryHour != 8 || s.IllnessSignals != 2 || s.IllnessNights != 2 ||
		s.FetchStale != 13*time.Hour || s.SyncStale != 18*time.Hour || len(s.Off) != 0 {
		t.Fatalf("defaults %+v, %v", s, err)
	}
	s, err = LoadSettings(env(map[string]string{"WH_ALERT_SUMMARY_AT": "07:00", "WH_ALERTS_OFF": "sync, fetch",
		"WH_ALERT_ILLNESS": "3/2", "WH_ALERT_STALE": "6h/1d"}))
	if err == nil {
		t.Error("1d isn't a Go duration and should be refused")
	}
	s, err = LoadSettings(env(map[string]string{"WH_ALERT_SUMMARY_AT": "07:00", "WH_ALERTS_OFF": "sync, fetch",
		"WH_ALERT_ILLNESS": "3/2", "WH_ALERT_STALE": "6h/24h"}))
	if err != nil || s.SummaryHour != 7 || !s.Off["sync"] || !s.Off["fetch"] || s.on("sync") || !s.on("illness") ||
		s.IllnessSignals != 3 || s.FetchStale != 6*time.Hour || s.SyncStale != 24*time.Hour {
		t.Errorf("set %+v, %v", s, err)
	}
	if got := strings.Join(s.Describe(), "; "); got != "the morning summary from 7 AM, once last night has synced; an early warning when 3 or more signals are up 2 nights running" {
		t.Errorf("described %q", got)
	}
	if s, err := LoadSettings(env(map[string]string{"WH_ALERT_SUMMARY_AT": "off"})); err != nil || s.on("summary") {
		t.Errorf("summary off: %+v, %v", s, err)
	}
	for key, bad := range map[string]string{"WH_ALERTS_OFF": "sleep", "WH_ALERT_SUMMARY_AT": "25",
		"WH_ALERT_ILLNESS": "5/2", "WH_ALERT_STALE": "30m/18h"} {
		if _, err := LoadSettings(env(map[string]string{key: bad})); err == nil || !strings.Contains(err.Error(), key) {
			t.Errorf("%s=%s: %v", key, bad, err)
		}
	}
}

// The rules follow their settings: an alert turned off doesn't fire, and
// the early warning needs its signals and nights.
func TestRulesFollowSettings(t *testing.T) {
	ctx := context.Background()
	db := sick(t) // three signals up, two nights
	illnessWith := func(signals, nights int) bool {
		s := DefaultSettings()
		s.IllnessSignals, s.IllnessNights = signals, nights
		got, err := Rules(ctx, db, fresh, morning, s)
		if err != nil {
			t.Fatal(err)
		}
		return len(got) == 1 && strings.HasPrefix(got[0].Key, "illness:")
	}
	if !illnessWith(3, 2) || illnessWith(4, 2) || illnessWith(2, 3) || !illnessWith(1, 1) {
		t.Error("the early warning doesn't follow its signals/nights")
	}
	off := DefaultSettings()
	off.Off["illness"] = true
	if got, _ := Rules(ctx, db, fresh, morning, off); len(got) != 0 {
		t.Errorf("illness off still sent %+v", got)
	}
	stale := DefaultSettings()
	stale.FetchStale = 3 * time.Hour
	if got, _ := Rules(ctx, testDB(t, nil, nil, nil), FetchState{LastSuccess: morning.Add(-4 * time.Hour), LastError: "x"}, morning, stale); len(got) != 1 {
		t.Errorf("a 3h fetch limit after 4h: %+v", got)
	}
}
