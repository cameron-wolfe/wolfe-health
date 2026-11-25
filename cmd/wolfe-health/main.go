// Command wolfe-health fetches Oura ring data into a local database, renders
// it as charts for an e-ink display, and serves them over HTTP.
//
// Usage:
//
//	wolfe-health         fetch the trailing WH_DAYS of data into the database
//	wolfe-health auth    log in to Oura in the browser and save a token (once)
//	wolfe-health chart   render a chart from the database to a PNG file
//	wolfe-health daemon  fetch on a schedule and serve the charts over HTTP
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/alerts"
	"github.com/cameron-wolfe/wolfe-health/internal/config"
	"github.com/cameron-wolfe/wolfe-health/internal/dashboard"
	"github.com/cameron-wolfe/wolfe-health/internal/fetch"
	"github.com/cameron-wolfe/wolfe-health/internal/oura"
	"github.com/cameron-wolfe/wolfe-health/internal/server"
	"github.com/cameron-wolfe/wolfe-health/internal/store"
	"github.com/cameron-wolfe/wolfe-health/internal/version"
	"github.com/cameron-wolfe/wolfe-health/internal/webhook"
)

// Metric names as stored in the database; short local names for store's.
const (
	metricSleep     = store.MetricSleep
	metricReadiness = store.MetricReadiness
	metricStress    = store.MetricStress
	metricActivity  = store.MetricActivity
	metricSteps     = store.MetricSteps
)

func main() {
	var err error
	switch {
	case len(os.Args) == 1:
		err = run()
	case os.Args[1] == "auth":
		err = auth(os.Args[2:])
	case os.Args[1] == "chart":
		err = chartCmd(os.Args[2:])
	case os.Args[1] == "daemon":
		err = daemonCmd(os.Args[2:])
	case os.Args[1] == "webhooks":
		err = webhooksCmd(os.Args[2:])
	case os.Args[1] == "habits":
		err = habitsCmd(os.Args[2:])
	case os.Args[1] == "reset":
		err = resetCmd(os.Args[2:])
	case os.Args[1] == "version", os.Args[1] == "-version", os.Args[1] == "--version":
		fmt.Println("wolfe-health", version.Get().String(time.Local))
	default:
		err = fmt.Errorf("usage: %s [auth -h | chart -h | daemon -h | webhooks | habits export|import | reset TABLE | version]", os.Args[0])
	}
	if err != nil {
		log.Fatal(err)
	}
}

// auth runs the one-time browser login and saves the token file.
//
// The approval URL is printed as well as opened, so this also works with no
// browser on the machine — over SSH, with the callback port forwarded:
// ssh -L 8080:localhost:8080 <host>. Where 8080 is taken, -port (or
// WH_AUTH_PORT) picks another; register http://localhost:<port>/callback on
// the Oura app first, since Oura only returns to registered addresses.
func auth(args []string) error {
	fs := flag.NewFlagSet("auth", flag.ContinueOnError)
	defPort := 8080
	if v, err := strconv.Atoi(os.Getenv("WH_AUTH_PORT")); err == nil {
		defPort = v
	}
	port := fs.Int("port", defPort, "local port for the login callback (WH_AUTH_PORT)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *port < 1 || *port > 65535 {
		return errors.New("auth: -port must be 1-65535")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	o := oura.NewOAuth(cfg.ClientID, cfg.ClientSecret)
	o.RedirectURI = oura.RedirectForPort(*port)

	// generous: covers logging in to Oura, not just clicking Approve
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	tok, err := o.Login(ctx, func(url string) {
		fmt.Printf("Approve wolfe-health in your browser. If it does not open, visit:\n\n  %s\n\n", url)
		openBrowser(url)
	})
	if err != nil {
		return err
	}
	if err := oura.SaveToken(cfg.TokenPath, tok); err != nil {
		return err
	}
	fmt.Printf("saved token to %s (scope %q, expires %s)\n",
		cfg.TokenPath, tok.Scope, tok.Expiry().Format("2006-01-02"))
	return nil
}

// chartViews are the command line's names for the screens, in the order the
// help lists them, each with the server's schedule name it draws.
var chartViews = []struct{ view, screen string }{
	{"daily", "daily"}, {"digest", "daily-digest"}, {"night", "night"}, {"phase", "wolfephase"},
	{"phase-month", "wolfephase-month"}, {"vitals", "vital-signs"}, {"deviation", "deviation-radar"},
	{"breakdown", "sleep-breakdown"}, {"readiness", "readiness-breakdown"}, {"trends", "trends"},
	{"week", "week-review"}, {"week-radar", "week-radar"}, {"habits-week", "habits-week"},
	{"habit-effects", "habit-effects"}, {"month", "month-report"}, {"stress-month", "stress-month"},
	{"score-month", "score-month"}, {"stages", "sleep-stages"}, {"schedule", "sleep-schedule"},
	{"payoff", "bedtime-payoff"}, {"rhythm", "weekly-rhythm"}, {"signals", "body-signals"}, {"early-warning", "early-warning"},
	{"fitness", "fitness"}, {"oxygen", "oxygen"}, {"heart", "heart-today"},
	{"heart-yesterday", "heart-today/yesterday"}, {"moon", "wolfephase"}, // WolfePhase's first name
}

// chartCmd renders one screen to a PNG, drawn exactly as the server serves
// it (server.Render), so the two can't drift apart. Each flag is the URL
// parameter of the same name; a screen ignores the ones it doesn't take, and
// one left out gets the screen's own default.
//
// It reads only the database, so it needs no Oura credentials and works
// offline.
func chartCmd(args []string) error {
	var names []string
	for _, v := range chartViews[:len(chartViews)-1] {
		names = append(names, v.view)
	}
	fs := flag.NewFlagSet("chart", flag.ContinueOnError)
	view := fs.String("view", "daily", strings.Join(names, ", "))
	date := fs.String("date", "", "draw the screen as of this day, YYYY-MM-DD (default today)")
	day := fs.String("day", "", "night and WolfePhase: the wake-up day; heart: the day (YYYY-MM-DD)")
	week := fs.String("week", "", "week views: any day YYYY-MM-DD in the week")
	month := fs.String("month", "", "month views: YYYY-MM")
	days := fs.Int("days", 0, "the window in days, for views that have one (default: the view's own)")
	alertPreview := fs.Bool("alert-preview", false, "draw a sample early-warning banner, to see how it looks")
	out := fs.String("o", "", "output PNG (default charts/<view>.png)")
	width := fs.Int("width", 800, "image width in pixels")
	height := fs.Int("height", 480, "image height in pixels")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := applyTempAlertMin(); err != nil { // drawn as the server draws it
		return err
	}
	screen := ""
	for _, v := range chartViews {
		if v.view == *view {
			screen = v.screen
		}
	}
	if screen == "" {
		return fmt.Errorf("chart: unknown -view %q (want %s)", *view, strings.Join(names, ", "))
	}
	q := url.Values{}
	for k, v := range map[string]string{"date": *date, "day": *day, "week": *week, "month": *month} {
		if v != "" {
			q.Set(k, v)
		}
	}
	if *days != 0 {
		q.Set("days", strconv.Itoa(*days))
	}
	if *alertPreview {
		q.Set("alert", "preview")
	}

	db, err := store.Open(config.DBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	img, _, err := server.Render(context.Background(), db, screen, q, *width, *height)
	if err != nil {
		return fmt.Errorf("chart: %w", err)
	}
	if *out == "" {
		*out = "charts/" + *view + ".png"
	}
	if err := savePNG(*out, img); err != nil {
		return err
	}
	fmt.Printf("wrote %s\n", *out)
	return nil
}

// savePNG writes img to path as a PNG, creating the directory if needed.
func savePNG(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := png.Encode(f, img); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// daemonCmd runs continuously: it fetches from Oura on startup and every
// -every after, and serves the charts over HTTP the whole time.
//
// The two halves are independent. A failed fetch (no network, an expired
// token) is logged and shown on the index page, and the server keeps serving
// what the database already holds. The token file is re-read on every fetch,
// so dropping in a fresh one after a `make auth` elsewhere needs no restart.
//
// Being the only process that fetches matters: Oura refresh tokens are
// single-use, and two processes refreshing the same token file would
// invalidate each other. Don't also schedule fetch.sh where the daemon runs.
func daemonCmd(args []string) error {
	fs := flag.NewFlagSet("daemon", flag.ContinueOnError)
	// each flag's default comes from its WH_ setting when that's set, so
	// .env can set them; a flag given on the command line still wins
	var env envDefaults
	listen := fs.String("listen", envOr("WH_LISTEN", ":8090"), "address to serve HTTP on (WH_LISTEN)")
	every := fs.Duration("every", env.duration("WH_FETCH_EVERY", time.Hour), "time between fetches; hourly keeps today's rings current (WH_FETCH_EVERY)")
	days := fs.Int("days", env.int("WH_FETCH_DAYS", 7), "days of history each scheduled fetch refreshes (WH_FETCH_DAYS)")
	startupDays := fs.Int("startup-days", env.int("WH_STARTUP_DAYS", 365), "days the first fetch after start covers, so data a new version stores fills in by itself (WH_STARTUP_DAYS)")
	trmnlBits := fs.Int("trmnl-bits", env.int("WH_TRMNL_BITS", 2), "TRMNL image depth: 2 (four grays, OG firmware 1.6+) or 1 (WH_TRMNL_BITS)")
	trmnlFormat := fs.String("trmnl-format", envOr("WH_TRMNL_FORMAT", "png"), "TRMNL image format: png or bmp (1-bit) (WH_TRMNL_FORMAT)")
	trmnlRefresh := fs.Duration("trmnl-refresh", env.duration("WH_TRMNL_REFRESH", 15*time.Minute), "how long a TRMNL sleeps between updates (WH_TRMNL_REFRESH)")
	webhookListen := fs.String("webhook-listen", envOr("WH_WEBHOOK_LISTEN", ""), "address for Oura's webhook notifications, a port of its own that serves nothing else, to expose publicly (e.g. :8091); empty turns webhooks off (WH_WEBHOOK_LISTEN)")
	backupDir := fs.String("backup-dir", envOr("WH_BACKUP_DIR", ""), "where the daily database snapshots go (default: backups/ beside the database) (WH_BACKUP_DIR)")
	backupKeep := fs.Int("backup-keep", env.int("WH_BACKUP_KEEP", 14), "daily snapshots to keep; 0 turns them off (WH_BACKUP_KEEP)")
	logAll := fs.Bool("log-requests", envBool("WH_LOG_REQUESTS"), "log every HTTP request, the displays' routine reads included; otherwise only changes, errors and slow requests (WH_LOG_REQUESTS=1)")
	webhookURL := fs.String("webhook-url", envOr("WH_WEBHOOK_URL", ""), "the public HTTPS URL that reaches -webhook-listen, ending "+webhook.Path+"; the daemon subscribes it and keeps the subscriptions renewed (WH_WEBHOOK_URL)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if err := env.err(); err != nil {
		return err
	}
	if err := applyTempAlertMin(); err != nil {
		return err
	}
	if *webhookURL != "" {
		if *webhookListen == "" {
			return errors.New("daemon: -webhook-url needs -webhook-listen, the port it reaches")
		}
		if !strings.HasPrefix(*webhookURL, "https://") || !strings.HasSuffix(*webhookURL, webhook.Path) {
			return fmt.Errorf("daemon: -webhook-url must be an https:// URL ending %s", webhook.Path)
		}
	}
	if *every < time.Minute {
		return errors.New("daemon: -every must be at least 1m")
	}
	if *trmnlBits != 1 && *trmnlBits != 2 {
		return errors.New("daemon: -trmnl-bits must be 1 or 2")
	}
	if *trmnlFormat != "png" && *trmnlFormat != "bmp" {
		return errors.New("daemon: -trmnl-format must be png or bmp")
	}
	if *trmnlRefresh < time.Minute {
		return errors.New("daemon: -trmnl-refresh must be at least 1m")
	}
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	// stop cleanly on Ctrl-C or a service manager's SIGTERM
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// a snapshot of the whole database each day: the habit log, the
	// settings and the display pairings exist nowhere else
	if *backupKeep > 0 {
		dir := *backupDir
		if dir == "" {
			dir = filepath.Join(filepath.Dir(cfg.DBPath), "backups")
		}
		go keepBackups(ctx, db, dir, *backupKeep)
	}

	// phone alerts through Home Assistant, when it's configured
	var alertEngine *alerts.Engine
	if ha := alerts.NewHomeAssistant(os.Getenv("WH_HA_URL"), os.Getenv("WH_HA_TOKEN"), os.Getenv("WH_HA_NOTIFY")); ha != nil {
		alertEngine = alerts.NewEngine(db, ha, os.Getenv("WH_ALERT_LINK"))
		if alertEngine.QuietFrom, alertEngine.QuietTo, err = alerts.ParseQuiet(envOr("WH_ALERT_QUIET", alerts.DefaultQuiet)); err != nil {
			return err
		}
		if v := os.Getenv("WH_ALERT_MAX_PER_DAY"); v != "" {
			if alertEngine.MaxPerDay, err = strconv.Atoi(v); err != nil || alertEngine.MaxPerDay < 1 {
				return fmt.Errorf("WH_ALERT_MAX_PER_DAY: want a whole number of at least 1, got %q", v)
			}
		}
		if alertEngine.Settings, err = alerts.LoadSettings(os.Getenv); err != nil {
			return err
		}
		log.Printf("alerts: on, to %s: %s; %s", ha.Target(), alertEngine.Limits(), strings.Join(alertEngine.Settings.Describe(), "; "))
	}

	status := &server.Tracker{}
	status.Update(func(s *server.Status) { s.Started, s.Interval = time.Now(), *every })
	// what the database last stored, so a restart doesn't show "never" while
	// the first fetch (a long back-fill) is still running
	if t, err := db.LastFetched(ctx); err != nil {
		log.Printf("reading the last fetch time: %v", err)
	} else if !t.IsZero() {
		status.Update(func(s *server.Status) { s.LastSuccess, s.Restored = t, true })
	}
	oauth := oura.NewOAuth(cfg.ClientID, cfg.ClientSecret)

	// Until a fetch succeeds after a start, fetches cover -startup-days
	// rather than -days: when a new version begins storing something Oura
	// was already sending, its history fills in on deploy, with no manual
	// backfill. A failed first try (no network yet at boot) doesn't use up
	// the backfill. After that, each fetch refreshes only the recent days.
	backfilled := false
	var fetchMu sync.Mutex // one fetch at a time: the poll's and a webhook's
	// fetchOnce fetches and reports whether it worked, so the schedule can
	// retry a failure soon (fetch.Every) rather than after a whole interval
	fetchOnce := func(ctx context.Context) error {
		fetchMu.Lock()
		defer fetchMu.Unlock()
		window := *days
		if !backfilled {
			window = max(*days, *startupDays)
		}
		start := time.Now()
		status.Update(func(s *server.Status) {
			s.LastAttempt, s.Fetching, s.Backfill = start, true, !backfilled
		})
		defer status.Update(func(s *server.Status) { s.Fetching, s.Backfill = false, false })
		// a routine fetch takes seconds; the back-fill after a start pulls a
		// year, heart rate included, and gets longer
		timeout := 5 * time.Minute
		if !backfilled {
			timeout = 20 * time.Minute
		}
		ctx, cancel := context.WithTimeout(ctx, timeout)
		defer cancel()

		r, err := func() (fetch.Result, error) {
			tokens, err := oura.NewFileTokenSource(cfg.TokenPath, oauth)
			if err != nil {
				return fetch.Result{}, err
			}
			return fetch.Run(ctx, oura.New(tokens), db, window)
		}()
		if err != nil {
			log.Printf("fetch failed after %s: %v", time.Since(start).Round(time.Millisecond), err)
			status.Update(func(s *server.Status) { s.LastError = err.Error() })
			return err
		}
		backfilled = true
		log.Printf("fetched %s in %s", r, time.Since(start).Round(time.Millisecond))
		status.Update(func(s *server.Status) {
			s.LastSuccess, s.LastResult, s.LastError, s.Restored = time.Now(), r.String(), "", false
		})
		return nil
	}

	srv := &http.Server{
		Addr: *listen,
		Handler: server.New(db, status, server.Config{TRMNL: server.TRMNLConfig{
			Bits: *trmnlBits, Format: *trmnlFormat, Refresh: *trmnlRefresh,
		}, Alerts: alertEngine, LogAll: *logAll}),
		ReadHeaderTimeout: 10 * time.Second,
		WriteTimeout:      60 * time.Second,
	}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.ListenAndServe() }()
	log.Printf("wolfe-health %s", version.Get().String(time.Local))
	log.Printf("serving on %s, fetching every %s", *listen, *every)

	// alerts: checked after each fetch, and every five minutes for what
	// the clock decides (the morning summary, quiet hours ending, data
	// going stale)
	checkAlerts := func(ctx context.Context) {
		if alertEngine == nil {
			return
		}
		st := status.Get()
		if _, err := alertEngine.Check(ctx, alerts.FetchState{LastSuccess: st.LastSuccess, LastError: st.LastError}); err != nil {
			log.Printf("alerts: %v", err)
		}
	}
	fetchAndAlert := func(ctx context.Context) error {
		err := fetchOnce(ctx)
		checkAlerts(ctx)
		return err
	}
	if alertEngine != nil {
		go func() {
			t := time.NewTicker(5 * time.Minute)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					checkAlerts(ctx)
				}
			}
		}()
	}

	go fetch.Every(ctx, *every, fetch.RetryDelays, fetchAndAlert, func(t time.Time) {
		status.Update(func(s *server.Status) { s.NextFetch = t })
	})
	if *webhookListen != "" {
		stopHooks, err := startWebhooks(ctx, db, cfg, status, *webhookListen, *webhookURL, fetchAndAlert)
		if err != nil {
			return err
		}
		defer stopHooks()
	}

	select {
	case err := <-serveErr:
		return fmt.Errorf("daemon: %w", err) // e.g. the port is taken
	case <-ctx.Done():
	}
	log.Printf("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return srv.Shutdown(shutCtx)
}

// webhooksCmd lists the app's Oura webhook subscriptions, or deletes one
// or all of them: `webhooks`, `webhooks delete ID`, `webhooks delete all`.
// The daemon makes and renews them itself; this is for looking, and for
// tidying up after a change of URL.
func webhooksCmd(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	c := webhook.NewClient(cfg.ClientID, cfg.ClientSecret)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	switch {
	case len(args) == 0 || args[0] == "list":
		subs, err := c.List(ctx)
		if err != nil {
			return err
		}
		if len(subs) == 0 {
			fmt.Println("no webhook subscriptions")
		}
		for _, s := range subs {
			fmt.Printf("%s  %-6s %-24s expires %s  %s\n", s.ID, s.EventType, s.DataType, s.ExpiresAt().Local().Format("Jan 2 2006"), s.CallbackURL)
		}
		return nil
	case args[0] == "delete" && len(args) == 2:
		ids := []string{args[1]}
		if args[1] == "all" {
			subs, err := c.List(ctx)
			if err != nil {
				return err
			}
			ids = ids[:0]
			for _, s := range subs {
				ids = append(ids, s.ID)
			}
		}
		for _, id := range ids {
			if err := c.Delete(ctx, id); err != nil {
				return err
			}
		}
		fmt.Printf("deleted %d subscription(s)\n", len(ids))
		return nil
	}
	return errors.New("usage: webhooks [list] | webhooks delete ID | webhooks delete all")
}

// webhookTokenKey is the setting holding the webhook verification token,
// made once and kept, so subscriptions made under it stay valid.
const webhookTokenKey = "webhook_verification_token"

// startWebhooks serves Oura's webhook on listen, a port of its own, and
// fetches soon after each notification: events within half a minute of
// the first are gathered into one fetch, as a sync sends several at once.
// With url set it subscribes that URL to every data type stored and checks
// the subscriptions daily, renewing any that lapse within a week. It
// returns a function that stops the listener.
func startWebhooks(ctx context.Context, db *store.DB, cfg *config.Config, status *server.Tracker,
	listen, url string, fetchOnce func(context.Context) error) (func(), error) {

	token, ok, err := db.Setting(ctx, webhookTokenKey)
	if err != nil {
		return nil, err
	}
	if !ok || token == "" {
		b := make([]byte, 24)
		if _, err := rand.Read(b); err != nil {
			return nil, err
		}
		token = hex.EncodeToString(b)
		if err := db.SetSetting(ctx, webhookTokenKey, token); err != nil {
			return nil, err
		}
	}

	events := make(chan webhook.Event, 64)
	h := webhook.Handler(token, cfg.ClientSecret, func(e webhook.Event) {
		status.Update(func(s *server.Status) { s.LastWebhook, s.LastWebhookEvent = time.Now(), e.EventType+" "+e.DataType })
		select {
		case events <- e:
		default: // a fetch is already coming
		}
	})
	srv := &http.Server{Addr: listen, Handler: h, ReadHeaderTimeout: 10 * time.Second, WriteTimeout: 10 * time.Second}
	ln, err := net.Listen("tcp", listen)
	if err != nil {
		return nil, fmt.Errorf("daemon: webhook listener: %w", err)
	}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Printf("webhook listener: %v", err)
		}
	}()
	log.Printf("webhook listening on %s", listen)

	// a notification starts a fetch half a minute later, gathering the
	// rest of the sync's notifications into it
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-events:
				log.Printf("webhook: %s %s; fetching shortly", e.EventType, e.DataType)
				timer := time.NewTimer(30 * time.Second)
			gather:
				for {
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-events:
					case <-timer.C:
						break gather
					}
				}
				fetchOnce(ctx) // a failure shows in the status; the schedule retries it
			}
		}
	}()

	if url != "" {
		subs := webhook.NewClient(cfg.ClientID, cfg.ClientSecret)
		go func() {
			failures := 0
			for {
				// Oura checks the URL while subscribing, so the listener is
				// up first. Checked daily; a failure is retried on the
				// fetch's schedule (a minute, two, five, ... up to an hour),
				// so a boot with the clock not yet set recovers in minutes
				c, cancel := context.WithTimeout(ctx, 5*time.Minute)
				r, err := subs.Ensure(c, url, token, 7*24*time.Hour)
				cancel()
				wait := 24 * time.Hour
				msg := r.String()
				if err != nil {
					msg = "subscriptions failed: " + err.Error()
				}
				if err != nil || len(r.Errors) > 0 {
					failures++
					wait = fetch.RetryDelay(fetch.RetryDelays, failures, wait)
					msg += fmt.Sprintf("; retrying in %s", wait)
				} else {
					failures = 0
				}
				log.Printf("webhook: %s", msg)
				status.Update(func(s *server.Status) { s.Webhooks = msg })
				select {
				case <-ctx.Done():
					return
				case <-time.After(wait):
				}
			}
		}()
	} else {
		status.Update(func(s *server.Status) { s.Webhooks = "listening, but no -webhook-url to subscribe" })
	}
	return func() {
		c, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		srv.Shutdown(c)
	}, nil
}

// envOr returns the environment variable's value, or def if it is unset.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envDefaults reads flags' defaults from WH_ settings, collecting what it
// can't read so the daemon reports every bad setting at once.
type envDefaults struct{ errs []error }

func (e *envDefaults) duration(key string, def time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		e.errs = append(e.errs, fmt.Errorf("%s: want a duration like 6h or 30m, got %q", key, v))
		return def
	}
	return d
}

func (e *envDefaults) int(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		e.errs = append(e.errs, fmt.Errorf("%s: want a whole number, got %q", key, v))
		return def
	}
	return n
}

func (e *envDefaults) err() error { return errors.Join(e.errs...) }

// applyTempAlertMin sets the early-warning banner's temperature threshold
// from WH_TEMP_ALERT_MIN, in °C above baseline (default 0.3).
func applyTempAlertMin() error {
	v := os.Getenv("WH_TEMP_ALERT_MIN")
	if v == "" {
		return nil
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f < 0 || f > 3 {
		return fmt.Errorf("WH_TEMP_ALERT_MIN: want °C above baseline between 0 and 3, like 0.3 or 0.5; got %q", v)
	}
	dashboard.MinAlertDeviation = f
	return nil
}

// envBool reads a yes/no setting: 1, true or yes is on; unset, empty, 0,
// false, no or anything else is off.
func envBool(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	}
	return false
}

// openBrowser tries to open url in the default browser. Failure is fine: the
// URL has already been printed for the user to open by hand.
func openBrowser(url string) {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	_ = exec.Command(name, url).Start()
}

func run() error {
	// Get a config struct with credentials, token path, dbpath, and days
	cfg, err := config.Load()

	if err != nil {
		return err
	}

	// ctx used to carry cancellation and deadlines across functions
	ctx := context.Background()

	// load the saved token; it refreshes itself (and rewrites the file) when
	// it nears expiry, so this is the only auth step a normal run needs
	tokens, err := oura.NewFileTokenSource(cfg.TokenPath, oura.NewOAuth(cfg.ClientID, cfg.ClientSecret))
	if errors.Is(err, oura.ErrNoToken) {
		return fmt.Errorf("%w — run `make auth` once to log in", err)
	}
	if err != nil {
		return err
	}

	db, err := store.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()

	r, err := fetch.Run(ctx, oura.New(tokens), db, cfg.Days)
	if err != nil {
		return err
	}
	fmt.Printf("fetched %s\n\n", r)

	// prints whats now in db and returns nil (no error)
	return report(ctx, db)
}

// report prints what is now in the database, as a check that the round trip
// through SQLite actually worked.
func report(ctx context.Context, db *store.DB) error {
	for _, m := range []string{metricSleep, metricReadiness, metricStress, metricActivity, metricSteps} {
		n, err := db.Count(ctx, m)
		if err != nil {
			return err
		}
		recent, err := db.Recent(ctx, m, 1)
		if err != nil {
			return err
		}
		latest := "no data"
		if len(recent) > 0 {
			latest = fmt.Sprintf("%s = %.0f", recent[0].Day, recent[0].Value)
		}
		fmt.Printf("%-10s %3d days stored, latest %s\n", m, n, latest)
	}
	return nil
}

// habitsCmd saves or restores the habit log, the one part of the database
// that can't be fetched again:
//
//	wolfe-health habits export [FILE]   every log as CSV (default: to stdout)
//	wolfe-health habits import FILE     add a CSV's logs, skipping ones already stored
//
// The file is the index page's "Download every log", so a copy saved there
// restores here.
func habitsCmd(args []string) error {
	usage := errors.New("usage: wolfe-health habits export [FILE] | habits import FILE")
	if len(args) == 0 {
		return usage
	}
	db, err := store.Open(config.DBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	ctx := context.Background()
	switch {
	case args[0] == "export" && len(args) <= 2:
		logs, err := db.HabitLogsBetween(ctx, time.Unix(0, 0), time.Now().AddDate(0, 0, 1), time.Local)
		if err != nil {
			return err
		}
		out := os.Stdout
		if len(args) == 2 {
			f, err := os.Create(args[1])
			if err != nil {
				return err
			}
			defer f.Close()
			out = f
		}
		if err := store.WriteHabitCSV(out, logs); err != nil {
			return err
		}
		if len(args) == 2 {
			fmt.Fprintf(os.Stderr, "wrote %d logs to %s\n", len(logs), args[1])
		}
		return nil
	case args[0] == "import" && len(args) == 2:
		f, err := os.Open(args[1])
		if err != nil {
			return err
		}
		defer f.Close()
		logs, err := store.ReadHabitCSV(f)
		if err != nil {
			return err
		}
		added, err := db.ImportHabitLogs(ctx, logs)
		if err != nil {
			return err
		}
		fmt.Printf("imported %d of %d logs; %d were already here\n", added, len(logs), len(logs)-added)
		return nil
	}
	return usage
}

// keepBackups takes the day's database snapshot now and checks hourly for a
// new day, keeping the newest keep. A failure is logged and retried on the
// next check; serving goes on regardless.
func keepBackups(ctx context.Context, db *store.DB, dir string, keep int) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if path, made, err := db.Backup(ctx, dir, time.Now(), keep); err != nil {
			log.Printf("backup: %v", err)
		} else if made {
			log.Printf("backup: wrote %s", path)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// resetCmd drops one of Oura's tables, the fix when its columns change:
// stop the daemon, reset the table, and start it again; the start's
// year-long fetch refills it. It refuses the tables holding data made here.
func resetCmd(args []string) error {
	if len(args) != 1 {
		return fmt.Errorf("usage: wolfe-health reset TABLE (one of %s)", strings.Join(store.CacheTables, ", "))
	}
	db, err := store.Open(config.DBPath())
	if err != nil {
		return err
	}
	defer db.Close()
	if err := db.DropCacheTable(context.Background(), args[0]); err != nil {
		return err
	}
	fmt.Printf("dropped %s; the next start's fetch refills it\n", args[0])
	return nil
}
