// Package server serves the charts over HTTP: an index page for people, and
// PNG endpoints that a browser or an e-ink panel can fetch.
//
// Every image is rendered on request from the database. A render takes
// milliseconds, so there is nothing to pre-render or go stale — the daily view
// rolls over at midnight whether or not a fetch has run since.
package server

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"image"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/alerts"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
	"github.com/cameron-wolfe/wolfe-health/internal/version"
)

// Image size limits and defaults. 800x480 is a common 7.5" e-ink panel.
const (
	defaultW, defaultH = 800, 480
	maxSide            = 4000
	defaultLineDays    = 30
)

// Status is what the fetch loop reports, shown on the index page and at
// /status so a stalled fetch is visible rather than silent.
type Status struct {
	Started     time.Time     `json:"started"`
	Interval    time.Duration `json:"interval_ns"`
	LastAttempt time.Time     `json:"last_attempt,omitzero"`
	LastSuccess time.Time     `json:"last_success,omitzero"`
	LastResult  string        `json:"last_result,omitempty"`
	LastError   string        `json:"last_error,omitempty"`
	NextFetch   time.Time     `json:"next_fetch,omitzero"`
	// Fetching is set while a fetch runs (it started at LastAttempt), and
	// Backfill while that fetch is the longer one after a start.
	Fetching bool `json:"fetching,omitempty"`
	Backfill bool `json:"backfill,omitempty"`
	// Restored marks LastSuccess as read from the database at start, from
	// before this run, rather than seen by it.
	Restored bool `json:"restored,omitempty"`
	// Webhooks is the state of Oura's webhook subscriptions, empty when
	// webhooks are off; LastWebhook is when the last notification came,
	// and LastWebhookEvent what it was.
	Webhooks         string    `json:"webhooks,omitempty"`
	LastWebhook      time.Time `json:"last_webhook,omitzero"`
	LastWebhookEvent string    `json:"last_webhook_event,omitempty"`
}

// Tracker holds the current Status for concurrent readers and one writer.
type Tracker struct {
	mu sync.Mutex
	s  Status
}

// Get returns a copy of the current status.
func (t *Tracker) Get() Status {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.s
}

// Update changes the status under the lock.
func (t *Tracker) Update(f func(*Status)) {
	t.mu.Lock()
	defer t.mu.Unlock()
	f(&t.s)
}

type server struct {
	db       *store.DB
	status   *Tracker
	now      func() time.Time // swappable for tests
	trmnl    *trmnlState
	trmnlCfg TRMNLConfig
	alerts   *alerts.Engine // nil: alerts are off
	verbose  bool           // log every request
	settings []Setting      // every setting in effect, for the Settings page
}

// Config is the server's optional settings.
type Config struct {
	TRMNL  TRMNLConfig
	Alerts *alerts.Engine // phone alerts, or nil when they're off
	// LogAll logs every request; otherwise routine reads are left out
	// (logRequests)
	LogAll bool
	// Settings are every setting in effect, for the Settings page; the
	// daemon fills them in (nil: the page says they aren't known)
	Settings []Setting
}

// New returns the HTTP handler for the given database and fetch status.
func New(db *store.DB, status *Tracker, cfg Config) http.Handler {
	return newServer(db, status, cfg).routes()
}

func newServer(db *store.DB, status *Tracker, cfg Config) *server {
	cfg.TRMNL.defaults()
	return &server{db: db, status: status, now: time.Now, trmnl: newTRMNLState(), trmnlCfg: cfg.TRMNL, alerts: cfg.Alerts, verbose: cfg.LogAll, settings: cfg.Settings}
}

func (s *server) routes() http.Handler {
	mux := http.NewServeMux()
	// the control panel: a page per tab, sharing one layout (index.go)
	for _, p := range pages {
		path := p.Path
		if path == "/" {
			path = "/{$}"
		}
		mux.HandleFunc("GET "+path, s.page(p.Name))
	}
	mux.HandleFunc("GET /app/manifest.webmanifest", s.appManifest)
	mux.HandleFunc("/", s.notFound) // anything not matched above
	mux.HandleFunc("GET /daily.png", s.imageHandler(scrDaily))
	mux.HandleFunc("GET /daily-digest.png", s.imageHandler(scrDigest))
	mux.HandleFunc("GET /night.png", s.imageHandler(scrNight))
	mux.HandleFunc("GET /trends.png", s.imageHandler(scrTrends))
	mux.HandleFunc("GET /week-review.png", s.imageHandler(scrWeekReview))
	mux.HandleFunc("GET /line/{file}", s.lineRedirect)
	mux.HandleFunc("GET /stress-month.png", s.imageHandler(scrStressMonth))
	mux.HandleFunc("GET /score-month.png", s.imageHandler(scrScoreMonth))
	mux.HandleFunc("GET /oxygen.png", s.imageHandler(scrOxygen))
	mux.HandleFunc("GET /heart-today.png", s.imageHandler(scrHeartToday))
	mux.HandleFunc("GET /heart-yesterday.png", s.imageHandler(scrHeartYesterday))
	mux.HandleFunc("GET /sleep-schedule.png", s.imageHandler(scrSleepSchedule))
	mux.HandleFunc("GET /fitness.png", s.imageHandler(scrFitness))
	mux.HandleFunc("GET /bedtime-payoff.png", s.imageHandler(scrPayoff))
	mux.HandleFunc("GET /early-warning.png", s.imageHandler(scrEarlyWarning))
	mux.HandleFunc("GET /vital-signs.png", s.imageHandler(scrVitals))
	mux.HandleFunc("GET /deviation-radar.png", s.imageHandler(scrDeviation))
	mux.HandleFunc("GET /week-radar.png", s.imageHandler(scrWeekRadar))
	mux.HandleFunc("GET /wolfephase.png", s.imageHandler(scrMoon))
	mux.HandleFunc("GET /wolfephase-month.png", s.imageHandler(scrPhaseMonth))
	mux.HandleFunc("GET /habits-week.png", s.imageHandler(scrHabitWeek))
	mux.HandleFunc("GET /habit-effects.png", s.imageHandler(scrHabitEffects))
	mux.HandleFunc("GET /wolfemoon.png", s.imageHandler(scrMoon)) // its first name, kept so old links work
	mux.HandleFunc("GET /sleep-breakdown.png", s.imageHandler(scrSleepBreakdown))
	mux.HandleFunc("GET /readiness-breakdown.png", s.imageHandler(scrReadinessParts))
	mux.HandleFunc("GET /sleep-stages.png", s.imageHandler(scrSleepStages))
	mux.HandleFunc("GET /weekly-rhythm.png", s.imageHandler(scrWeeklyRhythm))
	mux.HandleFunc("GET /body-signals.png", s.imageHandler(scrBodySignals))
	mux.HandleFunc("GET /month-report.png", s.imageHandler(scrMonthReport))
	mux.HandleFunc("GET /status", s.statusJSON)
	mux.HandleFunc("GET /summary.txt", s.summary)
	mux.HandleFunc("GET /health.txt", s.health)
	mux.HandleFunc("GET /current.png", s.current)
	mux.HandleFunc("GET /emulator", s.emulator)
	mux.HandleFunc("GET /emulator/now", s.emulatorNow)
	mux.HandleFunc("GET /emulator/icon.png", s.emulatorIcon)
	mux.HandleFunc("GET /emulator/manifest.webmanifest", s.emulatorManifest)
	mux.HandleFunc("POST /schedule", s.saveSchedule)
	mux.HandleFunc("GET /log/habits", s.habitList)
	mux.HandleFunc("POST /log", s.logHabit)
	mux.HandleFunc("POST /log/undo", s.undoHabit)
	mux.HandleFunc("POST /log/delete", s.deleteHabit)
	mux.HandleFunc("POST /log/edit", s.editHabit)
	mux.HandleFunc("POST /alerts/test", s.testAlert)
	mux.HandleFunc("POST /alerts/summary", s.summaryAlert)
	mux.HandleFunc("GET /log/export.csv", s.exportHabits)

	// TRMNL e-ink displays (bring your own server)
	mux.HandleFunc("GET /api/setup", s.trmnlSetup)
	mux.HandleFunc("GET /api/display", s.trmnlDisplay)
	mux.HandleFunc("POST /api/log", s.trmnlLog)
	mux.HandleFunc("GET /trmnl/img/{file}", s.trmnlImage)
	mux.HandleFunc("GET /trmnl/setup.png", s.trmnlSetupImage)
	return logRequests(mux, s.verbose)
}

// today is the server's local calendar date.
func (s *server) today() time.Time {
	n := s.now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, time.UTC)
}

// size reads ?w= and ?h=, defaulting to the panel size.
func size(r *http.Request) (int, int, error) {
	w, err := intParam(r, "w", defaultW, 1, maxSide)
	if err != nil {
		return 0, 0, err
	}
	h, err := intParam(r, "h", defaultH, 1, maxSide)
	if err != nil {
		return 0, 0, err
	}
	return w, h, nil
}

func intParam(r *http.Request, name string, def, min, max int) (int, error) {
	return qInt(r.URL.Query(), name, def, min, max)
}

func (s *server) statusJSON(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	// the fetch status, and which build is answering
	enc.Encode(struct {
		Status
		Version version.Info `json:"version"`
	}{s.status.Get(), version.Get()})
}

// writeImage encodes img and sends it with an ETag drawn from its bytes.
//
// The ETag is what lets an e-ink panel poll cheaply: it sends back the tag of
// the image it is showing in If-None-Match, and gets a bodiless 304 when
// nothing has changed — so it can skip a slow, flashing full refresh.
// ?bits=1 or 2 converts for e-ink, ?format=bmp sends a 1-bit BMP, and
// ?download=1 adds a Content-Disposition so browsers save instead of show.
func writeImage(w http.ResponseWriter, r *http.Request, img *image.Gray, name string) {
	q := r.URL.Query()
	bits, err := qInt(q, "bits", 0, 0, 2)
	if err != nil {
		writeErr(w, err)
		return
	}
	data, ctype, err := encodeImage(img, bits, q.Get("format"))
	if err != nil {
		writeErr(w, err)
		return
	}
	sum := sha256.Sum256(data)
	etag := `"` + hex.EncodeToString(sum[:8]) + `"`

	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "no-cache") // may cache, must revalidate
	if q.Get("download") == "1" {
		ext := "png"
		if ctype == "image/bmp" {
			ext = "bmp"
		}
		h.Set("Content-Disposition", fmt.Sprintf(`attachment; filename="wolfe-health-%s.%s"`, name, ext))
	}
	if match := r.Header.Get("If-None-Match"); match != "" && strings.Contains(match, etag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", ctype)
	h.Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}

func serverError(w http.ResponseWriter, err error) {
	log.Printf("server: %v", err)
	http.Error(w, "internal error: "+err.Error(), http.StatusInternalServerError)
}

// statusRecorder captures the response code for the request log.
type statusRecorder struct {
	http.ResponseWriter
	code int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.code = code
	r.ResponseWriter.WriteHeader(code)
}

// slowRequest is how long a request may take before it's logged even when
// the log is quiet.
const slowRequest = 2 * time.Second

// logRequests logs a line per request: method, path, status, duration,
// client. Quiet (the default), it leaves out the routine reads, which are
// most of them (displays checking every minute, the index page's charts):
// a successful GET is logged only when it's slow, or a display's call to
// the TRMNL API. Changes (POSTs), errors and slow requests always are.
// verbose logs every request.
func logRequests(next http.Handler, verbose bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w, code: http.StatusOK}
		next.ServeHTTP(rec, r)
		took := time.Since(start)
		routine := (r.Method == http.MethodGet || r.Method == http.MethodHead) && rec.code < 400 &&
			took < slowRequest && !strings.HasPrefix(r.URL.Path, "/api/")
		if verbose || !routine {
			log.Printf("http: %s %s %d %s %s", r.Method, r.URL.RequestURI(), rec.code,
				took.Round(time.Millisecond), r.RemoteAddr)
		}
	})
}
