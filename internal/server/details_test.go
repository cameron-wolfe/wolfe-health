package server

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"
)

// Every screen a display can show has detail views, and each is a screen
// a display can show: never itself, never twice.
func TestScreenDetails(t *testing.T) {
	for _, o := range scheduleScreens {
		ds := detailsOf(o.Name)
		if len(ds) == 0 {
			t.Errorf("%s has no detail views", o.Name)
		}
		seen := map[string]bool{}
		for _, d := range ds {
			if !knownScreen(d) || d == o.Name || seen[d] {
				t.Errorf("%s: bad detail %q", o.Name, d)
			}
			seen[d] = true
		}
	}
	for name := range screenDetails {
		if !knownScreen(name) {
			t.Errorf("details for %q, which isn't a screen", name)
		}
	}
	// a button steps through them, then home, and back round to the main
	// screen; from home, just its details
	want := []string{scrOxygen, scrBodySignals, scrEarlyWarning, scrDaily, scrOxygen, scrBodySignals}
	for step, w := range want {
		if got := detailStep(scrOxygen, step); got != w {
			t.Errorf("step %d: %s, want %s", step, got, w)
		}
	}
	if got := buttonSteps(scrDaily); len(got) != len(detailsOf(scrDaily)) {
		t.Errorf("home's steps %v", got)
	}
}

// A wake well before the time given is a press, and steps on; one on time
// after the hold, or in a new slot of the schedule, is back at the start.
func TestButtonStep(t *testing.T) {
	ts := newTRMNLState()
	t0 := time.Date(2026, 11, 19, 7, 0, 0, 0, time.UTC)
	check := func(at time.Duration, part string, wantStep int, wantPressed bool) {
		t.Helper()
		step, pressed := ts.press("m", part, t0.Add(at))
		if step != wantStep || pressed != wantPressed {
			t.Errorf("at %v: step %d pressed %v, want %d %v", at, step, pressed, wantStep, wantPressed)
		}
	}
	check(0, "a", 0, false) // first wake: nothing to compare with
	ts.sleep("m", t0, 15*time.Minute)
	check(time.Minute, "a", 1, true) // early: a press
	ts.sleep("m", t0.Add(time.Minute), 10*time.Minute+30*time.Second)
	check(2*time.Minute, "a", 2, true) // and another
	ts.sleep("m", t0.Add(2*time.Minute), 10*time.Minute+30*time.Second)
	check(12*time.Minute+30*time.Second, "a", 0, false) // on time, the hold over: home
	ts.sleep("m", t0.Add(12*time.Minute+30*time.Second), 15*time.Minute)
	check(14*time.Minute, "b", 1, true) // a press in a new slot starts at the first detail
	ts.sleep("m", t0.Add(14*time.Minute), 10*time.Minute+30*time.Second)
	check(24*time.Minute+40*time.Second, "c", 0, false) // the schedule moved on
}

// Pressing a TRMNL's button shows the main screen's first detail view and
// wakes it again when that view's time is up.
func TestDisplayButton(t *testing.T) {
	_, h := trmnlServer(t, TRMNLConfig{}, 5, true) // the daily brief
	hdr := map[string]string{"ID": mac, "Access-Token": "k", "Width": "800", "Height": "480"}
	if d := call(t, h, "GET", "/api/display", hdr, ""); d["filename"].(string)[:6] != "daily-" {
		t.Fatalf("first wake: %v", d["filename"])
	}
	d := call(t, h, "GET", "/api/display", hdr, "") // at once: the button
	if name := d["filename"].(string); name[:6] != "night-" || d["refresh_rate"] != 630.0 {
		t.Errorf("after a press: %v, refresh %v; want last night for 630s", name, d["refresh_rate"])
	}
}

// The emulator offers a screen's detail views and draws one asked for;
// an unknown screen is refused.
func TestEmulatorDetails(t *testing.T) {
	h := testServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/emulator/now?show=vital-signs", nil))
	var now emulatorNowJSON
	if err := json.NewDecoder(rec.Body).Decode(&now); err != nil || now.Showing != "Vital signs" ||
		len(now.Details) != 4 || now.Details[0].Screen != scrEarlyWarning || now.Details[0].Title != "Early warning" ||
		now.Name != scrVitals || now.Home != scrDaily || now.HomeTitle != "Daily brief" {
		t.Errorf("show=vital-signs: %+v, %v", now, err)
	}
	for u, code := range map[string]int{
		"/current.png?show=early-warning": 200, "/current.png?show=nope": 400, "/emulator/now?show=nope": 400,
	} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", u, nil))
		if r.Code != code {
			t.Errorf("%s: %d, want %d", u, r.Code, code)
		}
	}
}

// The daily brief's image says where its zones are, each opening its
// detail view; a screen without zones says none.
func TestZonesHeader(t *testing.T) {
	h := testServer(t)
	type zone struct {
		Screen     string
		X, Y, W, H float64
	}
	get := func(u string) []zone {
		t.Helper()
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", u, nil))
		var zs []zone
		if err := json.Unmarshal([]byte(rec.Header().Get("X-Zones")), &zs); rec.Code != 200 || err != nil {
			t.Fatalf("%s: %d, X-Zones %q, %v", u, rec.Code, rec.Header().Get("X-Zones"), err)
		}
		return zs
	}
	zs := get("/current.png?show=daily&w=800&h=480")
	want := map[string]bool{scrReadinessParts: true, scrMoon: true, scrHeartToday: true}
	for _, z := range zs {
		if !want[z.Screen] || z.X < 0 || z.Y <= 0 || z.X+z.W > 1 || z.Y+z.H > 1 || z.W <= 0 || z.H <= 0 {
			t.Errorf("zone %+v", z)
		}
		delete(want, z.Screen)
	}
	if len(want) != 0 {
		t.Errorf("missing zones for %v in %+v", want, zs)
	}
	if zs := get("/current.png?show=oxygen"); len(zs) != 0 {
		t.Errorf("oxygen: zones %+v", zs)
	}
	for _, zs := range screenZones { // every zone opens a real screen
		for name, to := range zs {
			if !knownScreen(to) {
				t.Errorf("zone %s opens %q", name, to)
			}
		}
	}
}

// A week's day cards open the daily brief as of their day, and a view can
// be drawn and described as of a date; a bad date is refused.
func TestWeekDayZones(t *testing.T) {
	h := testServer(t)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/current.png?show=week-review", nil))
	var zs []struct{ Screen, Date string }
	if err := json.Unmarshal([]byte(rec.Header().Get("X-Zones")), &zs); err != nil || len(zs) != 7 {
		t.Fatalf("week zones %q, %v", rec.Header().Get("X-Zones"), err)
	}
	for _, z := range zs {
		if z.Screen != scrDaily || len(z.Date) != 10 {
			t.Errorf("zone %+v", z)
		}
	}
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/emulator/now?show=daily&date=2026-11-17", nil))
	var now emulatorNowJSON
	if err := json.NewDecoder(rec.Body).Decode(&now); err != nil || now.Showing != "Daily brief · Tue, Nov 17" {
		t.Errorf("dated view: %+v, %v", now, err)
	}
	for u, code := range map[string]int{
		"/current.png?show=daily&date=2026-11-17": 200, "/current.png?show=sleep-breakdown&date=2026-11-17": 200,
		"/current.png?show=daily&date=nope": 400, "/emulator/now?show=daily&date=nope": 400,
	} {
		r := httptest.NewRecorder()
		h.ServeHTTP(r, httptest.NewRequest("GET", u, nil))
		if r.Code != code {
			t.Errorf("%s: %d, want %d", u, r.Code, code)
		}
	}
}

// On the month views each day so far opens its daily brief; on WolfePhase
// · Month each night slept opens the brief of the morning after it, which
// leads with that night. (Today is Nov 19.)
func TestMonthDayZones(t *testing.T) {
	h := testServer(t)
	for _, c := range []struct {
		u           string
		n           int
		first, last string
	}{
		{"/current.png?show=score-month", 19, "2026-11-01", "2026-11-19"},
		{"/current.png?show=stress-month", 19, "2026-11-01", "2026-11-19"},
		{"/current.png?show=month-report", 19, "2026-11-01", "2026-11-19"},
		{"/current.png?show=wolfephase-month", 18, "2026-11-02", "2026-11-19"},
		{"/current.png?show=score-month/last", 31, "2026-10-01", "2026-10-31"},
		{"/current.png?show=wolfephase-month/last", 31, "2026-10-02", "2026-11-01"},
	} {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest("GET", c.u, nil))
		var zs []struct{ Screen, Date string }
		if err := json.Unmarshal([]byte(rec.Header().Get("X-Zones")), &zs); err != nil || len(zs) != c.n {
			t.Errorf("%s: %d zones, want %d (%v)", c.u, len(zs), c.n, err)
			continue
		}
		if zs[0].Date != c.first || zs[len(zs)-1].Date != c.last || zs[0].Screen != scrDaily {
			t.Errorf("%s: zones run %+v to %+v, want %s to %s", c.u, zs[0], zs[len(zs)-1], c.first, c.last)
		}
	}
}
