package server

import (
	"context"
	"html"
	"image"
	"image/png"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/store"
)

// TestMain silences the per-request log so test output stays readable.
func TestMain(m *testing.M) {
	log.SetOutput(io.Discard)
	os.Exit(m.Run())
}

func testServer(t *testing.T) http.Handler {
	t.Helper()
	db, err := store.Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	ctx := context.Background()
	if err := db.Save(ctx, []store.Sample{
		{Metric: store.MetricSleep, Day: "2026-11-18", Value: 84},
		{Metric: store.MetricSleep, Day: "2026-11-19", Value: 66},
		{Metric: store.MetricReadiness, Day: "2026-11-19", Value: 67},
	}); err != nil {
		t.Fatal(err)
	}
	phases := "4422211113332224"
	if err := db.SaveSleepPeriods(ctx, []store.SleepPeriod{{
		ID: "p", Day: "2026-11-19", Type: "long_sleep",
		BedtimeStart: "2026-11-18T23:00:00-08:00", BedtimeEnd: "2026-11-18T23:08:00-08:00",
		TimeInBed: 480, Phases30Sec: &phases,
	}}); err != nil {
		t.Fatal(err)
	}
	s := newServer(db, &Tracker{}, Config{})
	s.now = func() time.Time { return time.Date(2026, 11, 19, 12, 0, 0, 0, time.Local) }
	return s.routes()
}

func get(t *testing.T, h http.Handler, url string, hdr ...string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("GET", url, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// Every image endpoint returns a PNG of the requested size.
func TestImagesRenderAtRequestedSize(t *testing.T) {
	h := testServer(t)
	for _, u := range []string{
		"/daily.png?w=700&h=440", "/night.png?w=700&h=440",
		"/night.png?day=2026-11-19&w=700&h=440", "/trends.png?days=30&w=700&h=440",
		"/stress-month.png?w=700&h=440", "/stress-month.png?month=2026-06&w=700&h=440",
		"/score-month.png?w=700&h=440", "/score-month.png?month=2026-06&w=700&h=440",
		"/sleep-breakdown.png?days=14&w=700&h=440",
		"/sleep-stages.png?days=14&w=700&h=440",
		"/weekly-rhythm.png?w=700&h=440",
		"/body-signals.png?w=700&h=440",
		"/oxygen.png?w=700&h=440", "/oxygen.png?days=14&w=700&h=440",
		"/heart-today.png?w=700&h=440", "/heart-today.png?day=2026-11-18&w=700&h=440",
		"/heart-yesterday.png?w=700&h=440",
		"/sleep-schedule.png?w=700&h=440", "/sleep-schedule.png?days=7&w=700&h=440",
		"/fitness.png?w=700&h=440", "/fitness.png?days=90&w=700&h=440",
		"/bedtime-payoff.png?w=700&h=440", "/bedtime-payoff.png?days=60&w=700&h=440",
		"/vital-signs.png?w=700&h=440", "/vital-signs.png?date=2026-11-12&w=700&h=440",
		"/deviation-radar.png?w=700&h=440", "/deviation-radar.png?date=2026-11-12&w=700&h=440",
		"/week-radar.png?w=700&h=440", "/week-radar.png?week=2026-11-10&w=700&h=440",
		"/wolfephase.png?w=700&h=440", "/wolfephase.png?day=2026-11-19&w=700&h=440", "/wolfemoon.png?w=700&h=440",
		"/month-report.png?w=700&h=440", "/month-report.png?month=2026-10&w=700&h=440",
		"/week-review.png?w=700&h=440", "/week-review.png?week=2026-11-19&w=700&h=440",
	} {
		rec := get(t, h, u)
		if rec.Code != 200 || rec.Header().Get("Content-Type") != "image/png" {
			t.Errorf("%s: %d %s %s", u, rec.Code, rec.Header().Get("Content-Type"), rec.Body.String())
			continue
		}
		img, err := png.Decode(rec.Body)
		if err != nil {
			t.Errorf("%s: %v", u, err)
			continue
		}
		if b := img.Bounds(); b.Dx() != 700 || b.Dy() != 440 {
			t.Errorf("%s: size %v, want 700x440", u, b.Size())
		}
	}
}

func TestBadRequests(t *testing.T) {
	h := testServer(t)
	for u, want := range map[string]int{
		"/night.png?day=11/19":            400,
		"/night.png?day=2020-01-01":       404, // no sleep that day
		"/daily.png?w=0":                  400,
		"/daily.png?w=5000":               400,
		"/trends.png?days=5":              400,
		"/week-review.png?week=11-19":     400,
		"/nothing-here":                   404,
		"/stress-month.png?month=2026-13": 400,
		"/score-month.png?month=June":     400,
		"/sleep-breakdown.png?days=1":     400,
		"/oxygen.png?days=3":              400,
		"/heart-today.png?day=yesterday":  400,
		"/sleep-schedule.png?days=90":     400,
		"/fitness.png?days=30":            400,
		"/bedtime-payoff.png?days=10":     400,
		"/week-radar.png?week=last":       400,
	} {
		if rec := get(t, h, u); rec.Code != want {
			t.Errorf("%s: %d, want %d (%s)", u, rec.Code, want, strings.TrimSpace(rec.Body.String()))
		}
	}
}

// A client that already has the image gets a bodiless 304.
func TestETagRevalidation(t *testing.T) {
	h := testServer(t)
	first := get(t, h, "/daily.png")
	etag := first.Header().Get("ETag")
	if etag == "" {
		t.Fatal("no ETag")
	}
	again := get(t, h, "/daily.png", "If-None-Match", etag)
	if again.Code != http.StatusNotModified || again.Body.Len() != 0 {
		t.Errorf("revalidation: %d with %d bytes, want 304 and empty", again.Code, again.Body.Len())
	}
	// a different size is a different image
	other := get(t, h, "/daily.png?w=640&h=384", "If-None-Match", etag)
	if other.Code != 200 {
		t.Errorf("different size answered %d, want 200", other.Code)
	}
}

func TestDownloadSetsFilename(t *testing.T) {
	h := testServer(t)
	rec := get(t, h, "/night.png?day=2026-11-19&download=1")
	if cd := rec.Header().Get("Content-Disposition"); !strings.Contains(cd, `filename="wolfe-health-night-2026-11-19.png"`) {
		t.Errorf("Content-Disposition = %q", cd)
	}
	if cd := get(t, h, "/night.png").Header().Get("Content-Disposition"); cd != "" {
		t.Errorf("plain request should display inline, got %q", cd)
	}
}

// The index lists every chart, carries ?w=&h= into image and download
// links, lists the stored nights, and shows a failed fetch.
// The panel's pages: Today has the fetch status, the tab bar and the key
// charts; Charts has every chart, sized as asked, and the recent nights;
// each other page answers under the same tabs.
func TestIndex(t *testing.T) {
	h := testServer(t)
	today := get(t, h, "/").Body.String()
	for _, want := range []string{"not yet run", `<a href="/charts" title="Charts (⌘2)"><svg`, `Charts</a>`, `class="on" aria-current="page"><svg`,
		`src="/daily.png"`, `src="/early-warning.png"`, "Running "} {
		if !strings.Contains(today, want) {
			t.Errorf("today missing %q", want)
		}
	}
	for _, p := range pages {
		rec := get(t, h, p.Path)
		body := rec.Body.String()
		on := strings.Index(body, `aria-current="page">`) // the marked tab: its icon, then its label
		if rec.Code != 200 || on < 0 || !strings.HasPrefix(body[on+len(`aria-current="page">`):], "<svg") ||
			!strings.Contains(body[on:on+strings.Index(body[on:], "</a>")], "</svg>"+p.Label) {
			t.Errorf("%s: %d, or its tab isn't marked", p.Path, rec.Code)
		}
	}
	body := get(t, h, "/charts?w=640&h=384").Body.String()
	for _, want := range []string{
		`src="/daily.png?h=384&amp;w=640"`,
		`src="/trends.png?days=30&amp;h=384&amp;w=640"`,
		`/night.png?day=2026-11-19&amp;h=384&amp;w=640&amp;download=1`,
		"Woke Thu Nov 19",
		`src="/stress-month.png?h=384&amp;month=2026-10&amp;w=640"`,
		`src="/sleep-breakdown.png?days=30&amp;h=384&amp;w=640"`,
		`src="/sleep-stages.png?days=30&amp;h=384&amp;w=640"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
}

func TestIndexShowsFetchError(t *testing.T) {
	db, _ := store.Open(filepath.Join(t.TempDir(), "x.db"))
	defer db.Close()
	tr := &Tracker{}
	tr.Update(func(s *Status) { s.LastAttempt = time.Now(); s.LastError = "no Oura token yet" })
	body := get(t, New(db, tr, Config{}), "/").Body.String()
	if !strings.Contains(body, "no Oura token yet") || !strings.Contains(body, `class="bad"`) {
		t.Error("index should flag the failed fetch")
	}
}

// While a fetch runs, the page says so, rather than "never" during the long
// back-fill after a start; a last success restored from the database shows
// as from before this start.
func TestIndexShowsFetchInProgress(t *testing.T) {
	db, _ := store.Open(filepath.Join(t.TempDir(), "x.db"))
	defer db.Close()
	tr := &Tracker{}
	tr.Update(func(s *Status) { s.LastSuccess, s.Restored = time.Now().Add(-2*time.Hour), true })
	body := get(t, New(db, tr, Config{}), "/").Body.String()
	if st := statusLine(body); !strings.Contains(st, "2h 0m ago") || !strings.Contains(st, "before this start") || strings.Contains(st, "never") {
		t.Errorf("restored last success not shown:\n%s", statusLine(body))
	}
	tr.Update(func(s *Status) { s.LastAttempt, s.Fetching, s.Backfill = time.Now(), true, true })
	body = get(t, New(db, tr, Config{}), "/").Body.String()
	if !strings.Contains(body, "fetching now") || !strings.Contains(body, "back-filling") || !strings.Contains(body, "last success 2h 0m ago") {
		t.Errorf("fetch in progress not shown:\n%s", statusLine(body))
	}
}

// statusLine is the index page's fetch status, for test messages.
func statusLine(body string) string {
	i := strings.Index(body, "Last fetch:")
	if i < 0 {
		return body
	}
	return body[i:min(len(body), i+400)]
}

// With an elevated temperature last night, the index page carries the
// warning too, and every image gets the banner.
func TestIndexShowsAlert(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "a.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	var samples []store.Sample
	for i := 0; i < 30; i++ {
		v := []float64{-0.1, 0, 0.1}[i%3]
		if i == 0 {
			v = 0.9
		}
		samples = append(samples, store.Sample{Metric: store.MetricTempDev,
			Day: time.Date(2026, 11, 19-i, 0, 0, 0, 0, time.UTC).Format("2006-01-02"), Value: v})
	}
	db.Save(context.Background(), samples)
	s := newServer(db, &Tracker{}, Config{})
	s.now = func() time.Time { return time.Date(2026, 11, 19, 12, 0, 0, 0, time.Local) }
	body := html.UnescapeString(get(t, s.routes(), "/").Body.String()) // the template escapes "+"
	if !strings.Contains(body, `class="alert"`) || !strings.Contains(body, "Temperature +0.9 °C") {
		i := strings.Index(body, "<main>")
		t.Errorf("index should show the warning; page starts: %.300s", body[max(0, i):])
	}
}

// ?alert=preview draws a sample banner on demand, so it can be seen before
// it has any reason to appear; without it, quiet data means no banner.
func TestAlertPreview(t *testing.T) {
	h := testServer(t)
	bannerInk := func(url string) int {
		rec := get(t, h, url)
		img, err := png.Decode(rec.Body)
		if err != nil {
			t.Fatalf("%s: %v", url, err)
		}
		n := 0
		for y := 32; y < 60; y++ { // just under the 30px masthead
			for x := 0; x < 800; x++ {
				if r, _, _, _ := img.At(x, y).RGBA(); r == 0 {
					n++
				}
			}
		}
		return n
	}
	plain, preview := bannerInk("/daily.png"), bannerInk("/daily.png?alert=preview")
	if preview < plain+1000 {
		t.Errorf("preview banner not drawn: %d ink pixels vs %d without", preview, plain)
	}
}

// The retired per-metric trend charts redirect to the trends screen,
// keeping ?days= when it fits.
func TestLineRedirects(t *testing.T) {
	h := testServer(t)
	for url, want := range map[string]string{
		"/line/sleep.png":         "/trends.png",
		"/line/steps.png?days=90": "/trends.png?days=90",
		"/line/sleep.png?days=7":  "/trends.png", // too short for trends
	} {
		rec := get(t, h, url)
		if rec.Code != 301 || rec.Header().Get("Location") != want {
			t.Errorf("%s: %d -> %q, want 301 -> %q", url, rec.Code, rec.Header().Get("Location"), want)
		}
	}
}

// ?date= draws a screen as of that day: today is the date (so its name and
// defaults follow it) and the masthead says so; a future or malformed date
// is refused.
func TestRenderAsOfDate(t *testing.T) {
	db, err := store.Open(filepath.Join(t.TempDir(), "d.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s := newServer(db, &Tracker{}, Config{})
	s.now = func() time.Time { return time.Date(2026, 11, 19, 12, 0, 0, 0, time.Local) }
	ctx := context.Background()

	for screen, want := range map[string]string{
		scrDaily:          "daily-2026-11-12",
		scrHeartToday:     "heart-2026-11-12",
		scrHeartYesterday: "heart-2026-11-11",
		scrStressMonth:    "stress-2026-11",
		scrWeekReview:     "week-2026-11-01", // the week before Nov 12's (Sunday Nov 8)
		scrLastMonth:      "month-2026-10",
	} {
		_, name, err := s.render(ctx, screen, url.Values{"date": {"2026-11-12"}}, 800, 480)
		if err != nil || name != want {
			t.Errorf("%s as of Nov 12: %q, %v; want %q", screen, name, err, want)
		}
	}
	// the masthead names the date; today's own date leaves it off
	dated, _, _ := s.render(ctx, scrDaily, url.Values{"date": {"2026-11-12"}}, 800, 480)
	plain, _, _ := s.render(ctx, scrDaily, url.Values{"date": {"2026-11-19"}}, 800, 480)
	live, _, _ := s.render(ctx, scrDaily, nil, 800, 480)
	if dated == nil || plain == nil || live == nil {
		t.Fatal("render failed")
	}
	mast := mastheadRows(dated)
	if mast == mastheadRows(live) {
		t.Error("a dated screen's masthead should name its date")
	}
	if mastheadRows(plain) != mastheadRows(live) {
		t.Error("today's date shouldn't change the masthead")
	}
	for _, bad := range []string{"2026-11-20", "11/12/2026"} {
		if _, _, err := s.render(ctx, scrDaily, url.Values{"date": {bad}}, 800, 480); err == nil {
			t.Errorf("date %q: want an error", bad)
		}
	}
}

// mastheadRows is the masthead's pixels as a string, for comparing two.
func mastheadRows(img *image.Gray) string {
	return string(img.Pix[:img.Stride*30])
}

// The index page carries a date of interest into every chart link, but not
// into the nights list, whose links name their own night.
func TestIndexAsOfDate(t *testing.T) {
	h := testServer(t)
	body := get(t, h, "/charts?date=2026-11-12").Body.String()
	for _, want := range []string{
		`src="/daily.png?date=2026-11-12"`,
		`src="/stress-month.png?date=2026-11-12&amp;month=2026-10"`,
		`value="2026-11-12"`,
		"Back to today",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("index missing %q", want)
		}
	}
	// "Last night" is the night ending on the date: it carries it
	if !strings.Contains(body, `src="/night.png?date=2026-11-12"`) {
		t.Error("last night should be drawn as of the date")
	}
	// the nights list names each night, and stops at the date
	if strings.Contains(body, `date=2026-11-12&amp;day=`) {
		t.Error("the nights list shouldn't carry the page's date")
	}
	if !strings.Contains(get(t, h, "/charts").Body.String(), `night.png?day=2026-11-19`) ||
		strings.Contains(get(t, h, "/charts?date=2026-11-18").Body.String(), `night.png?day=2026-11-19`) {
		t.Error("the nights list should stop at the date of interest")
	}
	if rec := get(t, h, "/charts?date=2027-01-01"); rec.Code != 400 {
		t.Errorf("future date: %d", rec.Code)
	}
}

// A date of interest picks the night that ended on it, for both night
// screens: with no sleep stored that day, there's nothing to show, rather
// than the latest night passing for it.
func TestNightScreensFollowDate(t *testing.T) {
	h := testServer(t)
	for _, u := range []string{"/night.png", "/wolfephase.png"} {
		if rec := get(t, h, u); rec.Code != 200 {
			t.Errorf("%s: %d", u, rec.Code)
		}
		if rec := get(t, h, u+"?date=2026-11-18"); rec.Code != 404 {
			t.Errorf("%s?date=2026-11-18 (no sleep that day): %d, want 404", u, rec.Code)
		}
	}
}

// A path the server doesn't have: a browser gets the panel's 404 page,
// with the wolf mark and the way back; anything else, plain text.
func TestNotFound(t *testing.T) {
	h := testServer(t)
	req := httptest.NewRequest("GET", "/nope", nil)
	req.Header.Set("Accept", "text/html,application/xhtml+xml")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	body := rec.Body.String()
	if rec.Code != 404 || !strings.Contains(body, `class="code">404`) || !strings.Contains(body, "<code>/nope</code>") ||
		!strings.Contains(body, `href="/">Back to Today`) || !strings.Contains(body, "<svg") {
		t.Errorf("browser 404: %d\n%.400s", rec.Code, body)
	}
	plain := get(t, h, "/nope.png")
	if plain.Code != 404 || strings.Contains(plain.Body.String(), "<html") {
		t.Errorf("non-browser 404: %d %q", plain.Code, plain.Body.String())
	}
	// the real pages still answer, and their charts sit on the placeholder
	if page := get(t, h, "/charts"); page.Code != 200 || !strings.Contains(page.Body.String(), `class="shot"`) ||
		!strings.Contains(page.Body.String(), "--ratio: 800 / 480") {
		t.Errorf("charts: %d, no placeholder boxes", page.Code)
	}
}

// The small touches: a confirmation after a form shows as a toast that
// fades (failures stay on the page), removing a log asks first, each tab
// names its ⌘ shortcut, and every page carries the connection watcher.
func TestPanelTouches(t *testing.T) {
	h := testServer(t)
	for u, want := range map[string]string{
		"/display?schedule=saved":  `class="toast" id="toast" role="status">Schedule saved.`,
		"/alerts?alerts=test-sent": `class="toast" id="toast" role="status">Test sent: check the phone.`,
		"/habits?habit=removed":    `class="toast" id="toast" role="status">Log removed.`,
	} {
		if body := get(t, h, u).Body.String(); !strings.Contains(body, want) {
			t.Errorf("%s: no toast %q", u, want)
		}
	}
	if body := get(t, h, "/display").Body.String(); strings.Contains(body, `id="toast"`) {
		t.Error("a toast with nothing to say")
	}
	body := get(t, h, "/").Body.String()
	for _, want := range []string{`title="Settings (⌘6)"`, `id="offline"`, "Can't reach Wolfe Health", `fetch("/status"`} {
		if !strings.Contains(body, want) {
			t.Errorf("today missing %q", want)
		}
	}
}
