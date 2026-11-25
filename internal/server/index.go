package server

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/cameron-wolfe/wolfe-health/internal/chart"
	"github.com/cameron-wolfe/wolfe-health/internal/dashboard"
	"github.com/cameron-wolfe/wolfe-health/internal/habits"
	"github.com/cameron-wolfe/wolfe-health/internal/version"
)

// card is one image on the index page.
type card struct {
	Title string
	Note  string // a short line under the title
	Src   string // image URL, including the page's size parameters
}

// Download is Src with ?download=1, so the browser saves rather than shows.
func (c card) Download() string { return withParam(c.Src, "download", "1") }

type indexData struct {
	Page    string // which page: today, charts, display, habits, alerts, settings
	Status  Status
	Now     time.Time
	W, H    int
	Days    int
	Date    string    // the date of interest, YYYY-MM-DD; "" for today
	AsOf    time.Time // that date, for the page's wording
	MaxDate string    // today, the latest date the picker allows
	Today   []card
	Key     []card // Today's page: the few charts most worth a glance
	Trends  []card
	More    []card
	Nights  []card
	Sizes   []string
	Healthy bool
	Mark    template.HTML // the wolf mark, inline SVG
	Favicon template.URL  // the mark as a data: URL for the tab icon

	Devices   []deviceRow // paired TRMNL displays
	DeviceLog []DeviceLog // their recent reports, newest last
	ServerURL string      // what to enter as the display's custom server
	Refresh   string      // how often displays update
	Depth     string      // what the displays are sent: "4-gray PNG"

	Sched    []schedRow     // the schedule form: saved dayparts, then blank rows
	Options  []screenOption // screens the schedule can use
	Hold     int
	Monthly  bool   // last month's report on the 1st
	Weekly   bool   // last week's review on Sundays
	Alert    string // the early-warning banner's message, or ""
	AlertWhy string // why it's hidden, when it is
	Showing  string // "Daily brief until 1:00 PM"
	HeldOn   string // the displays' date of interest, YYYY-MM-DD; "" for live
	SchedMsg string // "Schedule saved"
	SchedErr string

	HabitToken string     // the habit log's token, for the shortcut
	Habits     []habitRow // this week's logs (since Sunday), newest first
	HabitNames []string   // the catalog, for the page's wording
	HabitMax   string     // now, the latest time a log can be set to

	Version string // the running build: commit, uncommitted changes, when built

	AlertsTo     string     // where phone alerts go; "" when they're off
	AlertsLimits string     // their quiet hours and daily cap, worded
	AlertsRules  []string   // each alert that's on, worded
	AlertsSent   []alertRow // the latest sent, newest first
	AlertsMsg    string     // after Send test: "sent" or "failed"

	Settings []Setting // every setting in effect, for the Settings page
}

// Setting is one setting in effect, for the Settings page.
type Setting struct {
	Group string // "Daemon", "Displays", "Phone alerts", …
	Name  string // the .env name: "WH_FETCH_EVERY"
	Value string // as in effect; a secret's is masked
	Set   bool   // set in .env or on the command line, rather than the default
}

// pages are the panel's pages, in the tab bar's order: path, tab label.
var pages = []struct{ Name, Path, Label string }{
	{"today", "/", "Today"},
	{"charts", "/charts", "Charts"},
	{"display", "/display", "Display"},
	{"habits", "/habits", "Habits"},
	{"alerts", "/alerts", "Alerts"},
	{"settings", "/settings", "Settings"},
}

// schedRow is one daypart in the schedule form.
type schedRow struct {
	I       int
	Start   string
	Screens [maxScreensPerPart]string
	Active  bool
}

// Slots indexes a row's screen dropdowns for the template.
func (r schedRow) Slots() []int { return []int{0, 1, 2, 3} }

// deviceRow is a paired display, formatted for the page.
type deviceRow struct {
	ID, Model, Firmware, Battery, Signal, Seen, Screen string
}

// markHTML and favicon are the Wolfe Health mark for the page: white on the
// black masthead, and black on white for the browser tab.
var (
	markHTML = template.HTML(chart.WolfMarkSVG(30, "#fff", "#111"))
	favicon  = template.URL("data:image/svg+xml," + url.PathEscape(chart.WolfMarkSVG(64, "#111", "#fff")))
)

// page serves one of the panel's pages (pages): the same data, a
// different part of it shown, under a shared masthead and tab bar.
func (s *server) page(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { s.index(w, r, name) }
}

// index builds the panel's data and renders the page named: Today, the
// charts (each with a link to open it alone and one to download it; ?w=&h=
// resize every image, ?days= sets the trends' window, ?date= draws them as
// of a day), the display, habits, alerts and settings.
func (s *server) index(w http.ResponseWriter, r *http.Request, page string) {
	width, height, err := size(r)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	days, err := intParam(r, "days", defaultLineDays, 1, 3650)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	// a date of interest: every chart on the page is drawn as of that day
	base, _, dated, err := s.asOf(r.URL.Query().Get("date"))
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	sz := url.Values{}
	if width != defaultW || height != defaultH {
		sz.Set("w", strconv.Itoa(width))
		sz.Set("h", strconv.Itoa(height))
	}
	if dated {
		sz.Set("date", base.Format("2006-01-02"))
	}
	src := func(path string, extra url.Values) string {
		q := url.Values{}
		for k, v := range sz {
			q[k] = v
		}
		for k, v := range extra {
			if v == nil {
				delete(q, k) // a link that opts out of a page-wide setting
				continue
			}
			q[k] = v
		}
		if len(q) == 0 {
			return path
		}
		return path + "?" + q.Encode()
	}

	d := indexData{Page: page, Settings: s.settings, Status: s.status.Get(), Now: s.now(), W: width, H: height, Days: days,
		Sizes: []string{"800x480", "640x384", "960x540", "1200x825"},
		AsOf:  base, MaxDate: s.today().Format("2006-01-02")}
	if dated {
		d.Date = base.Format("2006-01-02")
	}
	d.Healthy = d.Status.LastError == ""
	d.Mark, d.Favicon = markHTML, favicon

	d.Today = []card{
		{Title: "Daily brief", Note: "readiness as a gauge, last night as a moon, today's rings and yesterday's figures, as a console", Src: src("/daily.png", nil)},
		{Title: "Daily digest", Note: "the calmer layout: readiness large, four range strips, last night as one stage bar", Src: src("/daily-digest.png", nil)},
		{Title: "Last night", Note: "sleep stages, movement, heart rate, HRV", Src: src("/night.png", nil)},
		{Title: "Vital signs", Note: "last night's body signals and scores against your normal ranges, as a console", Src: src("/vital-signs.png", nil)},
		{Title: "WolfeSense · Signals", Note: "each of last night's readings against your normal range, as a radar", Src: src("/deviation-radar.png", nil)},
		{Title: "WolfeSense · Sleep", Note: fmt.Sprintf("last night's seven parts of the sleep score against your %d-night average", days),
			Src: src("/sleep-breakdown.png", url.Values{"days": {strconv.Itoa(days)}})},
		{Title: "WolfePhase", Note: "last night as a moon in phase, its sleep stages wrapped round as rings", Src: src("/wolfephase.png", nil)},
		{Title: "WolfeSense · Readiness", Note: fmt.Sprintf("today's parts of the readiness score against your %d-day average", days),
			Src: src("/readiness-breakdown.png", url.Values{"days": {strconv.Itoa(days)}})},
		{Title: "Heart rate today", Note: "today's readings against your usual for each time of day", Src: src("/heart-today.png", nil)},
		{Title: "Heart rate yesterday", Note: "the whole day against your usual", Src: src("/heart-yesterday.png", nil)},
	}
	d.Trends = []card{{
		Title: "Trends",
		Note:  fmt.Sprintf("sleep score, readiness and steps, last %d days", max(days, 14)),
		Src:   src("/trends.png", url.Values{"days": {strconv.Itoa(max(days, 14))}}),
	}}
	thisMonth := base
	lastMonth := time.Date(thisMonth.Year(), thisMonth.Month()-1, 1, 0, 0, 0, 0, time.UTC)
	d.More = []card{
		{Title: "Stress this month", Note: thisMonth.Format("January 2006") + " · high-stress time per day",
			Src: src("/stress-month.png", nil)},
		{Title: "Stress last month", Note: lastMonth.Format("January 2006"),
			Src: src("/stress-month.png", url.Values{"month": {lastMonth.Format("2006-01")}})},
		{Title: "WolfePhase · Month", Note: thisMonth.Format("January 2006") + " · each night as a moon, lit from bedtime to waking",
			Src: src("/wolfephase-month.png", nil)},
		{Title: "WolfePhase · Last month", Note: lastMonth.Format("January 2006"),
			Src: src("/wolfephase-month.png", url.Values{"month": {lastMonth.Format("2006-01")}})},
		{Title: "Scores this month", Note: thisMonth.Format("January 2006") + " · sleep, readiness and activity rings per day",
			Src: src("/score-month.png", nil)},
		{Title: "Scores last month", Note: lastMonth.Format("January 2006"),
			Src: src("/score-month.png", url.Values{"month": {lastMonth.Format("2006-01")}})},
		{Title: "Sleep schedule", Note: "bedtime and wake each night against your best bedtime window, last 30 nights",
			Src: src("/sleep-schedule.png", nil)},
		{Title: "Bedtime payoff", Note: "each night's bedtime against the sleep score that followed, and your best window, last 6 months",
			Src: src("/bedtime-payoff.png", nil)},
		{Title: "Sleep stages", Note: fmt.Sprintf("time in each stage per night, last %d nights", min(days, 120)),
			Src: src("/sleep-stages.png", url.Values{"days": {strconv.Itoa(min(days, 120))}})},
		{Title: "Last week", Note: "week in review", Src: src("/week-review.png", nil)},
		{Title: "WolfeSense · Week", Note: "last week's averages against your last 8 weeks: outward is better", Src: src("/week-radar.png", nil)},
		{Title: "Habits · Week", Note: "this week's logged habits, each day with the night after it",
			Src: src("/habits-week.png", url.Values{"week": {base.Format("2006-01-02")}})},
		{Title: "Habit effects", Note: "the nights after each habit against the nights without, last 90 days", Src: src("/habit-effects.png", nil)},
		{Title: "This week so far", Note: "week in review",
			Src: src("/week-review.png", url.Values{"week": {base.Format("2006-01-02")}})},
		{Title: "This month", Note: thisMonth.Format("January 2006") + " in review",
			Src: src("/month-report.png", nil)},
		{Title: "Last month", Note: lastMonth.Format("January 2006") + " in review",
			Src: src("/month-report.png", url.Values{"month": {lastMonth.Format("2006-01")}})},
		{Title: "Body signals", Note: "HRV, lowest heart rate and temperature against your normal range",
			Src: src("/body-signals.png", nil)},
		{Title: "Early warning", Note: "temperature, lowest heart rate, HRV and breathing against your normal, last 21 nights: several up together can come before illness",
			Src: src("/early-warning.png", nil)},
		{Title: "Fitness and sleep", Note: "VO2 max, cardio age, resting heart rate and sleep score, last 12 months",
			Src: src("/fitness.png", nil)},
		{Title: "Oxygen tide", Note: "blood oxygen trend against your normal range, last 90 nights",
			Src: src("/oxygen.png", nil)},
		{Title: "Weekly rhythm", Note: "averages by day of week, last 365 days",
			Src: src("/weekly-rhythm.png", nil)},
	}

	// Today's charts: the few most worth a glance, from the lists above
	for _, want := range []string{"Daily brief", "Early warning", "Last night", "WolfeSense · Readiness"} {
		for _, c := range append(append([]card{}, d.Today...), d.More...) {
			if c.Title == want {
				d.Key = append(d.Key, c)
			}
		}
	}

	devices, err := s.db.Devices(r.Context())
	if err != nil {
		serverError(w, err)
		return
	}
	for _, dv := range devices {
		row := deviceRow{ID: dv.FriendlyID, Model: dv.Model, Firmware: dv.Firmware,
			Battery: "—", Signal: "—", Seen: ago(d.Now, dv.LastSeen), Screen: dv.LastScreen}
		if dv.Battery != nil {
			row.Battery = fmt.Sprintf("%.2f V", *dv.Battery)
		}
		if dv.RSSI != nil {
			row.Signal = fmt.Sprintf("%d dBm", *dv.RSSI)
		}
		if row.Screen == "" {
			row.Screen = "—"
		}
		d.Devices = append(d.Devices, row)
	}
	sc := s.schedule(r.Context())
	sl := sc.at(d.Now)
	d.Options, d.Hold, d.Monthly, d.Weekly = scheduleScreens, sc.HoldMinutes, !sc.SkipMonthly, !sc.SkipWeekly
	if d.HabitToken, err = s.habitToken(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	if d.Habits, err = s.recentHabits(r.Context()); err != nil {
		serverError(w, err)
		return
	}
	for _, h := range habits.Catalog {
		d.HabitNames = append(d.HabitNames, h.Name)
	}
	d.HabitMax = s.now().Add(5 * time.Minute).Format("2006-01-02T15:04")
	if err := s.alertsData(r.Context(), &d); err != nil {
		serverError(w, err)
		return
	}
	d.AlertsMsg = r.URL.Query().Get("alerts")
	d.Version = version.Get().String(s.now().Location())
	d.Showing = screenTitle(sl.Screen) + " until " + sl.Until.Format("3:04 PM")
	d.HeldOn = sc.AsOf
	if sc.AsOf != "" {
		held, _ := time.Parse("2006-01-02", sc.AsOf)
		d.Showing += ", as of " + held.Format("Monday, Jan 2")
	}
	for i, p := range sc.Dayparts {
		row := schedRow{I: i, Start: p.Start, Active: i == sl.Part}
		copy(row.Screens[:], p.Screens)
		d.Sched = append(d.Sched, row)
	}
	for i := len(sc.Dayparts); i < len(sc.Dayparts)+2; i++ {
		d.Sched = append(d.Sched, schedRow{I: i}) // blank rows to add dayparts
	}
	switch r.URL.Query().Get("schedule") {
	case "saved":
		d.SchedMsg = "Schedule saved."
	case "reset":
		d.SchedMsg = "Schedule reset to the default."
	}
	d.SchedErr = r.URL.Query().Get("schedule_error")

	if d.Alert, d.AlertWhy, err = dashboard.AlertStatus(r.Context(), s.db, base); err != nil {
		serverError(w, err)
		return
	}
	d.DeviceLog = s.trmnl.recentLogs(5)
	d.ServerURL = baseURL(r)
	d.Refresh = s.trmnlCfg.Refresh.String()
	d.Depth = "4-gray PNG"
	if s.trmnlCfg.Format == "bmp" {
		d.Depth = "1-bit BMP"
	} else if s.trmnlCfg.Bits == 1 {
		d.Depth = "1-bit PNG"
	}

	// the nights up to the date of interest; each link names its own night,
	// so it doesn't carry the page's date
	nights, err := s.db.SleepDays(r.Context(), base.Format("2006-01-02"), 14)
	if err != nil {
		serverError(w, err)
		return
	}
	for _, day := range nights {
		t, _ := time.Parse("2006-01-02", day)
		d.Nights = append(d.Nights, card{
			Title: "Woke " + t.Format("Mon Jan 2"),
			Src:   src("/night.png", url.Values{"day": {day}, "date": nil}),
		})
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := indexTmpl.Execute(w, d); err != nil {
		serverError(w, err)
	}
}

// withParam adds one query parameter to a URL path that may already have some.
func withParam(u, k, v string) string {
	sep := "?"
	for _, c := range u {
		if c == '?' {
			sep = "&"
			break
		}
	}
	return u + sep + url.QueryEscape(k) + "=" + url.QueryEscape(v)
}

func ago(now, t time.Time) string {
	if t.IsZero() {
		return "never"
	}
	d := now.Sub(t).Round(time.Minute)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh %dm ago", int(d.Hours()), int(d.Minutes())%60)
	}
	return fmt.Sprintf("%d days ago", int(d.Hours()/24))
}

var indexTmpl = template.Must(template.New("index").Funcs(template.FuncMap{
	"ago":   ago,
	"clock": func(t time.Time) string { return t.Format("Mon 3:04 PM") },
	"pages": func() any { return pages },
}).Parse(`<!doctype html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1, viewport-fit=cover">
<meta name="apple-mobile-web-app-capable" content="yes">
<meta name="mobile-web-app-capable" content="yes">
<meta name="apple-mobile-web-app-status-bar-style" content="black">
<meta name="apple-mobile-web-app-title" content="Wolfe Health">
<meta name="theme-color" content="#111111">
<link rel="manifest" href="/app/manifest.webmanifest">
<link rel="apple-touch-icon" href="/emulator/icon.png">
<title>Wolfe Health{{range pages}}{{if and (eq .Name $.Page) (ne .Name "today")}} · {{.Label}}{{end}}{{end}}</title>
<link rel="icon" href="{{.Favicon}}">
<style>
  :root { --ink:#111; --muted:#666; --line:#ddd; --bg:#fafafa; --card:#fff; --bad:#a00; --tab:#2a2a2a; }
  @media (prefers-color-scheme: dark) {
    :root { --ink:#eee; --muted:#9a9a9a; --line:#333; --bg:#151515; --card:#1e1e1e; --bad:#f77; }
  }
  * { box-sizing: border-box; }
  body { margin:0; background:var(--bg); color:var(--ink);
         font:15px/1.4 system-ui, -apple-system, Segoe UI, sans-serif; }
  main { padding:16px max(16px, env(safe-area-inset-right)) 32px max(16px, env(safe-area-inset-left));
         max-width:1400px; margin:0 auto; }
  /* the masthead: the same black band, mark and wordmark as on every image,
     with the panel's tabs under it */
  header { position:sticky; top:0; z-index:5; background:#111; color:#fff;
           padding-top:env(safe-area-inset-top); }
  .masthead { display:flex; align-items:center; gap:12px; padding:10px max(16px, env(safe-area-inset-left)) 6px; }
  .masthead svg { display:block; flex:none; }
  .wordmark { font-weight:700; font-size:16px; letter-spacing:.24em; }
  .masthead .when { margin-left:auto; font-size:13px; letter-spacing:.08em; opacity:.8; }
  nav.tabs { display:flex; gap:4px; overflow-x:auto; padding:0 max(12px, env(safe-area-inset-left)) 8px;
             scrollbar-width:none; -webkit-overflow-scrolling:touch; }
  nav.tabs::-webkit-scrollbar { display:none; }
  nav.tabs a { color:#cfcfcf; text-decoration:none; padding:6px 14px; border-radius:999px;
               white-space:nowrap; font-size:14px; }
  nav.tabs a:hover { background:var(--tab); color:#fff; }
  nav.tabs a.on { background:#fff; color:#111; font-weight:600; }
  h1 { font-size:20px; margin:4px 0 12px; }
  h2 { font-size:17px; margin:28px 0 10px; }
  h3 { font-size:15px; margin:22px 0 6px; }
  .muted { color:var(--muted); }
  .bad { color:var(--bad); }
  .ok { color:var(--muted); }
  .err { font-weight:600; }
  .status { margin:0 0 4px; }
  .panel { background:var(--card); border:1px solid var(--line); border-radius:10px; padding:12px 14px; margin:0 0 16px; }
  .actions { display:flex; flex-wrap:wrap; gap:8px; margin:10px 0 0; }
  .actions a, .actions button { display:inline-block; padding:6px 12px; border:1px solid var(--line);
             border-radius:8px; background:var(--card); color:inherit; text-decoration:none; font:inherit; font-size:14px; cursor:pointer; }
  form { display:flex; flex-wrap:wrap; gap:8px 16px; align-items:center; margin:12px 0 0; }
  form label { color:var(--muted); }
  input, select, button { font:inherit; }
  input[type=number] { width:5.5em; }
  .scroll { overflow-x:auto; -webkit-overflow-scrolling:touch; }
  .habits td { padding:2px 8px 2px 0; vertical-align:middle; }
  .habits th.habit-day { text-align:left; font-weight:600; padding:14px 0 4px; border-bottom:1px solid var(--line); }
  .habits tr:first-child th.habit-day { padding-top:4px; }
  .habit-edit { display:flex; gap:6px; align-items:center; flex-wrap:wrap; margin:0; }
  .habit-count { width:4.5em; }
  .grid { display:grid; grid-template-columns:repeat(auto-fill, minmax(min(100%, 420px), 1fr)); gap:16px; }
  .card { background:var(--card); border:1px solid var(--line); border-radius:8px; padding:12px; }
  .card h3 { font-size:15px; margin:0; }
  .card .note { margin:0 0 8px; font-size:13px; }
  /* charts are drawn white for e-ink; keep them white in dark mode too */
  .card img { display:block; width:100%; height:auto; background:#fff; border:1px solid var(--line); }
  .links { margin-top:8px; font-size:13px; display:flex; gap:16px; }
  a { color:inherit; }
  ul.nights { list-style:none; padding:0; margin:0; display:grid;
              grid-template-columns:repeat(auto-fill, minmax(200px, 1fr)); gap:6px 16px; }
  code { font-size:13px; overflow-wrap:anywhere; }
  table.devices { border-collapse:collapse; font-size:14px; }
  table.devices th, table.devices td { text-align:left; padding:4px 14px 4px 0; border-bottom:1px solid var(--line); vertical-align:top; }
  table.devices th { color:var(--muted); font-weight:600; }
  table.settings td.set { font-weight:600; }
  ul.logs { margin:4px 0 0; padding-left:18px; font-size:13px; }
  .alert { border:2px solid var(--ink); padding:8px 12px; margin:0 0 12px; border-radius:6px; }
  form.sched select, form.sched input[type=time] { font-size:14px; }
  form.sched input[type=number] { width:4.5em; }
  tr.active td { background:var(--line); }
  @media (max-width: 600px) {
    .masthead .when { display:none; }
    h2 { margin-top:22px; }
  }
</style>
</head>
<body>
<header>
  <div class="masthead">
    {{.Mark}}
    <span class="wordmark">WOLFE HEALTH</span>
    <span class="when">{{clock .Now}}</span>
  </div>
  <nav class="tabs">{{range pages}}<a href="{{.Path}}"{{if eq .Name $.Page}} class="on" aria-current="page"{{end}}>{{.Label}}</a>{{end}}</nav>
</header>
<main>
{{if .Alert}}<p class="alert"><b>Body signals:</b> {{.Alert}} · <a href="/body-signals.png">see the chart</a></p>{{end}}

{{if eq .Page "today"}}
<div class="panel">
{{template "fetch" .}}
<div class="actions">
  <a href="/emulator?show=daily">Open the display</a>
  <a href="/charts">All charts</a>
  {{if .AlertsTo}}<form method="post" action="/alerts/summary" style="margin:0"><button type="submit">Send morning summary</button></form>{{end}}
</div>
</div>
<div class="grid">
{{range .Key}}{{template "card" .}}{{end}}
</div>
{{end}}

{{if eq .Page "charts"}}
<h1>Charts</h1>
<form method="get">
  <label>Size
    <select name="size" onchange="var p=this.value.split('x');this.form.w.value=p[0];this.form.h.value=p[1];">
      <option value="">custom</option>
      {{range .Sizes}}<option value="{{.}}">{{.}}</option>{{end}}
    </select>
  </label>
  <label>w <input type="number" name="w" value="{{.W}}" min="1" max="4000"></label>
  <label>h <input type="number" name="h" value="{{.H}}" min="1" max="4000"></label>
  <label>trend days <input type="number" name="days" value="{{.Days}}" min="1" max="3650"></label>
  <label>as of <input type="date" name="date" value="{{.Date}}" max="{{.MaxDate}}"></label>
  <button type="submit">Apply</button>
</form>
{{if .Date}}<p class="alert">Showing every chart as of <b>{{.AsOf.Format "Monday, January 2, 2006"}}</b>:
each screen as it would have looked that day. <a href="?w={{.W}}&amp;h={{.H}}&amp;days={{.Days}}">Back to today</a></p>{{end}}

<h2>Today</h2>
<div class="grid">
{{range .Today}}{{template "card" .}}{{end}}
</div>

<h2>Stress and sleep detail</h2>
<div class="grid">
{{range .More}}{{template "card" .}}{{end}}
</div>

<h2>Trends</h2>
<div class="grid">
{{range .Trends}}{{template "card" .}}{{end}}
</div>

<h2>Recent nights</h2>
{{if .Nights}}
<ul class="nights">
{{range .Nights}}<li>{{.Title}} — <a href="{{.Src}}">open</a> · <a href="{{.Download}}">download</a></li>{{end}}
</ul>
{{else}}<p class="muted">No sleep periods stored yet.</p>{{end}}
{{end}}

{{if eq .Page "display"}}
<h1>Display</h1>
<div class="panel">
<p class="status">Showing now: <b>{{.Showing}}</b></p>
<div class="actions"><a href="/emulator">Open the display emulator</a><a href="/current.png">/current.png</a></div>
</div>

<h2 id="schedule">Schedule</h2>
{{if .SchedMsg}}<p class="ok">{{.SchedMsg}}</p>{{end}}
{{if .SchedErr}}<p class="bad">Not saved: {{.SchedErr}}</p>{{end}}
<form method="post" action="/schedule" class="sched">
<div class="scroll"><table class="devices">
<tr><th>From</th><th colspan="4">Screens, in rotation order</th></tr>
{{$opts := .Options}}
{{range $row := .Sched}}<tr{{if $row.Active}} class="active"{{end}}>
  <td><input type="time" name="start_{{$row.I}}" value="{{$row.Start}}"></td>
  {{range $j := $row.Slots}}<td><select name="screen_{{$row.I}}_{{$j}}">
    <option value="">—</option>
    {{range $opts}}<option value="{{.Name}}"{{if eq .Name (index $row.Screens $j)}} selected{{end}}>{{.Title}}</option>{{end}}
  </select></td>{{end}}
</tr>
{{end}}</table></div>
<p>Each daypart runs until the next one starts; screens rotate every
<input type="number" name="hold" value="{{.Hold}}" min="1" max="720" step="1"> minutes.
Clear a row's time to remove it; fill a blank row to add one.</p>
<p><label><input type="checkbox" name="monthly" value="1"{{if .Monthly}} checked{{end}}>
On the 1st of each month, show last month's report first in each daypart that rotates</label><br>
<label><input type="checkbox" name="weekly" value="1"{{if .Weekly}} checked{{end}}>
On Sundays, show last week's review first in each daypart that rotates</label></p>
<p><label>Show the displays as of <input type="date" name="as_of" value="{{.HeldOn}}" max="{{.MaxDate}}"></label>
<span class="muted">— every screen drawn as it looked that day; leave blank (or clear it) for today, live</span></p>
<p><button type="submit">Save schedule</button>
<button type="submit" name="reset" value="1" formnovalidate>Reset to default</button>
<button type="button" id="sched-clear" title="Empty every daypart, to build a schedule from scratch; nothing changes until you save">Clear all dayparts</button></p>
</form>
<script>
// Clear all dayparts: empties every row's time and screens in the form,
// leaving the hold time and checkboxes; nothing is saved until Save.
document.getElementById("sched-clear").addEventListener("click", () => {
  const form = document.querySelector("form.sched");
  form.querySelectorAll('input[type="time"]').forEach(el => { el.value = ""; });
  form.querySelectorAll("select").forEach(el => { el.value = ""; });
  form.querySelectorAll("tr.active").forEach(el => el.classList.remove("active"));
  form.querySelector('input[type="time"]').focus();
});
</script>

<h2>Displays</h2>
{{if .Devices}}
<div class="scroll"><table class="devices">
<tr><th>Device</th><th>Model</th><th>Firmware</th><th>Battery</th><th>Signal</th><th>Last update</th><th>Showing</th></tr>
{{range .Devices}}<tr><td>{{.ID}}</td><td>{{.Model}}</td><td>{{.Firmware}}</td><td>{{.Battery}}</td><td>{{.Signal}}</td><td>{{.Seen}}</td><td>{{.Screen}}</td></tr>
{{end}}</table></div>
{{if .DeviceLog}}<p class="muted">Recent device logs:</p>
<ul class="logs">{{range .DeviceLog}}<li><code>{{clock .At}}</code> {{.Message}}</li>{{end}}</ul>{{end}}
{{else}}
<p>No display paired yet. To connect a TRMNL: hold its button for 5 seconds, join the
<b>TRMNL</b> Wi-Fi network from your phone, choose <b>Custom server</b>, and enter
<code>{{.ServerURL}}</code>, then your home Wi-Fi.</p>
{{end}}
<p class="muted">Displays wake every {{.Refresh}} (sooner when the schedule changes screen) and get
the {{.Depth}}. Custom server address: <code>{{.ServerURL}}</code></p>

<h3 id="warning">Early-warning banner</h3>
<p>{{if .Alert}}<b>Showing</b> on every screen: {{.Alert}}{{else}}Hidden: {{.AlertWhy}}.{{end}}
<a href="/daily.png?alert=preview">Preview the banner</a> (add <code>?alert=preview</code> to any image; displays never get it).</p>

<h3>For other displays</h3>
<p class="muted">Each image is its own URL, rendered fresh on request, and answers
<code>If-None-Match</code> with <code>304</code> when unchanged. Parameters:
<code>w</code>, <code>h</code> (all images), <code>days</code> (trends, stages, breakdown),
<code>day=YYYY-MM-DD</code> (nights, the wake-up day; heart rate, the day), <code>month=YYYY-MM</code> (stress and score calendars),
<code>bits=1</code> or <code>bits=2</code> (e-ink: black and white, or four grays),
<code>format=bmp</code> (1-bit BMP), <code>download=1</code>. Fetch status as JSON: <a href="/status">/status</a>.</p>
{{end}}

{{if eq .Page "habits"}}
<h1 id="habits">Habits</h1>
<p class="muted">What you had or did, logged with a Siri shortcut: say "log a habit" and pick from
{{range $i, $n := .HabitNames}}{{if $i}}, {{end}}{{$n}}{{end}}.
One caffeine log is a double shot of espresso, one alcohol log one drink; meals, sweets and workouts are judged late or not against your bedtime.
The shortcut sends <code>POST /log</code> with <code>{"habit": "caffeine"}</code> and this token in an
<code>X-Log-Token</code> header; the steps to build it are in the README's "Habit log" section.</p>
<p class="muted">Your habit logs exist only on this server: Oura can't send them back.
<a href="/log/export.csv">Download every log (CSV)</a> to keep a copy elsewhere; the server
also snapshots its whole database daily.</p>
<p>Token: <code id="habit-token">{{.HabitToken}}</code>
<button type="button" id="habit-copy">Copy</button></p>
<script>
// the clipboard API needs https; on this http page, select the token and
// copy the selection instead, which works everywhere
document.getElementById("habit-copy").addEventListener("click", function () {
  const el = document.getElementById("habit-token");
  const done = () => { this.textContent = "Copied"; };
  const fallback = () => {
    const r = document.createRange(); r.selectNodeContents(el);
    const sel = window.getSelection(); sel.removeAllRanges(); sel.addRange(r);
    try { document.execCommand("copy") && done(); } catch (e) {}
  };
  if (navigator.clipboard && window.isSecureContext) {
    navigator.clipboard.writeText(el.textContent.trim()).then(done, fallback);
  } else {
    fallback();
  }
});
</script>
{{if .Habits}}<div class="scroll"><table class="habits">
{{range .Habits}}{{if .DayStart}}<tr><th colspan="5" class="habit-day">{{.Day}}</th></tr>
{{end}}<tr><td>{{.What}}</td>
{{/* the time and count sit in their own cells, joined to the Save form by
     form=, so Save lines up whether or not a habit has a count */}}<td><input type="datetime-local" name="at" value="{{.At}}" max="{{$.HabitMax}}" aria-label="When" form="habit-edit-{{.ID}}"></td>
<td>{{if .Counted}}<input type="number" name="count" value="{{.Count}}" min="0.5" max="10" step="0.5" aria-label="How many" class="habit-count" form="habit-edit-{{.ID}}">{{end}}</td>
<td><form method="post" action="/log/edit" class="habit-edit" id="habit-edit-{{.ID}}"><input type="hidden" name="id" value="{{.ID}}">
<button type="submit" title="Save this log's time{{if .Counted}} and count{{end}}">Save</button></form></td>
<td><form method="post" action="/log/delete" class="habit-edit"><input type="hidden" name="id" value="{{.ID}}"><button type="submit" title="Remove this log">Remove</button></form></td></tr>
{{end}}</table></div>
<p class="muted">This week's logs, Sunday on; older ones stay in the database and the CSV. Change a time or count and press Save; times are your local time.</p>{{else}}<p class="muted">Nothing logged since Sunday.</p>{{end}}
{{end}}

{{if eq .Page "alerts"}}
<h1 id="alerts">Phone alerts</h1>
{{if .AlertsTo}}<p class="muted">Sent to {{.AlertsTo}}, once each, {{.AlertsLimits}}
{{if .AlertsRules}}for {{range $i, $r := .AlertsRules}}{{if $i}}; {{end}}{{$r}}{{end}}.{{else}}but every alert is turned off (<code>WH_ALERTS_OFF</code>).{{end}}
Each is set in <code>.env</code>: see <a href="/settings">Settings</a>.</p>
<form method="post"><button type="submit" formaction="/alerts/test">Send test</button>
<button type="submit" formaction="/alerts/summary" title="Send this morning's summary to the phone now, as it stands; the 8 AM one still goes out">Send morning summary now</button>
{{if eq .AlertsMsg "test-sent"}}<span class="ok">Test sent: check the phone.</span>{{end}}
{{if eq .AlertsMsg "summary-sent"}}<span class="ok">Morning summary sent: check the phone.</span>{{end}}
{{if or (eq .AlertsMsg "test-failed") (eq .AlertsMsg "summary-failed")}}<span class="err">It didn't send; the server log says why.</span>{{end}}</form>
<h2>Sent</h2>
{{if .AlertsSent}}<div class="scroll"><table class="devices"><tr><th>Sent</th><th>Alert</th></tr>
{{range .AlertsSent}}<tr><td>{{.At}}</td><td><b>{{.Title}}</b><br><span class="muted">{{.Message}}</span></td></tr>
{{end}}</table></div>{{else}}<p class="muted">None sent yet.</p>{{end}}
{{else}}<p class="muted">Off. To get alerts on a phone through Home Assistant, set <code>WH_HA_URL</code>,
<code>WH_HA_TOKEN</code> and <code>WH_HA_NOTIFY</code> (see the README's "Phone alerts").</p>{{end}}
{{end}}

{{if eq .Page "settings"}}
<h1>Settings</h1>
<div class="panel">
{{template "fetch" .}}
</div>
<p class="muted">Every setting in effect. <b>Bold</b> ones are set in <code>.env</code> or on the
command line; the rest are defaults. To change one, edit <code>.env</code> on the server and restart
the service; the README's Configuration says what each does. Secrets are never shown.</p>
{{if .Settings}}<div class="scroll"><table class="devices settings">
<tr><th>Group</th><th>Setting</th><th>In effect</th></tr>
{{range .Settings}}<tr><td class="muted">{{.Group}}</td><td><code>{{.Name}}</code></td><td{{if .Set}} class="set"{{end}}>{{.Value}}</td></tr>
{{end}}</table></div>{{else}}<p class="muted">Not known here: the settings are listed when the daemon serves this page.</p>{{end}}
{{end}}
</main>
</body>
</html>
{{define "fetch"}}<p class="status">
  Last fetch:
  {{if .Status.Fetching}}<b>fetching now</b><span class="muted">{{if .Status.Backfill}}, back-filling after a start{{end}}, since {{clock .Status.LastAttempt}}{{if not .Status.LastSuccess.IsZero}} · last success {{ago .Now .Status.LastSuccess}}{{end}}</span>
  {{else if .Status.LastAttempt.IsZero}}{{if .Status.LastSuccess.IsZero}}<span class="muted">not yet run</span>{{else}}{{ago .Now .Status.LastSuccess}} <span class="muted">— before this start</span>{{end}}
  {{else if .Healthy}}{{ago .Now .Status.LastSuccess}} <span class="muted">— {{if .Status.Restored}}before this start{{else}}{{.Status.LastResult}}{{end}}</span>
  {{else}}<span class="bad">failed {{ago .Now .Status.LastAttempt}}: {{.Status.LastError}}</span>
    {{if not .Status.LastSuccess.IsZero}}<span class="muted">(last success {{ago .Now .Status.LastSuccess}})</span>{{end}}
  {{end}}
</p>
{{if not .Status.NextFetch.IsZero}}<p class="status muted">Next fetch {{clock .Status.NextFetch}}</p>{{end}}
<p class="status muted">Running {{.Version}}</p>
{{if .Status.Webhooks}}<p class="status muted">Oura webhooks: {{.Status.Webhooks}}{{if not .Status.LastWebhook.IsZero}} · last notification {{ago .Now .Status.LastWebhook}} ({{.Status.LastWebhookEvent}}){{else}} · no notification yet{{end}}</p>{{end}}{{end}}
{{define "card"}}<div class="card">
  <h3>{{.Title}}</h3>
  <p class="note muted">{{.Note}}</p>
  <a href="{{.Src}}"><img src="{{.Src}}" alt="{{.Title}} chart" loading="lazy"></a>
  <div class="links"><a href="{{.Src}}">open</a><a href="{{.Download}}">download</a></div>
</div>{{end}}`))
