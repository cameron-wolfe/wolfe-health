# wolfe-health

Health wearable data display — pulls personal Oura Ring data, with the eventual
goal of rendering it to an e-ink dashboard on a Raspberry Pi.

Built in stages. **Collecting, charting and serving are working:** a daemon
fetches from Oura (on webhooks, with a poll to catch up) and serves every
screen over HTTP to a TRMNL e-ink display, a browser, or the display
emulator.

**New here?** [docs/quickstart.md](docs/quickstart.md) gets you from a fresh
clone to your own Oura data on charts in about ten minutes.

## Contents

- [Roadmap](#roadmap)
- [Layout](#layout)
- [Commands](#commands)
- [Setup](#setup)
  - [Turning on more data after registering](#turning-on-more-data-after-registering)
  - [Scheduled fetching](#scheduled-fetching)
  - [How authentication works](#how-authentication-works)
  - [Logging in on the Pi](#logging-in-on-the-pi)
  - [Logging in by hand](#logging-in-by-hand)
  - [Configuration](#configuration)
  - [Coming from the old setup](#coming-from-the-old-setup)
- [Charts](#charts)
- [Serving the charts: the daemon](#serving-the-charts-the-daemon)
  - [Backups](#backups)
  - [Oura webhooks](#oura-webhooks)
  - [Phone alerts](#phone-alerts)
  - [Habit log](#habit-log)
  - ["How did I sleep" shortcut](#how-did-i-sleep-shortcut)
  - ["Check Wolfe Health" shortcut](#check-wolfe-health-shortcut)
  - [Building and changing the shortcuts](#building-and-changing-the-shortcuts)
  - [TRMNL e-ink display](#trmnl-e-ink-display)
  - [Detail views](#detail-views)
  - [Deploying to a server](#deploying-to-a-server)
  - [Logs](#logs)
- [How it works](#how-it-works)
  - [Fetching](#fetching)
  - [Drawing a screen for a request](#drawing-a-screen-for-a-request)
  - [Alerts](#alerts)
  - [Where a change goes](#where-a-change-goes)
- [Inspecting the data](#inspecting-the-data)
- [Development](#development)
  - [Releases](#releases)
- [Known gaps](#known-gaps)

## Roadmap

1. **Collect and store** ✓. Fetch from the Oura API, persist to SQLite.
2. **Chart** ✓. Read from the database, render screens to images.
3. **Display** ✓. Serve a TRMNL panel on a daily schedule.
4. **Interact** ✓. Step from a screen into its detail views: a tap on a
   tablet or phone, the button on a TRMNL. See [Detail views](#detail-views).
5. **Notify** ✓ ← we are here. Phone alerts through an existing Home
   Assistant: the morning summary, an early warning when several signals
   drift toward illness, and the data going stale. See
   [Phone alerts](#phone-alerts).

Still open: trying the TRMNL's button stepping on a real device.

The database is the interface between collecting and charting.

## Layout

<details>
<summary>Show</summary>

```
cmd/wolfe-health/   default: fetch once; `auth`: log in; `chart`: render a screen to a file;
                    `daemon`: fetch and serve; `webhooks`: manage Oura's webhook subscriptions
internal/
  config/           environment-based settings
  oura/             Oura API v2 client, OAuth2 login and token refresh
  store/            SQLite persistence
  fetch/            one fetch of every collection into the database
  habits/           the habit log's catalog, shared by the server and the charts
  dashboard/        decides what each screen shows; reads store, feeds chart
  chart/            grayscale rendering for e-ink: one file per screen, plus shared parts
  server/           HTTP: the screens, index page, display schedule, TRMNL protocol, habit log
  webhook/          Oura webhook receiver and subscriptions
docs/               deploying to a Linux server; the habit log's Siri shortcut
scripts/            scheduled fetch (launchd on macOS, cron on the Pi), the systemd
                    service, and shortcut.py, which builds the Siri shortcuts
```

</details>

## Commands

<details>
<summary>Show</summary>

Everything is one program, `wolfe-health` (built to `bin/` by `make build`):

| Command | What it does | When you'd use it | Details |
|---|---|---|---|
| `wolfe-health` | fetches from Oura once into the database, then exits | a one-off fetch, or a scheduled one where no daemon runs (`make run`) | [Setup](#setup), [Scheduled fetching](#scheduled-fetching) |
| `wolfe-health auth` | logs in to Oura: opens the browser, catches the callback, saves the token | once at setup, or after the token is revoked (`make auth`) | [How authentication works](#how-authentication-works) |
| `wolfe-health daemon` | runs the server: fetches on Oura's webhooks and on a schedule, serves every screen, the index page, the emulator, the TRMNL protocol and the habit log | what a deployment runs all the time (`make daemon` locally) | [Serving the charts](#serving-the-charts-the-daemon) |
| `wolfe-health version` | prints which build this is: its git commit, whether it had uncommitted changes, and when it was built | checking what's deployed (`ssh <host> /opt/wolfe-health/wolfe-health version`) | [Deploying to a server](#deploying-to-a-server) |
| `wolfe-health webhooks` | lists Oura webhook subscriptions; `webhooks delete ID` or `webhooks delete all` removes them | checking or resetting webhooks; the daemon creates and renews them itself | [Oura webhooks](#oura-webhooks) |
| `wolfe-health habits` | `habits export [FILE]` saves every habit log as CSV; `habits import FILE` restores one, adding only what's missing | keeping a copy of the habit log, or restoring it | [Backups](#backups) |
| `wolfe-health reset TABLE` | drops one of Oura's tables so the next start refetches it; refuses the tables holding data made here | after an update changes how a table is stored | [Backups](#backups) |
| `wolfe-health chart` | draws one screen from the database into a PNG, identical to the daemon's image | previewing a change, another panel size, a past date, or archiving; needs only the database (`make daily`, `make night`, `make chart`) | [Charts](#charts) |

Each takes `-h` for its flags. All of them find the database through
`WH_DB_PATH` (default `wolfe-health.db` in the current folder).

</details>

## Setup

<details>
<summary>Show</summary>

Oura retired personal access tokens in December 2025, so the API is reached
through OAuth2: you register wolfe-health as an *application*, then approve it
once in the browser. That is three one-time steps.

**1. Register an application** at
<https://developer.ouraring.com/applications>. The fields that matter:

| Field | Value |
|---|---|
| Redirect URI | `http://localhost:8080/callback` — exactly this. `127.0.0.1` is rejected if it is not the registered one. |
| Scopes | all of them. Ticked here, a scope is marked Required on the approval page; the login asks for every scope either way |
| Website / privacy / terms | the repo URL is fine for a personal app |

Unapproved applications are limited to 10 users, so a personal one never
needs Oura's review.

**2. Put the client ID and secret in `.env`:**

```bash
cp .env.example .env    # then fill in OURA_CLIENT_ID and OURA_CLIENT_SECRET
```

**3. Log in:**

```bash
make auth
```

This opens Oura's approval page, which lists every permission the login
asks for with a checkbox each. **Tick them all before approving:** one left
unticked is never granted, and its data is skipped on every fetch. The
command then waits for the redirect on `localhost:8080` and saves the
resulting tokens to `oura-token.json`, printing the scopes granted:

```
saved token to oura-token.json (scope "extapi:email extapi:personal extapi:daily extapi:heartrate extapi:workout extapi:tag extapi:session extapi:spo2 extapi:ring_configuration extapi:stress extapi:heart_health", expires …)
```

All eleven should be there. Then:

```bash
make run
```

That builds, loads `.env`, fetches the trailing 30 days, and prints what landed:

```
fetching 2026-10-20 to 2026-11-19
stored 151 samples in wolfe-health.db

stored 30 nights of sleep contributors

sleep       30 days stored, latest 2026-11-19 = 66
readiness   30 days stored, latest 2026-11-19 = 67
stress      31 days stored, latest 2026-11-19 = 0
activity    30 days stored, latest 2026-11-18 = 58
steps       30 days stored, latest 2026-11-18 = 6765
```

Re-running is safe — days are upserted, so a re-fetch overwrites rather than
duplicating. That matters because Oura revises recent days as the ring syncs.

For a first run, backfill your whole history; Oura returns only what exists,
and pages through it automatically:

```bash
WH_DAYS=3650 make run
```

</details>

### Turning on more data after registering

<details>
<summary>Show</summary>

A login that granted only some scopes (early versions of this README asked
for just **Daily** and **Stress**, or a permission was left unticked on the
approval page) gets only that data: blood oxygen, heart rate, workouts, VO2
max, cardiovascular age, tags and the rest are skipped. A token keeps the
scopes granted when you approved it, so adding more means logging in again:

1. **Tick every scope** on your application at
   <https://developer.ouraring.com/applications> and save, so none are
   optional on the approval page.
2. **Log in again** with `make auth`, on the machine that fetches (on a
   server, see [Logging in on the Pi](#logging-in-on-the-pi)), and **tick
   every permission** on the approval page it opens before approving. The
   new `saved token to oura-token.json (scope "…")` line lists the scopes
   granted: check all eleven are there.

Then restart the daemon (or run `WH_DAYS=3650 make run`), so the new
collections back-fill their history: the daemon's routine fetches cover
only the last week. A collection whose scope is missing shows up in the
fetch log as `skipped <collection>: …`, while the rest carry on.

</details>

### Scheduled fetching

<details>
<summary>Show</summary>

`scripts/fetch.sh` loads `.env` and runs the built binary, fetching the last
7 days (enough to catch Oura's revisions to recent nights). It runs the binary
in `bin/` rather than `make run`, because schedulers have no Go on their PATH —
so **rebuild after pulling changes** (`make build`, or `make schedule` on the
Mac, which rebuilds and reinstalls).

**On the Mac**, a launchd agent runs it every 3 hours, and on wake if the Mac
was asleep at the time:

```bash
make schedule     # build, install the agent, and run once now
make logs         # recent output (~/Library/Logs/wolfe-health.log)
make unschedule   # remove it
```

**On the Pi**, use cron:

```bash
crontab -e
# then add:
0 */3 * * * $HOME/wolfe-health/scripts/fetch.sh >> $HOME/wolfe-health/fetch.log 2>&1
```

Schedule only the machine that holds `oura-token.json`: when you move the
token to the Pi, run `make unschedule` on the Mac.

</details>

### How authentication works

<details>
<summary>Show</summary>

`.env` and `oura-token.json` hold different things, and both are gitignored:

| File | Holds | Changes? |
|---|---|---|
| `.env` | the application's client ID and secret | never, unless you re-register |
| `oura-token.json` | your access token (30 days) and refresh token | rewritten by the app on every refresh |

Every `make run` checks the access token's expiry and refreshes it a day before
it runs out. The client ID and secret are needed for that, which is why `.env`
stays required after login. If the API rejects a token early (revoked, say),
the run refreshes once and retries.

Oura's refresh tokens are **single-use**: each refresh returns a new one and
kills the old. The app writes the new token file before using the new token,
via a temp file and an atomic rename, so a crash never leaves you holding only
a spent refresh token. Two consequences:

- **Don't copy `oura-token.json` and use it in two places.** Whichever copy
  refreshes first invalidates the other. Log in separately on each machine, or
  keep one machine as the only one that fetches.
- **If refreshing fails** (token file deleted, app access revoked in the Oura
  app, refresh token expired after long disuse), run `make auth` again.

</details>

### Logging in on the Pi

<details>
<summary>Show</summary>

The Pi has no browser, so forward the callback port from your laptop and open
the printed URL there:

```bash
ssh -L 8080:localhost:8080 pi
cd wolfe-health && make auth    # open the printed URL in your laptop's browser
```

The browser redirect to `localhost:8080` then travels through the tunnel to the
Pi. Alternatively, log in on the laptop and move (not copy — see above) the
token file to the Pi.

</details>

### Logging in by hand

<details>
<summary>Show</summary>

Useful for debugging, and what `make auth` does under the hood:

```bash
set -a; source .env; set +a
open "https://cloud.ouraring.com/oauth/authorize?response_type=code&client_id=$OURA_CLIENT_ID&redirect_uri=http://localhost:8080/callback&scope=email%20personal%20daily%20heartrate%20workout%20tag%20session%20spo2%20ring_configuration%20stress%20heart_health"
# approve; the browser lands on a "can't connect" page - copy code=... from its URL
curl -s -X POST https://api.ouraring.com/oauth/token \
  -u "$OURA_CLIENT_ID:$OURA_CLIENT_SECRET" \
  -d grant_type=authorization_code -d code=PASTE_CODE \
  -d redirect_uri=http://localhost:8080/callback -o oura-token.json
chmod 600 oura-token.json
```

The code expires within about a minute, and Oura answers an expired one with
an unhelpful `400 invalid_request` — if you see that, just repeat faster.
`make auth` avoids the race by catching the redirect itself.

</details>

### Configuration

<details>
<summary>Show</summary>

Every setting is an environment variable, kept in `.env` (gitignored; copy
`.env.example`). On a server, that's `.env` in the install directory, which
the service reads at start: edit it, then restart (`sudo systemctl restart
wolfe-health`, or a `make deploy`). A value the daemon can't read stops it
at start with a message naming the setting, rather than being ignored. The
daemon's flags (`wolfe-health daemon -h`) take the same settings, and a flag
given on the command line wins: if the service's command line has
`-every 6h`, `WH_FETCH_EVERY` is ignored until that flag is removed
(`install-service.sh --args`, or the unit file).

Only the first two are required.

**Oura and storage**

| Variable | Default | Meaning |
|---|---|---|
| `OURA_CLIENT_ID` | — | OAuth application client ID (required) |
| `OURA_CLIENT_SECRET` | — | OAuth application client secret (required) |
| `WH_TOKEN_PATH` | `oura-token.json` | where the OAuth tokens are kept |
| `WH_DB_PATH` | `wolfe-health.db` | the SQLite database |
| `WH_DAYS` | `30` | days of history `make run` fetches |
| `WH_AUTH_PORT` | `8080` | the login callback's port; register the matching address on the Oura app |

**The daemon** ([Serving the charts](#serving-the-charts-the-daemon))

| Variable | Flag | Default | Meaning |
|---|---|---|---|
| `WH_LISTEN` | `-listen` | `:8090` | address to serve on |
| `WH_FETCH_EVERY` | `-every` | `1h` | time between scheduled fetches (a Go duration: `6h`, `30m`) |
| `WH_FETCH_DAYS` | `-days` | `7` | days of history each scheduled fetch refreshes |
| `WH_STARTUP_DAYS` | `-startup-days` | `365` | days the first fetch after a start covers |
| `WH_BACKUP_DIR` | `-backup-dir` | `backups/` beside the database | where the daily snapshots go ([Backups](#backups)) |
| `WH_BACKUP_KEEP` | `-backup-keep` | `14` | daily snapshots to keep; `0` turns them off |
| `WH_LOG_REQUESTS` | `-log-requests` | off | `1` logs every request, not just changes, errors and slow ones ([Logs](#logs)) |
| `WH_WEBHOOK_LISTEN` | `-webhook-listen` | off | Oura's webhook port, e.g. `:8091` ([Oura webhooks](#oura-webhooks)) |
| `WH_WEBHOOK_URL` | `-webhook-url` | — | the public HTTPS URL that reaches it |

**Displays** ([TRMNL e-ink display](#trmnl-e-ink-display))

| Variable | Flag | Default | Meaning |
|---|---|---|---|
| `WH_TRMNL_BITS` | `-trmnl-bits` | `2` | `2` for four grays, `1` for black and white (older firmware) |
| `WH_TRMNL_FORMAT` | `-trmnl-format` | `png` | `bmp` for a 1-bit BMP |
| `WH_TRMNL_REFRESH` | `-trmnl-refresh` | `15m` | how long a TRMNL sleeps between updates |
| `WH_TEMP_ALERT_MIN` | — | `0.3` | °C above baseline a night's temperature needs (as well as being above your normal range) to raise the early-warning banner on the screens |

**Phone alerts** ([Phone alerts](#phone-alerts))

| Variable | Default | Meaning |
|---|---|---|
| `WH_HA_URL` | — | Home Assistant's address; with the next two, turns alerts on |
| `WH_HA_TOKEN` | — | a Home Assistant long-lived access token |
| `WH_HA_NOTIFY` | — | the phone's notify action, e.g. `mobile_app_your_iphone` |
| `WH_ALERT_LINK` | — | this server as the phone reaches it, for tapping an alert |
| `WH_ALERT_QUIET` | `22-7` | quiet hours, local: `22-7`, `22:00-07:00`, or `off` |
| `WH_ALERT_MAX_PER_DAY` | `10` | the most alerts a day; the test buttons don't count |
| `WH_ALERT_SUMMARY_AT` | `8` | the morning summary's hour (`8` or `08:00`), or `off`; it waits up to three hours for last night to sync |
| `WH_ALERTS_OFF` | — | alerts to turn off, by name: `summary`, `illness`, `fetch`, `sync`, e.g. `sync,fetch` |
| `WH_ALERT_ILLNESS` | `2/2` | an early warning's threshold as signals/nights: `3/2` is three or more signals up, two nights running (signals 1–4, nights 1–14) |
| `WH_ALERT_STALE` | `13h/18h` | how long without a fetch working / without a ring sync before saying so (each an hour or more) |

The daemon logs the alert settings in effect at start, and the index page's
Phone alerts section words them.

</details>

### Coming from the old setup

<details>
<summary>Show</summary>

If you ran this before November 2026 with a personal access token:

- `OURA_TOKEN` is gone; delete it from `.env`. Oura no longer accepts personal
  tokens.
- Add `OURA_CLIENT_ID` and `OURA_CLIENT_SECRET` (step 1 above) and run
  `make auth` once.
- The existing `wolfe-health.db` needs no changes.

</details>

## Charts

<details>
<summary>Show</summary>

Every screen, by name, its URL on the daemon, and its `chart -view`:

| Screen | URL | `-view` | Shows | Options |
|---|---|---|---|---|
| Daily brief | `/daily.png` | `daily` | readiness as a radar of its parts, last night as a moon with the sleep score, today's rings, yesterday's figures | |
| Daily digest | `/daily-digest.png` | `digest` | the brief's calmer first layout: readiness large, four range strips, last night as one stage bar | |
| Last night | `/night.png` | `night` | a hypnogram of one night: stages, movement, heart rate, HRV | `day` |
| WolfePhase | `/wolfephase.png` | `phase` | a night as a moon in phase on a 12-hour face, its stages wrapped round as rings | `day` |
| WolfePhase · Month | `/wolfephase-month.png` | `phase-month` | each night of a month as a small moon, lit from bedtime to waking | `month` |
| Vital signs | `/vital-signs.png` | `vitals` | last night's body signals and scores as a console of gauges against your normal ranges | |
| WolfeSense · Signals | `/deviation-radar.png` | `deviation` | each of last night's readings as its distance from your normal | |
| WolfeSense · Sleep | `/sleep-breakdown.png` | `breakdown` | the sleep score's seven parts against their average | `days` (30) |
| WolfeSense · Readiness | `/readiness-breakdown.png` | `readiness` | the readiness score's parts against their average | `days` (30) |
| WolfeSense · Week | `/week-radar.png` | `week-radar` | a week's averages against your last 8 weeks | `week` |
| Week in review | `/week-review.png` | `week` | each day's score rings, the best and toughest nights, the week against the last | `week` (last complete) |
| Habits · Week | `/habits-week.png` | `habits-week` | each day's logged habits and the night after | `week` (this week) |
| Habit effects | `/habit-effects.png` | `habit-effects` | nights after each habit against nights without | `days` (90) |
| Monthly report | `/month-report.png` | `month` | a month's averages against the last, its nights, its standout days | `month` |
| Stress calendar | `/stress-month.png` | `stress-month` | each day as a dial of high stress and recovery | `month` |
| Score calendar | `/score-month.png` | `score-month` | each day's three score rings | `month` |
| Sleep stages | `/sleep-stages.png` | `stages` | time in each stage per night, stacked | `days` (30, to 120) |
| Sleep schedule | `/sleep-schedule.png` | `schedule` | bedtime and waking each night against your best bedtime window | `days` (30, 7–60) |
| Bedtime payoff | `/bedtime-payoff.png` | `payoff` | bedtime against the next sleep score, and the window that pays off | `days` (180, 30–365) |
| Weekly rhythm | `/weekly-rhythm.png` | `rhythm` | sleep, readiness and stress by day of week against your average | `days` (365) |
| Body signals | `/body-signals.png` | `signals` | nightly HRV, lowest heart rate and temperature against your normal range | `days` (60) |
| Early warning | `/early-warning.png` | `early-warning` | temperature, lowest heart rate, HRV and breathing against your normal, turned so up is toward illness; nights with 2+ up shaded, and what was logged before each | `days` (21, 7–60) |
| Fitness and sleep | `/fitness.png` | `fitness` | VO2 max, cardio age, resting heart rate and sleep score, long term | `days` (365, 60–730) |
| Oxygen tide | `/oxygen.png` | `oxygen` | blood oxygen against your normal range, and nights with more dips than usual | `days` (90, 7–365) |
| Heart rate today | `/heart-today.png` | `heart` | today's heart rate against your usual for each time of day | `day` |
| Heart rate yesterday | `/heart-yesterday.png` | `heart-yesterday` | the same for yesterday, the whole day | |
| Trends | `/trends.png` | `trends` | sleep score, readiness and steps with 7-day averages and your typical range | `days` (60, 14–365) |

Options are the same on both: `day=YYYY-MM-DD` (a night's wake-up day, or
the day for heart rate), `week=YYYY-MM-DD` (any day in the week),
`month=YYYY-MM`, `days=N` (default and range in brackets), plus `date=` and
`alert=preview` on every screen (below). On the command line each is a flag
of the same name:

```bash
make daily                                         # charts/daily.png
make night                                         # charts/night.png
make chart                                         # charts/trends.png
./bin/wolfe-health chart -view phase-month -month 2026-10
./bin/wolfe-health chart -view night -day 2026-11-18
./bin/wolfe-health chart -view week -date 2026-11-12   # as it looked that day
./bin/wolfe-health chart -view daily -width 640 -height 384
```

The command line draws each screen through the server's own code
(`server.Render`), so a file and the daemon's image are identical. It reads
only the database, so it works offline and needs no Oura credentials.
Output is an 800×480 grayscale PNG by default (sized for a common 7.5" e-ink
panel; `-width`/`-height` for yours) in `charts/<view>.png`, which is
gitignored.

**Reading a screen.** Every screen keys its own marks: what a tick, a
pointer, a hatch or an inverted value means is drawn and labeled on the
screen itself. How each one is built, and why, is in the doc comment on its
function in `internal/chart` (`BriefView`, `MoonView`, `RadarView`, ...),
and how its figures are chosen and worded is on its builder in
`internal/dashboard`.

**The early-warning banner.** When last night's temperature was above your
normal range (the body-signals range) *and* at least +0.3 °C above baseline
(`WH_TEMP_ALERT_MIN`),
every screen — on the panel and in the browser — gets a bordered banner under
the masthead: *"Temperature +0.8 °C, above your normal range for 2 nights ·
lowest heart rate also high · HRV below normal"*, counting consecutive nights
and mentioning heart rate and HRV only when they agree that night. It hides
whenever last night was normal, and when the latest reading is more than a
day old, so a warning never lingers. The index page shows it too, and while
it's hidden says why ("last night +0.1 °C, within your normal range"). To see
the banner before it has reason to appear, add `?alert=preview` to any image
URL (or `-alert-preview` to `chart`); it's marked "Preview", and displays
never get it. It describes; it doesn't diagnose.

**Masthead and footer.** Every image carries the Wolfe Health masthead (the
wolf mark and wordmark, with the screen's name on the right) and ends in a
one-line footer: the ring's battery and when it last synced on the left
(*"ring 64% · synced 2:28 PM"*), and when the server last updated from Oura
on the right (*"updated 2:40 PM"*). Oura only has the ring's data after the
phone app syncs it, so a sync time well behind the update time means the
phone, not the server, is the hold-up. Below 20% the battery reads
*"charge soon"* in bold.

</details>

## Serving the charts: the daemon

<details>
<summary>Show</summary>

`wolfe-health daemon` is the deployed form: one long-running process that
fetches from Oura on startup and every hour, and serves every chart over
HTTP the whole time. Open `http://<host>:8090` in any browser on the network
for a page showing each chart, with links to open or download it.

```bash
make daemon                                 # run it here: http://localhost:8090
./bin/wolfe-health daemon -listen :8090 -every 1h -days 7
```

| Endpoint | Returns |
|---|---|
| `/` | index page: every chart, fetch status, size and trend-window controls, the display schedule, the habit log |
| `/<screen>.png` | each screen in the [Charts](#charts) table; `/line/…` redirects to `/trends.png`, `/wolfemoon.png` (WolfePhase's first name) still works |
| `/current.png` | whatever the display schedule shows right now |
| `/emulator` | a page emulating the display: the current screen as a TRMNL gets it, refreshed every minute; tap it for [detail views](#detail-views) |
| `/status` | fetch status as JSON, and under `version` which build is running |
| `/summary.txt` | the morning's figures as a few sentences to read aloud, for the "How did I sleep" shortcut; `?date=` for another day |
| `/health.txt` | `ok`, or a line per problem with the data (no fetch working for 13 h, the ring not syncing for 18 h; `WH_ALERT_STALE`), for the "Check Wolfe Health" shortcut |
| `/api/setup`, `/api/display`, `/api/log` | the TRMNL device protocol (below) |

Every image takes `?w=&h=` (default 800×480) and `?download=1` to save rather
than display.

**A date of interest.** Every image also takes `?date=YYYY-MM-DD`, which
draws the screen **as of that day**: as it would have looked then. The
date stands in for today throughout, so the daily brief and last night
show that day and the night before it, heart rate today shows that whole
day (and heart rate yesterday the one before), month and week screens take
that date's month and week (their "last month" and "last week" count back
from it), and every trailing window (trends, oxygen, the sleep schedule,
body signals) ends there. The masthead adds the date (*"WEEK IN REVIEW ·
NOV 12"*), so a screen from another day is never mistaken for today's. A
screen's own `?day=`, `?week=` or `?month=` still wins over it. Today's
date is the same as none, and a future date is refused. The footer stays
live: it describes the ring and the server now. On the index page, the
**as of** box in the settings form applies a date to every chart on the
page (the nights list then stops at it, and its links keep their own
nights), with a link back to today. Images are rendered on each request — a few milliseconds — so
they are never stale: the daily view rolls over at midnight whether or not a
fetch has run. Each carries an `ETag`; a client that sends it back in
`If-None-Match` gets a bodiless `304` when nothing changed, which is how an
e-ink panel can poll without redrawing.

Until a fetch succeeds after a start, fetches cover `-startup-days` (default
365) instead of `-days` (7): when a new version starts storing something Oura
was already sending — temperature deviation, say — its history fills in on
deploy, with no manual backfill, and the year-long screens (fitness and
sleep) have their whole year. That back-fill pulls a year of every
collection, heart rate included, so it gets 20 minutes before it times out
rather than a routine fetch's 5; it typically takes a few minutes, and the
index page says "fetching now, back-filling" meanwhile.

A failed fetch (network down, token revoked) is logged and shown in red on
the index page, and the server keeps serving what the database holds. It's
retried soon rather than after a whole interval: after a minute, then 2, 5,
15 and 30 minutes, then hourly, never later than `-every`, and back on the
usual schedule once one succeeds. The commonest failure is a boot on a board
with no battery clock (most small boards, a Raspberry Pi): until the clock is set
from the network Oura's certificate doesn't verify, and the retry a minute
later goes through. The service also waits for the clock to be set where it
can (see `scripts/wolfe-health.service.in`). The webhook subscription check
retries on the same schedule. The
token file is re-read on every fetch, so a fresh one dropped in takes effect
without a restart. **Run only one fetcher per token**: where the daemon runs,
don't also schedule `fetch.sh`, since refresh tokens are single-use.

</details>

### Backups

<details>
<summary>Show</summary>

Most of the database is a copy of Oura that a fetch rebuilds, but not all of
it. The **habit log**, the **settings** (display schedule, the habit log's and
the webhook's tokens) and the **display pairings** were made here and exist
nowhere else. So:

- **The daemon snapshots the whole database once a day** into `backups/`
  beside it (`wolfe-health-2026-11-21.db`), keeping 14 (`-backup-keep`; 0
  turns it off; `-backup-dir` or `WH_BACKUP_DIR` to put them elsewhere). A
  snapshot is consistent while the daemon writes, private to its owner (it
  holds the tokens), and an ordinary database file: to roll back, stop the
  service and copy one over `wolfe-health.db`.
- **Keep a copy of the habit log off the server:** *Download every log* on
  the index page (`/log/export.csv`), or `wolfe-health habits export
  habits.csv`. `wolfe-health habits import habits.csv` puts it back, adding
  only the logs that are missing, so it's safe to run on a database that
  already has some.
- **Never delete the database to fix a changed table.** Stop the daemon,
  run `wolfe-health reset sleep_periods` (it drops only tables Oura can
  refill, and refuses the rest), and start it again: the start's year-long
  fetch refills the table. Deleting the file loses everything above, and
  any history older than a year.

</details>

### Oura webhooks

<details>
<summary>Show</summary>

Polling finds a sync on its next fetch, hours later. With webhooks, Oura
tells the daemon about 30 seconds after the ring syncs, and the daemon
fetches straight away: notifications within half a minute of each other
(a sync sends several) become one fetch. Oura has to reach the daemon from
the internet over HTTPS, so the webhook gets a port of its own that serves
only `/oura/webhook`, and that one port is exposed (for example with
Tailscale Funnel; see `docs/deploy-linux.md`). The charts and the
schedule form, which have no login, stay on the home network.

```bash
wolfe-health daemon -webhook-listen 127.0.0.1:8091 \
  -webhook-url https://<machine>.<tailnet>.ts.net/oura/webhook -every 6h
```

- `-webhook-listen` serves Oura's one-time verification (it echoes the
  challenge for the right token) and its notifications, refusing any whose
  `x-oura-signature` isn't the HMAC-SHA256 of the timestamp and body under
  the app's client secret.
- `-webhook-url`, the public address of that port, is subscribed to new and
  changed data of every type stored, using the app's client ID and secret.
  The daemon checks the subscriptions daily and renews any lapsing within a
  week. The verification token is made once and kept in the `settings`
  table.
- Keep polling, more slowly: Oura retries a failed delivery for only about
  an hour, and heart rate has no webhook. `-every 6h` is a fair catch-up.
- `wolfe-health webhooks` lists the subscriptions; `webhooks delete ID` or
  `webhooks delete all` removes them (after a change of URL, say).

The index page shows the subscriptions and the last notification.

Setting it up on a server (Tailscale, Funnel, the DNS and certificate
pitfalls, and the security picture) is in `docs/deploy-linux.md`.

</details>

### Phone alerts

<details>
<summary>Show</summary>

The daemon can send a phone notification when something is worth knowing
without looking at a display. It sends through **Home Assistant**, to a
phone its companion app has registered.

**This expects a Home Assistant you already run.** wolfe-health doesn't
install or manage one: it only calls an existing one's notify action, over
its API. Without Home Assistant, leave the settings unset and alerts stay
off; everything else works the same.

The alerts:

| Alert | When |
|---|---|
| **Morning summary** | every day from 8 AM, once last night has synced, short enough to read in the lock-screen preview: the two scores and their change against your 7-day average as the title (`Sleep 92 ↑7 · Readiness 86 ↓4`), then time asleep and when, the two weakest parts behind the scores, and yesterday's steps and activity. If last night hasn't synced by 11, it goes out saying so. A tap opens the daily brief |
| **Early warning** | two or more of the early warning's signals (temperature, lowest heart rate, HRV, breathing) beyond your normal two nights running, ending last night. It names each signal's reading and anything logged before last night (alcohol, a late night) that could explain it. Time-sensitive: it gets through a Focus |
| **Not getting data** | no fetch from Oura has worked for 13 hours. When the error is the login's, it says to run `make auth` |
| **The ring hasn't synced** | fetches work, but the ring's newest reading is over 18 hours old |

Each goes out **once** per occurrence (a key per day, recorded in the
`alerts_sent` table), **never in quiet hours** (10 PM to 7 AM by default;
one that comes up overnight goes out when they end), and **at most ten a
day** by default (the test buttons' sends don't count). A tap opens the
relevant page on this server. Rules are checked after every fetch and every
five minutes, so the summary arrives within five minutes of 8, or of the
ring syncing after that. The index page's **Phone alerts** section names the phone,
has a **Send test** button and a **Send morning summary now** button (this
morning's summary as it stands, any time, without using up the day's 8 AM
one), and lists the latest sent.

**Enabling alerts**, once Home Assistant is running:

1. **Put the Home Assistant app on the phone** (App Store or Google Play),
   sign in to your Home Assistant, and allow notifications when it asks.
   Signing in registers the phone, which gives it a notify action named
   after it: `notify.mobile_app_<phone name>`.
2. **Check the server can reach Home Assistant.** On the server, this
   should print `401` (reachable, but no token yet):
   ```bash
   curl -s -o /dev/null -w "%{http_code}\n" http://<home-assistant>:8123/api/
   ```
   If Home Assistant is on another network, use an address that crosses
   it, such as its Tailscale address (`100.x.y.z`).
3. **Make a token.** In Home Assistant, open your profile (bottom left) →
   **Security** → **Long-lived access tokens** → **Create token**, name it
   `wolfe-health`, and copy it: it's shown once.
4. **Find the phone's notify action.** In Home Assistant, **Developer
   tools** → **Actions**, and search `notify.mobile_app`: one per phone.
   Use the part after `notify.`, e.g. `mobile_app_your_iphone`.
5. **Add the settings to `.env` on the server** (table below), keeping the
   file private (`chmod 600 .env`).
6. **Restart** (`sudo systemctl restart wolfe-health`, or `make deploy`).
   The log says `alerts: on, to Home Assistant · mobile_app_…`.
7. **Send a test** from the index page's **Phone alerts** section, and
   check the phone.

The settings, in `.env` on the server:

| Variable | Value |
|---|---|
| `WH_HA_URL` | Home Assistant's address as the server reaches it, e.g. `http://homeassistant.local:8123`, or its Tailscale address on another network |
| `WH_HA_TOKEN` | a long-lived access token: in Home Assistant, your profile → Security → Long-lived access tokens |
| `WH_HA_NOTIFY` | the phone's notify action, without `notify.`: e.g. `mobile_app_your_iphone` (Developer tools → Actions lists them) |
| `WH_ALERT_LINK` | this server as the phone reaches it, for the tap, e.g. `http://100.x.y.z:8090` (its Tailscale address, so a tap works away from home) (optional) |
| `WH_ALERT_QUIET` | quiet hours, local time: whole hours like `22-7` (the default) or `22:00-07:00`, across midnight when the end is earlier; `off` for none (optional) |
| `WH_ALERT_MAX_PER_DAY` | the most alerts sent in a day, counted from local midnight; the test buttons' sends don't count (optional; default `10`) |
| `WH_ALERT_SUMMARY_AT`, `WH_ALERTS_OFF`, `WH_ALERT_ILLNESS`, `WH_ALERT_STALE` | the summary's hour, alerts to turn off, the early warning's threshold and the stale-data limits: see [Configuration](#configuration) (optional) |

Without all of the first three, alerts are off. The token is a password to
Home Assistant: keep it in `.env`, which is gitignored and readable only by
you. Pushes to an iPhone go through Home Assistant's push relay to Apple.

The quiet hours and cap need nothing unless you want other values. To
change them, add the lines to `.env` and restart (a `make deploy` restarts
too); a value the daemon can't read stops it at start with a message
saying why, rather than being ignored:

```bash
ssh -t <host> 'printf "WH_ALERT_QUIET=23-6\nWH_ALERT_MAX_PER_DAY=8\n" >> /opt/wolfe-health/.env && sudo systemctl restart wolfe-health'
```

The start's log line shows what's in effect (`alerts: on, to … : never
between 11 PM and 6 AM (held until then), at most 8 a day`), as does the
index page's Phone alerts section.

</details>

### Habit log

<details>
<summary>Show</summary>

Oura's tags are the way to tie a habit to the night after, but logging them
means opening the app in the moment, and Oura's API can't take them from
anywhere else. So the daemon keeps its own **habit log**, filled from a Siri
shortcut: say "Hey Siri, log a habit", Siri asks "What are you logging?",
you answer "caffeine", and it replies with what it logged ("Logged 1 double
shot of espresso at 2:14 PM, about 130 mg of caffeine. That's 2 today.").

The shortcut's menu sends a habit's name exactly; the server matches it
ignoring case.

| Habit | Badge | How it counts |
|---|---|---|
| Caffeine | CAF | counted: one double shot of espresso (about 130 mg) |
| Alcohol | ALC | counted: one standard drink (about 14 g) |
| Meal | MEAL | judged: late within 3 hours of bed |
| Sweets | SWT | judged: late within 3 hours of bed |
| Workout | WKT | judged: late within 2 hours of bed |
| Screens in bed | SCR | once a day |
| Cannabis | CAN | counted: one dose |
| Late night out | OUT | once a day |
| Melatonin | MEL | counted: one dose |
| Cold plunge | CLD | once a day |
| Stressful day | STR | once a day |
| Sick | SICK | once a day |
| Travel | TRV | once a day |

**Counted** habits add up. **Once a day** habits count only whether they
happened. **Judged** habits are logged whenever they happen, and the server
decides whether each was late by its time against that night's bedtime (or,
for a night not slept yet, your usual bedtime: the average of the last two
weeks). The reply says so ("Logged meal at 9:10 PM, within 3 hours of your
usual 10:30 PM bedtime"). The charts count a late one as Late meal, Late
sweets or Late workout, with its badge drawn solid; an ordinary meal or
dessert isn't charted, while an ordinary workout is.

The catalog is `internal/habits`, shared by the server and the charts; a new
entry is understood and charted with no change to the shortcut. A log counts
toward the night after its day, and a day runs from 4 AM, so a drink at 1 AM
belongs to the evening before. Tags you add in the Oura app count too: those
that mean a habit here (Oura's alcohol, caffeine, travel and so on) as that
habit, others by their own names.

**Endpoints**, on the chart port (8090) only: reachable from the home
network and the tailnet, never through the Funnel, which carries only the
webhook port.

| Endpoint | What it does |
|---|---|
| `GET /log/habits` | the menu: one habit per line (no token needed) |
| `POST /log` | log one: JSON `{"habit": "Caffeine"}`, a name from the menu, optionally `"count": 2` (0.5 to 10) and `"at"` for a log made after the fact, or the same as form fields. Answers with a sentence to speak |
| `POST /log/undo` | remove the latest log, if it's from the last day |

`at` is when it happened, if not now: a time of day (`8:30 AM`, `08:30`),
taken as today, or yesterday if that would be later than now, so "10:30 PM"
said the next morning is last night; or a full date and time, ISO 8601 as
Shortcuts' Format Date gives it (`2026-11-21T08:30:00-08:00`). It can't be in
the future or more than a week back. A backdated log is charted, and judged
late or not, by its own time, and the reply names the day if it isn't today.

`POST` needs the habit log's **token**, in an `X-Log-Token` header (or a
`token` field). The daemon makes it once; the index page's **Habit log**
section shows it with a Copy button, along with the last week's logs. Each
has a date-and-time picker (and a count, for a counted habit) to correct it
with Save, and a Remove button.

**The ready-made shortcut**: [`docs/shortcuts/Log-a-habit.shortcut`](docs/shortcuts/Log-a-habit.shortcut)
is the whole thing below, the "When?" step included, and it ends by showing
the server's reply (Siri reads it aloud when you ask by voice). It's binary,
signed by Apple so phones will import it;
[`Log-a-habit.plist`](docs/shortcuts/Log-a-habit.plist) beside it is the same
shortcut as readable XML, and
[`Log-a-habit.json`](docs/shortcuts/Log-a-habit.json) the spec it's built
from. To use it:

1. Get the file onto the iPhone (AirDrop it from a Mac, or open it from
   iCloud Drive in Files) and tap **Add Shortcut**. It's signed by Apple, so
   it imports without any setting changes.
2. Answer its two setup questions:
   - **the server's Tailscale IPv4 address, with its port:**
     `http://100.x.y.z:8090` (`tailscale ip -4` on the server shows the
     address; its home-network address works too, but only at home);
   - **the habit log's token,** copied from the index page's Habit log
     section.

   They fill two Text steps at the top of the shortcut, which every request
   in it uses, so there's nothing else to edit. To change one later, edit
   that step.
3. Rename it **Log a habit** (or whatever you'd like to say to Siri).

**Building the shortcut** yourself instead (once, on the iPhone, in the
Shortcuts app; about five minutes). Name it **Log a habit**: Siri runs a
shortcut by its name.

1. **Get Contents of URL**: `http://100.x.y.z:8090/log/habits` (the
   server's Tailscale IPv4 address; `http://192.168.1.20:8090/log/habits`
   works too, but only at home). Leave the method as GET.
2. **Split Text**: split *Contents of URL* by **New Lines**.
3. **Choose from List**: the *Split Text*, with the prompt **What are you
   logging?**
4. **Get Contents of URL**: the same address ending in `/log` instead of
   `/log/habits`. Open *Show More*: Method **POST**; under *Headers* add
   `X-Log-Token` with the token from the index page; *Request Body* **JSON**,
   with one Text field, `habit`, set to *Chosen Item*.
5. **Speak Text** (or **Show Result**): *Contents of URL*, the server's reply.

**To backdate a log you forgot** (the 8 AM coffee, remembered at 11), add a
"When?" step between 3 and 4:

- **Choose from Menu**, prompt **When?**, with two items, **Now** and
  **Earlier**.
- Under **Now**: a **Text** action left empty.
- Under **Earlier**: **Ask for Input**, Input Type **Date and Time**, prompt
  **When was it?** (it starts at now, so you only move the hour), then
  **Format Date** with Date Format **ISO 8601** and *Include ISO 8601 Time*
  on.
- In step 4's JSON body, add a second Text field, `at`, set to *Menu Result*.
  Empty means now.

To log two at once, add **Ask for Number** ("How many?") before step 4 and a
Number field `count` set to it in the body. For mistakes, make a second
shortcut, **Undo habit**: one **Get Contents of URL** to `/log/undo`, method
POST with the same header, then **Speak Text**.

Other ways to run the same shortcut: **Back Tap** (Settings → Accessibility →
Touch → Back Tap → Double Tap → *Log a habit*), the **Action button** on
phones that have one, a Home Screen icon, or an **NFC sticker** on the coffee
machine (Shortcuts → Automation → NFC). The automation can run *Get Contents
of URL* to `/log` with `habit` fixed to Caffeine, so a tap logs it without
asking.

**Reaching the server from the phone**: use the server's **Tailscale IPv4
address** (`100.x.y.z`, from `tailscale ip -4` on the server) rather than
its tailnet name. The name depends on Tailscale's DNS on the phone, which
can fail ("DNS unavailable") while the connection itself is fine; the
address doesn't, and it stays the same unless the server leaves the
tailnet. Either needs Tailscale connected on the iPhone: turn on **VPN On
Demand** in the Tailscale app so it reconnects by itself, or add
Tailscale's own *Connect* action as the shortcut's first step. If the
server can't be reached, the shortcut stops with an error, so a log is
never silently lost.

</details>

### "How did I sleep" shortcut

<details>
<summary>Show</summary>

Ask Siri "How did I sleep" and it reads the morning's figures, from the
same data as the daily brief:

> *Sleep score 88, 5 above your weekly average. You slept 9 hours 30
> minutes. Readiness 90, above your usual. Sleep was held back by timing
> at 54.*

It names the readiness parts under 70 when there are any, and when last
night's score isn't in yet it says so and reads the latest night instead.
The shortcut is [`docs/shortcuts/How did I sleep.shortcut`](<docs/shortcuts/How did I sleep.shortcut>):
fetch `/summary.txt`, then speak it. Import it on the iPhone (AirDrop it,
or open it from iCloud Drive in Files) and answer its one question, the
server's Tailscale address with the endpoint: `http://100.x.y.z:8090/summary.txt`.
It keeps the file's name, which is the Siri phrase. For the morning, a
Shortcuts automation can run it when you stop your alarm (Automation →
Alarm → Is Stopped).

</details>

### "Check Wolfe Health" shortcut

<details>
<summary>Show</summary>

The phone alerts can't say the server is down: the server is what sends
them. `docs/shortcuts/Check Wolfe Health.shortcut` checks from the outside,
from your phone, on a timer:

```
 Automation (a time of day) ──► GET /health.txt
                                     │
            no answer (server down,  │ answers
            asleep, unreachable) ────┤
                    │                ▼
                    ▼           "ok"? ──yes──► nothing (silent)
   iOS: "Check Wolfe Health        │no
   failed" notification            ▼
                         notification: "Wolfe Health needs a look"
                         and what's wrong (no data from Oura since …,
                         the ring hasn't synced since …)
```

**Set it up:**

1. **Import it:** AirDrop `docs/shortcuts/Check Wolfe Health.shortcut` to
   the phone (or open it from iCloud Drive in Files) and tap **Add
   Shortcut**. It asks for the server's address: the Tailscale one, so the
   check works away from home, ending `/health.txt`.
2. **Run it once by hand** in the Shortcuts app. Nothing should happen
   (all's well), and iOS asks once to allow it to reach that address:
   allow it.
3. **Put it on a timer:** Shortcuts → **Automation** → **+** → **Time of
   Day**: pick a time, **Daily**, and **Run Immediately** (with **Notify When
   Run** off, so only problems make a sound). iOS runs automations at set
   times, not every N minutes, so for a few checks a day make a few
   automations (e.g. 9 AM, 1 PM, 6 PM).

Two limits: a phone that can't reach the server (off Tailscale, no
signal) looks the same as a server that's down, so a lone failure while
away from home may be the phone; and the iOS failure notice names the
shortcut, not why, so open the index page to see.

</details>

### Building and changing the shortcuts

<details>
<summary>Show</summary>

Each shortcut in `docs/shortcuts` comes as three files:

- `Name.json`, **the source**: a short, readable spec of the shortcut's
  steps, the one to edit;
- `Name.plist`, the shortcut as Apple's XML, built from the spec, for
  reading and diffing;
- `Name.shortcut`, the same signed by Apple, the one a phone imports
  (phones refuse unsigned shortcuts).

`scripts/shortcut.py` turns one into the next, and `make shortcut` runs it
for every spec that changed: it builds the `.plist`, checks that every step
it refers to exists and every menu closes, signs it into the `.shortcut`,
and verifies the signed copy matches. It needs macOS 12 or later (for its
`shortcuts`, `aea` and `aa` commands), `python3`, and a Mac signed in to
iCloud and online, since Apple issues the signing certificate. Step IDs are
derived from the spec, so an unchanged spec builds the same `.plist`, and
`make shortcut` leaves shortcuts whose spec hasn't changed alone.

The spec format is documented at the top of `scripts/shortcut.py`:

```json
{"name": "How did I sleep",
 "import_questions": [{"action": "summary", "param": "WFURL", "text": "Your server's address"}],
 "actions": [
   {"action": "downloadurl", "id": "summary", "params": {"WFURL": "http://100.x.y.z:8090/summary.txt"}},
   {"action": "detect.text", "id": "text", "params": {"WFInput": "{{summary}}"}},
   {"action": "speaktext", "params": {"WFText": "{{text}}"}}]}
```

Each step names an action and its parameters, and `{{id}}` uses an earlier
step's output; menus, Ifs and repeats are written as one step holding their
branches.

**Going the other way**, `make decode` turns any shortcut back into a spec:
one exported from a phone, one you built or edited in the Shortcuts app, or
someone else's.

```sh
make decode SHORTCUT=~/Downloads/Mine.shortcut                       # writes ~/Downloads/Mine.decoded.json
make decode SHORTCUT=~/Downloads/Mine.shortcut SPEC=docs/shortcuts/Mine.json
```

It names each step whose output is used after that output (`contents_of_url`,
`selected_item`, …; rename them freely), turns references back into
`{{id}}`, headers and JSON bodies into `$dict`, menus, Ifs and repeats into
single steps, and setup questions into `import_questions`. Anything the
shorthand can't say, like a property taken from a value, is kept exactly as
`$raw`, so rebuilding the spec gives back the same shortcut. It never
overwrites a file. So to change a shortcut on the phone and keep it here,
export it, decode it over a fresh name, and replace the old spec with it.

Apple doesn't document actions' parameter names, so the reliable way to
learn one is to build a small shortcut using it on a phone, export it, and
decode it: `make decode` shows it as a spec, and
`python3 scripts/shortcut.py decode It.shortcut It.plist` as Apple's own
XML. Keep tokens and private addresses out of specs: use
placeholders and setup questions (`import_questions`), as the shortcuts
here do.

That's how the If in Check Wolfe Health was put right. Built at first, its
condition showed up blank on the phone, with no error: the builder linked
the If's input to the earlier step's output as text holding the variable
(`WFTextTokenString`). Fixed by hand on the phone, exported and decoded,
it showed iOS stores that input as a direct link to the output
(`WFTextTokenAttachment`), which the builder now writes. A field that
comes up blank on the phone is the sign of a format like that; fix it
there once, export, decode, and compare with the built `.plist`.

</details>

### TRMNL e-ink display

<details>
<summary>Show</summary>

The daemon is also a TRMNL "bring your own server" (BYOS) server
([protocol](https://docs.trmnl.com/go/diy/byos)), so a TRMNL device can be
pointed straight at it: hold the device's button 5 seconds, join its
**TRMNL** Wi-Fi network from a phone, choose **Custom server**, and enter the
daemon's address (e.g. `http://192.168.1.20:8090`), then your home Wi-Fi. The
index page lists paired devices with battery, signal and last update, and
shows the exact address to enter.

| Endpoint | Called by the device |
|---|---|
| `GET /api/setup` | once after Wi-Fi setup: pairs, returns a key; the screen shows the wolf mark and a device ID |
| `GET /api/display` | every wake: returns the image URL, a filename, and how long to sleep |
| `POST /api/log` | when something goes wrong; shown on the index page |

What it shows follows the **display schedule** (below), rendered at the size
the device reports. The device caches
images by filename and downloads only names it hasn't seen, so the filename
is `<screen>-<hash>-<time first made>`: it changes exactly when the image
does. Any device that asks is paired (this is a home server), and a device
with a key the server doesn't know — after replacing the database, say — is
adopted rather than reset; the server never sends `reset_firmware`.

**Display schedule.** The day is split into dayparts, each starting at a
clock time and running until the next, with up to four screens that rotate on
a hold time. The defaults follow the day, each screen when it's most use,
rotating every 15 minutes (a TRMNL's usual wake interval) so every daypart
shows all its screens:

| From | | Screens, rotating every 15 minutes |
|---|---|---|
| 5 AM | Morning: today, and last night in detail | Daily brief → WolfePhase → Last night → WolfeSense · Sleep |
| 6:30 AM | Morning: the body against its normal | Daily brief → Early warning → Vital signs → WolfeSense · Readiness |
| 8 AM | Weekly progress | This week so far → WolfeSense · Week → Weekly rhythm → Trends |
| 11 AM | The longer trends | Last week's review → WolfeSense · Signals → Fitness and sleep → Oxygen tide |
| 2 PM | Toward a good night | Sleep schedule → Sleep stages → Bedtime payoff |
| 10 PM | Overnight | Bedtime payoff only: the best bedtime window as you turn in, and nothing changes overnight, so a panel in a dark room never flashes |

The month's calendars stay out of the daily rotation. **On the 1st of each
month**, every daypart that rotates leads with how last month panned out:
last month's report, then its score calendar, stress calendar and
WolfePhase · Month, each the whole month just ended. **On Sundays**, last
week's review leads instead; on a Sunday the 1st, the month comes first,
then the week. Single-screen dayparts keep their one screen, as the
unchanging overnight one does.
Checkboxes in the schedule form turn either off. "Last week's review", "This
week so far", "Last month's report" and last month's three calendars are also
screens you can put in any daypart.

To show the displays a past day, set **Show the displays as of** in the
schedule form: every screen they show is drawn as of that date, as with
`?date=`, until you clear the box, when they return to today, live. The
index page says so beside what's showing now. A future date is refused.

Edit it under **Display schedule** on the index page (start times, screens,
hold time from 1 minute to 12 hours, reset to the default, or clear every
daypart to build one from scratch before saving; a short hold
wakes a TRMNL and refreshes its screen that often, so suits one on mains
power); it is stored in the database's
`settings` table. Which screen is up is computed from the clock alone, so a
restart changes nothing, and a device's sleep is cut short so it wakes just
after each scheduled change. A screen that can't be drawn (last night, before
any sleep is stored) falls back to the daily brief. `/current.png` serves
whatever the schedule shows now, for any other display.

**Display emulator.** No panel yet? `/emulator` shows what one would:
`/current.png` converted exactly as a TRMNL receives it (four grays, or
whatever `-trmnl-bits` and `-trmnl-format` set), in a panel-shaped frame,
with the screen that's up and when it changes. It checks every minute and
swaps in a new screen with a flash, as a panel does on a full refresh.
**Next screen** shows the next one in the daypart's rotation at once (in a
daypart of one screen, the next of every screen offered); the timer keeps
running, and the schedule takes over again at its next change. It steps the
emulator only, never the displays (`/current.png?advance=N` does the same).
A button switches between a paper look (e-ink's warm white and soft black)
and the image's exact pixels. Three views: framed, as the device looks; edge
to edge, the panel's 800×480 as large as the window allows; and fill screen,
every screen drawn at the window's own size, so it fills a tablet in either
orientation (laid out for that size, so not pixel for pixel what a TRMNL
shows). Each view fits the room the page has, measured rather than taken
from viewport units: on a phone on its side, fill screen draws at the
smallest size the charts allow (640×384) and shows it scaled to fit, the
caption shrinks to one line, and the frame slims; on a screen taller than
wide, fill screen keeps the display's shape rather than drawing every screen
tall and thin. `?view=framed|edge|fill` opens the page in a view. `make emulator` opens it in a window of its own on a Mac (a Chrome
app window, or Safari, where File > Add to Dock does the same), from the
`DEPLOY_HOST` server or `EMULATOR_URL`. On an iPad or iPhone, open it in
Safari and choose Share > Add to Home Screen: from that icon it opens full
screen, as an app, with no browser bars, in the fill-screen view. Set
Auto-Lock to Never to keep it showing.

</details>

### Detail views

<details>
<summary>Show</summary>

Each screen has a few detail views, its closer looks (`internal/server/details.go`):
the daily brief opens onto last night, WolfeSense · Sleep and · Readiness,
vital signs, the early warning and this week so far (Week in review for the
current week); the week views onto this month's report, and from there its
calendars; vital signs onto the
early warning, the radar, body signals and oxygen tide; and so on, for every
screen the schedule offers. So a display can sit on one main screen and let
you look behind it, rather than the schedule rotating through everything.

**On a tablet (the emulator).** Tap the screen: a strip of its detail views
appears along the bottom. Tap one to show it; tap again for *its* detail
views, and so on. **Back** steps out one. **Home is the daily brief**, a
button on every screen's strip and in the caption, whatever the schedule is
showing; after three minutes without a tap the page goes back to the
schedule by itself.

Some screens also have **tap zones** that open a view directly. On the daily
brief: **Readiness** opens WolfeSense · Readiness, **Last night** opens
WolfePhase, and **Today** opens heart rate today. On **Week in review**
(last week or this week so far), **each day's card opens the daily brief
for that day**, and so does **each day on the month views** (the monthly
report, score calendar, stress calendar and WolfePhase · Month, this month's
or last). A WolfePhase night opens the brief of the morning after it, the
one that leads with that night. Whatever you tap open from there stays on
that day, and the caption names it. A tap anywhere else
(Yesterday, the title, the edges) opens the strip as usual, and while the
strip is open a tap anywhere but its buttons just closes it. Each chart names
its zones as it draws them, so they match at any size; `/current.png` sends
them, as fractions of the image, in an `X-Zones` header, and
`internal/server/details.go` (`screenZones`) says what each opens.

`/emulator?show=<screen>` opens the emulator on a screen,
`/current.png?show=<screen>` draws any screen as a display gets it, and
`/emulator/now?show=<screen>` lists its detail views; each takes
`&date=YYYY-MM-DD` for the screen as of that day.

**On a TRMNL (the button).** A TRMNL has no touchscreen, and it sleeps
between updates, so it steps instead: **press the button** and it shows the
main screen's first detail view; press again for the next. After the last
comes the daily brief, home, so it's always a few presses away, and then
it's back to the main screen. Ten minutes after the last press, or when the
schedule moves on, it returns to the main screen by itself. The device
doesn't say why it woke, but only a press wakes it early: a request well
before the time it was told to sleep until (by a third of that sleep, at
most two minutes) counts as one. Each press is logged as
`trmnl: <device> button: showing <screen>`. A server restart forgets where a
device had stepped to, and its next wake shows the main screen.

**E-ink output.** Images go to the panel as a 4-gray (2-bit) PNG by default
— the OG model's firmware 1.6+ supports it, and it keeps the charts' light
gray lines. Any image URL also takes `?bits=1|2` and `?format=bmp`, so you can
preview in a browser exactly what a panel gets. The 1-bit conversion is
deliberate, not a blind threshold: hairlines become dotted lines, the lane
shade becomes white, gray text is thresholded high enough to stay whole, and
white text on black (the masthead) is judged by its dark surroundings.

| Daemon flag | Default | Meaning |
|---|---|---|
| `-trmnl-bits` | `2` | `2` for four grays, `1` for black and white (older firmware) |
| `-trmnl-format` | `png` | `bmp` for a 1-bit BMP (exactly 48,062 bytes at 800×480) |
| `-trmnl-refresh` | `15m` | how long the device sleeps between updates |

</details>

### Deploying to a server

<details>
<summary>Show</summary>

Any Linux machine can run the daemon (a small ARM board, a Raspberry Pi,
a PC), and nothing about a particular machine is hard-coded. Each
machine's details are settings:

| Setting | Where | Default |
|---|---|---|
| SSH destination | `DEPLOY_HOST` in `deploy.mk` (gitignored; copy `deploy.mk.example`) | — |
| Install directory | `DEPLOY_DIR` in `deploy.mk` | `/opt/wolfe-health` |
| CPU | `TARGET_ARCH` / `TARGET_ARM` in `deploy.mk` | `arm64` (64-bit ARM boards and Pis); `arm` + `7`/`6` for 32-bit Pis; `amd64` |
| Service user | `install-service.sh --user` on the server | whoever runs it with sudo |
| HTTP port | `install-service.sh --port` | `8090` |
| Timezone | `install-service.sh --tz` | the system's |
| Extra daemon flags | `install-service.sh --args` | none |
| Login callback port | `wolfe-health auth -port` / `WH_AUTH_PORT` (register the matching address on the Oura app) | `8080` |

First install: `make bundle`, copy `bin/deploy` to the server's install
directory, and run `sudo ./install-service.sh` there; it fills in the service
template (`scripts/wolfe-health.service.in`), sets ownership, and starts it.
Updates: `make deploy` (build, copy, keep the previous binary, restart) and
`make rollback`. **Run only one fetcher per token**: move the token, don't
copy it, and `make unschedule` on the Mac.

**Which build is running.** Every build is stamped with its release (from
the newest `v*` tag: `v1.0.0`, or `v1.0.0-3-gabc1234` three commits
after it), the git commit it was built from, whether the working tree had
uncommitted changes, and when it was built (`internal/version`: Go records
the commit, the Makefile adds the release and build time; see
[Releases](#releases)). It shows on the index page (`Running v1.0.0
(a6feadd, committed Nov 24 9:48 PM, built Nov 24 10:00 PM)`), in the daemon's first log
line at start, under `version` at `/status`, and from
`wolfe-health version`. "+ uncommitted changes" (or `e44ea92+`) means the
build had work that isn't committed, so it can't be rebuilt from git alone.

The step-by-step runbook for a Linux server (first setup, updates, Oura
webhooks through Tailscale Funnel, backups and troubleshooting) is
[docs/deploy-linux.md](docs/deploy-linux.md).

</details>

### Logs

<details>
<summary>Show</summary>

The service logs to the systemd journal. From the Mac (with
`DEPLOY_HOST` as the server, e.g. `you@192.168.1.20`):

```bash
ssh <host> 'journalctl -u wolfe-health -f'                        # follow live (Ctrl-C stops)
ssh <host> 'journalctl -u wolfe-health -n 50 --no-pager'          # the last 50 lines
ssh <host> 'journalctl -u wolfe-health --since today --no-pager'  # since midnight
ssh <host> 'journalctl -u wolfe-health --no-pager | grep alerts:' # one kind of line
```

The service's user can read its journal without `sudo`. `-u` (`--unit`)
selects the systemd unit: the daemon's own lines plus systemd's about it
(started, stopped, crashed and its exit code), so it's the one to use when
the daemon restarts unexpectedly. `-t wolfe-health` (`--identifier`)
matches the lines tagged `wolfe-health` from anywhere, which is nearly the
same but leaves out systemd's.

Lines worth knowing:

| Line | Means |
|---|---|
| `wolfe-health e44ea92 + uncommitted changes, committed …, built …` | the daemon started: which build (its git commit, whether it had uncommitted changes, when it was built) |
| `serving on :8090, fetching every 6h0m0s` | the daemon started |
| `fetched <from> to <to>: …` / `fetch failed after …: …` | each fetch from Oura; `skipped <collection>` in a fetched line is a missing scope |
| `alerts: on, to Home Assistant · …` | phone alerts are configured (at start) |
| `alerts: sent <key>: <title>` / `alerts: …` with an error | an alert went out / one didn't, and is tried again at the next check |
| `trmnl: <device> button: showing <screen>` | a TRMNL's button stepped to a detail view |
| `http: <method> <path> <status> <time> <client>` | a request to the server. By default only the ones worth seeing: changes (POSTs), errors, requests slower than 2 seconds, and the TRMNL's calls to `/api/`. The displays' routine reads (`/current.png` and `/emulator/now` every minute, the index page's charts) are left out; see "Logging every request" below |

**Logging every request.** The request log is quiet by default. To see
every request, the displays' minute checks included (for example, to watch
what a display is asking for), turn on `WH_LOG_REQUESTS` and restart:

```bash
ssh -t <host> 'echo WH_LOG_REQUESTS=1 >> /opt/wolfe-health/.env && sudo systemctl restart wolfe-health'
ssh <host> 'journalctl -u wolfe-health -f'    # watch it
```

To go back to quiet, remove the line and restart:

```bash
ssh -t <host> "sed -i '/^WH_LOG_REQUESTS=/d' /opt/wolfe-health/.env && sudo systemctl restart wolfe-health"
```

`1`, `true` or `yes` turns it on; `0`, `false`, `no` or leaving it out
keeps it quiet. Running the daemon by hand, `WH_LOG_REQUESTS=1 make daemon`
or `./bin/wolfe-health daemon -log-requests` does the same. `sudo` asks for
your password on the server, hence `ssh -t`. Expect about two lines a
minute per open display.

</details>



## How it works

<details>
<summary>Show</summary>

One process, the daemon, does everything: it fetches from Oura into a
SQLite file, draws every screen from that file on request, serves the
displays, and sends the phone alerts.

```
   Oura API ──── webhook: "new data" ───────────────┐
      ▲                                             │
      │ fetch: every few hours, on a webhook        ▼
      │ (oura, fetch; OAuth token, single-use)  ┌──────────────────────────┐
      └─────────────────────────────────────────┤  daemon                  │
                                                │  cmd/wolfe-health        │
   Siri shortcuts ── POST /log (habits) ───────►│                          │
                                                └───┬──────────────────┬───┘
                                                    │                  │
                                          ┌─────────▼─────────┐        │ after each fetch
                                          │ SQLite (store)    │        │ and every 5 min
                                          │ Oura's data (a    │        ▼
                                          │ cache) + what's   │  ┌────────────┐   notify    ┌────────────┐
                                          │ made here: habit  │◄─┤ alerts     ├────────────►│ Home       ├──► phone
                                          │ log, settings,    │  │ rules →    │  (its API)  │ Assistant  │
                                          │ alerts_sent       │  │ engine     │             └────────────┘
                                          └─────────┬─────────┘  └────────────┘
                                                    │
                           dashboard → chart → server.Render (a screen name → an image)
                                                    │
            ┌───────────────────────┬───────────────┴──────┬────────────────────┐
            ▼                       ▼                      ▼                    ▼
   TRMNL: /api/display      emulator (iPad,        index page: every     /summary.txt:
   every 15 min; an early   phone): /current.png   chart, schedule,      the morning in
   wake is a button press   each minute; taps      habits, alerts        words (shortcut)
   → next detail view       → detail views
```

Drawing a screen is a pipeline: each layer hands a plain Go value to the
next and knows nothing about the layers beyond its neighbors:

```
 SQLite file         store              shaping                  chart              output
┌────────────┐   ┌───────────┐   ┌─────────────────────┐   ┌──────────────┐   ┌──────────────┐
│ samples,   │──►│ SQL query │──►│ pick, compare, word │──►│ paint pixels │──►│ PNG / BMP,   │
│ periods, … │   │ → structs │   │ → chart input types │   │ → image.Gray │   │ e-ink levels │
└────────────┘   └───────────┘   └─────────────────────┘   └──────────────┘   └──────────────┘
                  store            dashboard                chart              server
```

| Layer | Package | Knows about | Doesn't know about |
|---|---|---|---|
| Storage | `internal/store` | SQL, tables, rows | charts, images, Oura |
| Shaping | `internal/dashboard` | store types, what is meaningful to show and how to word it | pixels, fonts |
| Drawing | `internal/chart` | pixels, fonts, layout | the database |
| Serving | `internal/server` | which screen, its options, the schedule, the display, detail views and tap zones | how a screen is drawn |
| Collecting | `internal/oura`, `internal/fetch`, `internal/webhook` | Oura's API, OAuth, which collections to fetch, webhook subscriptions | screens |
| Alerting | `internal/alerts` | the rules, sending once, quiet hours, the cap, Home Assistant | pixels |
| Habits | `internal/habits` | what can be logged, and which night a log counts toward | storage |
| Wiring | `cmd/wolfe-health` | flags and `.env` settings, the daemon's loops | — |

**`chart` imports nothing else in the project**, so screens can be tested
with made-up data and the database can change without touching drawing.
`server.Render` is the one place a screen name becomes an image; the daemon,
the TRMNL schedule and the `chart` command all go through it. Within
`chart`, the parts screens share live in their own files: the WolfeSense
right-hand column (`column.go`), keys (`key.go`), the console banks and
gauges (`gauges.go`), formatting and hatching (`format.go`), the moon dial
(`drawPhaseDial` in `moon.go`), and the masthead and footer (`brand.go`).

</details>

### Fetching

<details>
<summary>Show</summary>

Two things start a fetch: the schedule, and Oura's webhooks. Both go
through the same `fetchOnce`, one at a time (a mutex), and each is followed
by an alerts check.

```
 schedule (fetch.Every)                         webhook (port 8091, via Funnel)
 ──────────────────────                         ───────────────────────────────
 start ──► fetch now                            Oura: "new sleep / readiness / …"
              │                                          │
        ┌─────┴──────┐                          token and signature check
        │ worked?    │                                   │
        └─┬───────┬──┘                          queue it (64 deep; if it's full,
       yes│       │no                            drop it: a fetch is coming)
          ▼       ▼                                      │
   wait -every   wait 1m, 2m, 5m, 15m,          wait 30 s, gathering the rest
   (6h here)     30m, 1h, 1h … (RetryDelays,    of the sync's notifications
          │      never past -every)                      │
          └──────┬───────┘                               │
                 ▼                                       ▼
          ┌──────────────────────────── fetchOnce ─────────────────────────────┐
          │ 1. lock (one fetch at a time)                                      │
          │ 2. window: -startup-days (365) until one works after a start,      │
          │    then -days (7); 20 min to finish a back-fill, 5 min otherwise   │
          │ 3. token: refresh first if it expires within a day (below)         │
          │ 4. the core collections (scores, sleep periods, contributors), then│
          │    the rest, each best effort: one that fails is "skipped"         │
          │ 5. upsert everything (a re-fetch overwrites, never duplicates)     │
          │ 6. status: last success / last error, shown at / and /status       │
          └────────────────────────────────┬───────────────────────────────────┘
                                           ▼
                                    alerts check (below)
```

**The Oura token** (`internal/oura/token.go`) is refreshed before it
expires, and Oura's refresh tokens work once, so the new one is saved
before it's used:

```
 need a token ──► expires within 24 h? ──no──► use it
                         │yes
                         ▼
                 refresh with the refresh token ──fails──► error: "run make auth"
                         │                                 (the fetch fails; after
                         ▼                                  13 h, an alert says so)
                 write the new tokens to a temp file,
                 rename over oura-token.json (atomic)
                         │
                         ▼
                 use the new access token
 an API call answers 401 ──► refresh once, retry once ──401 again──► error
```

</details>

### Drawing a screen for a request

<details>
<summary>Show</summary>

Every display request draws fresh, from the database (see Known gaps):

```
 GET /current.png?w&h[&show=…][&date=…][&advance=…]          (the emulator, each minute)
        │
        ├── show=<screen>? ──yes──► that screen, as of date= if given
        │        │no
        │        ▼
        │   the schedule: which daypart, which screen in its rotation now
        │   (1st of the month / Sunday: last month's / week's review first),
        │   stepped on by advance=
        ▼
 render(screen, date or the schedule's held date, w, h)
        │
        ├── dashboard: read the store, decide what to show, word it
        ├── chart: draw it; parts a tap can open are named (addZone)
        ├── frame: masthead, early-warning banner if it applies, footer
        │
        ├── can't draw (no sleep stored yet)? ──► the daily brief instead
        ▼
 encode: 4-gray PNG, or 1-bit / BMP if asked ─► hash = ETag
        │
        ├── X-Zones header: the zones, as fractions of the image
        ▼
 If-None-Match is that ETag? ──yes──► 304, nothing sent (the page keeps its image)
        │no
        ▼
 200 with the image (the page flashes and swaps it in)
```

The TRMNL takes the same path from `/api/display`, which returns the
image's URL and how long to sleep instead of the image itself.

</details>

### Alerts

<details>
<summary>Show</summary>

`internal/alerts` splits deciding from sending. **Rules** (`Rules`) are
plain functions of the database, the fetch status and the clock that say
what's true now, each as an alert with a key naming its occurrence
(`illness:2026-11-24`). The **engine** (`Engine.Check`) sends each key
once, holds alerts through quiet hours, stops at the day's cap, and
records what went out in `alerts_sent`; a failed send isn't recorded, so
the next check tries again. The **notifier** (`HomeAssistant`) posts to
Home Assistant's notify action; any type with `Send` and `Target` could
stand in. The daemon runs a check after every fetch and every five
minutes. The settings in `.env` (`LoadSettings`) adjust the rules without
code.


**A check** (`Engine.Check`), after every fetch and every five minutes:

```
 FetchState (last success, last error) + the clock + the database
        │
        ▼
 Rules: each alert that's on (not in WH_ALERTS_OFF) and true now
   illness · summary · fetch · sync   (each with a key: "illness:2026-11-24")
        │
        ├── none? ──► done
        ├── quiet hours (WH_ALERT_QUIET)? ──► done; nothing recorded, so whatever
        │                                     is still true goes out when they end
        ▼
 sent today = rows in alerts_sent since local midnight, not counting the
              test buttons' (test:…, summary-now:…)
        │
        ▼  for each alert, in order
 ┌─────────────────────────────────────────────────────────┐
 │ key already in alerts_sent? ──yes──► skip it            │
 │        │no                                              │
 │ sent today ≥ WH_ALERT_MAX_PER_DAY? ──yes──► stop, log   │
 │        │no                                    "holding" │
 │ send: Home Assistant notify, the tap's link             │
 │       = WH_ALERT_LINK + its page                        │
 │        │                                                │
 │   failed? ──yes──► keep going; report it; NOT recorded, │
 │        │          so the next check tries again         │
 │        │no                                              │
 │ record the key in alerts_sent; sent today + 1           │
 └─────────────────────────────────────────────────────────┘
```

**The rules**, each a plain function of the data and the clock:

```
 illness   early warning over the last week ─► nights with ≥ S signals up,
           counted back from last night ─► a run of ≥ N? ─► alert, naming each
           signal up and anything logged before last night  (S/N: WH_ALERT_ILLNESS)

 summary   before WH_ALERT_SUMMARY_AT? ─► nothing yet
           last night synced? ─yes─► alert: the scores, the night, the weak parts
                    │no
           three hours past? ─yes─► alert: "last night hasn't synced yet"
                    │no
                  wait (the next check looks again)

 fetch     a fetch has worked before, none for 13 h, and an error ─► alert;
           worded for the login when the error is the token's

 sync      fetches work, but the ring's newest reading (heart rate or
           battery) is over 18 h old ─► alert          (13 h / 18 h: WH_ALERT_STALE)
```

A key per day (`summary:2026-11-24`) is what makes each go out at most once
a day; the buttons use keys of their own (`test:<time>`,
`summary-now:<time>`), so they never use up the day's real one.

</details>

### Where a change goes

<details>
<summary>Show</summary>

| To add… | Touch |
|---|---|
| a new query | one short method in `store`, modeled on `Range` |
| a new screen | a builder in `dashboard`, a view in `chart` built from the shared parts (keys included), a case in `server`'s `render` with its route, and a line in the schedule menu and `chartViews` |
| a new data source | fetch it (`oura`, `fetch`) and store it (`store`); nothing downstream changes until a screen uses it |
| a new habit | an entry in `internal/habits`; the shortcut, the charts and the index page pick it up |
| a detail view | an entry in `screenDetails` (`internal/server/details.go`); the emulator's strip and the TRMNL's button pick it up |
| a tap zone | an `addZone` call where the chart draws the part, and an entry in `screenZones` saying what it opens |
| an alert | a rule in `internal/alerts` returning `[]Alert` with a key per occurrence, its name in `RuleNames` (so `WH_ALERTS_OFF` can turn it off), and a test with `testDB` |
| a setting | read it in `cmd/wolfe-health` (or `alerts.LoadSettings`), refuse a bad value at start, and add it to Configuration and `.env.example` |

For a quick experiment with a screen, the tests in `internal/chart` show how
to build one from made-up data; `chart -view <name> -o /tmp/x.png` renders
any screen from the real database.

</details>

## Inspecting the data

<details>
<summary>Show</summary>

```bash
sqlite3 wolfe-health.db 'SELECT * FROM samples ORDER BY day DESC LIMIT 10;'
```

Daily metrics live in one table, keyed by `(metric, day)`:

```sql
CREATE TABLE samples (
	metric     TEXT NOT NULL,     -- sleep, readiness, stress, activity, steps, temp_deviation, recovery,
	                              -- active_calories, total_calories, target_calories, meters_to_target,
	                              -- spo2, breathing_disturbance, cardio_age, pulse_wave_velocity,
	                              -- vo2_max, resilience (1-5), resilience_sleep/_daytime/_stress
	day        TEXT NOT NULL,     -- YYYY-MM-DD
	value      REAL NOT NULL,
	qualifier  TEXT NOT NULL DEFAULT '',  -- e.g. stress: restored/normal/stressful
	fetched_at TIMESTAMP NOT NULL,
	PRIMARY KEY (metric, day)
);
```

A single generic table rather than one per metric: adding a new Oura field is
then a code change with no migration. For stress, `value` is seconds spent in
high stress.

The seven sub-scores behind each night's sleep score get their own table,
`sleep_contributors`, one row per night keyed by `day` (so it joins onto the
`sleep` metric). A contributor Oura could not compute is `NULL`, not `0`.

`sleep_periods` holds one row per period of sleep — a night, a nap, or a
fragment — keyed by Oura's id, with `day` the wake-up day. It keeps Oura's
stage and movement strings as-is (one character per 30 seconds or 5 minutes:
`1` deep, `2` light, `3` REM, `4` awake) and the heart-rate and HRV series as
JSON. Note that Oura's `sleep` endpoint treats `end_date` as exclusive, unlike
the daily ones; the client adjusts for it.

**Everything else the login grants** is fetched too, into its own table or
as daily metrics above:

| Oura collection | Stored as |
|---|---|
| `daily_spo2` | metrics `spo2` (nightly average %) and `breathing_disturbance` |
| `daily_cardiovascular_age` | metrics `cardio_age` and `pulse_wave_velocity` |
| `vO2_max` | metric `vo2_max` |
| `daily_resilience` | metric `resilience`, 1 limited to 5 exceptional (the level's name in `qualifier`), and its three contributors |
| `sleep_time` | `sleep_times`: Oura's bedtime window and recommendation per day |
| `workout` | `workouts`: activity, intensity, times, calories, distance |
| `session` | `sessions`: type, mood, times, heart-rate and HRV series as JSON |
| `enhanced_tag` | `tags`: tag code or custom name, comment, times |
| `rest_mode_period` | `rest_mode_periods` |
| `heartrate` | `heart_rate`: every reading, day and night, keyed by Unix time |
| `ring_battery_level` | `ring_battery`: level and charging, keyed by Unix time |
| `ring_configuration` | `ring_configurations`: model, size, firmware |
| `personal_info` | `personal_info`: one row — age, height, weight, sex, email |

These are best effort: a collection that fails (a scope not granted, a ring
that doesn't measure it) is skipped with a warning in the fetch log, and the
rest go ahead. Heart rate and battery are fetched in 7-day requests, since
they hold a reading every few minutes. Nothing charts them yet; they are
there to build on. The database now holds your profile and email as well as
your health data — it never leaves the machine, but treat the file as
personal.

</details>

## Development

<details>
<summary>Show</summary>

```bash
make build   # host binary
make test    # store round-trips; OAuth, refresh, pagination; chart geometry
make vet
make pi      # linux/arm64 cross-compile for the Pi
```

The SQLite driver is `modernc.org/sqlite`, which is pure Go. Not
`mattn/go-sqlite3`: that needs cgo, which would turn `make pi` into a
cross-compiler setup problem. Worth preserving as dependencies get added.

</details>

### Releases

<details>
<summary>Show</summary>

Releases are git tags, `vMAJOR.MINOR.PATCH`, with notes in
[CHANGELOG.md](CHANGELOG.md): **major** when upgrading needs something from
you (listed under **Breaking**), **minor** for new things that need
nothing, **patch** for fixes.

```
 work on main, committed and pushed
        │
        ▼
 CHANGELOG.md: a "## vX.Y.Z — date" section saying what's in it; commit, push
        │
        ▼
 make release VERSION=vX.Y.Z ── refuses unless: the version is vX.Y.Z and newer
        │                       than the last tag · the CHANGELOG has its section ·
        │                       the tree is clean · main matches GitHub · vet and
        │                       tests pass
        ▼
 an annotated tag, pushed ──► the release page on GitHub (the link it prints;
        │                     paste the CHANGELOG section as the notes)
        ▼
 make deploy ──► the index page says "Running vX.Y.Z (…)"
```

`make release` is `scripts/release.sh`. A build between releases is
stamped `v1.0.0-3-gabc1234` (three commits after v1.0.0), so a deploy always
says how far it is from the last release. With tags, a version can be
installed directly: `go install
github.com/cameron-wolfe/wolfe-health/cmd/wolfe-health@v1.0.0`.

</details>

## Known gaps

- **Every request redraws.** Screens aren't cached: each display check
  draws its image fresh (0.1–0.7 s on a small ARM server), and the 304 for an
  unchanged image saves sending it, not drawing it. Fine for a few displays
  checking once a minute (a few percent of one core); with many more, keep
  drawn images in memory until the data changes (a fetch, a habit log, a
  saved setting) or the date does.
- **Webhooks still lean on a poll.** Heart rate has no webhook, and Oura
  retries a failed delivery for only about an hour, so a longer outage of the
  webhook URL (a reboot, Funnel down) is caught only by the next scheduled
  fetch. Each notification also refetches the recent days in full rather
  than just the object it names: simpler, and well inside the rate limits.
- **Overlapping runs.** A manual `make run` during a scheduled one is safe for
  the data (upserts) and the token (a losing refresh writes nothing), but the
  loser may exit with a refresh error; just run it again.
