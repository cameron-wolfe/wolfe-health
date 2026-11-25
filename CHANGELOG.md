# Changelog

Each release, newest first. Versions are `vMAJOR.MINOR.PATCH`:

- **Major**: upgrading needs something from you (a setting renamed or
  removed, a database table to reset, a shortcut to reinstall). Listed
  under **Breaking**, with what to do.
- **Minor**: new things that need nothing from you (a screen, an alert, a
  setting with a default).
- **Patch**: fixes only.

To release: add a section here, commit and push, then
`make release VERSION=vX.Y.Z` (see the README's *Releases*): it tags,
pushes, and publishes the GitHub release with this section as the notes.

## v1.2.0 — 2026-11-24

Small touches that make the panel feel like an app. Nothing to do on
upgrade.

- **Chart placeholders.** A chart's box keeps the chart's shape and shows
  the wolf mark, pulsing, while it draws, then fades the chart in; one that
  can't be drawn says so instead of showing a broken image.
- **Tab icons and shortcuts.** An icon on each tab, and ⌘1–⌘6 (Ctrl on
  Windows) to switch.
- **Confirmations that fade.** "Schedule saved", "Test sent", "Log removed"
  show briefly at the bottom and fade; failures stay on the page.
- **Asked first.** Removing a habit log or resetting the schedule asks to
  confirm.
- **A 404 page** with the wolf mark and the way back, for a browser; scripts
  and displays still get plain text.
- **Connection watcher.** When the server stops answering, an overlay says
  so and retries every few seconds, reloading the page once it's back. It
  runs on a one-second heartbeat, so a Safari Dock app or a phone's Home
  Screen app, whose timers can be held in the background, still notices; its
  status can be shown in the corner (Settings, or `?watch=1`).

## v1.1.0 — 2026-11-24

The web page becomes a control panel. Nothing to do on upgrade: deploy, and
the old address opens the new Today tab.

- **Tabs.** The one long page is now six, under the masthead: **Today**
  (fetch status, the running build, quick actions, and the daily brief,
  early warning, last night and readiness), **Charts**, **Display**
  (schedule, TRMNLs, the early-warning banner), **Habits**, **Alerts** and
  **Settings**. Each form returns to its own tab.
- **Settings page.** Every setting in effect, grouped, with the ones set in
  `.env` or on the command line in bold and secrets hidden.
- **Installable.** Add to Home Screen (or Add to Dock on a Mac) opens the
  panel like an app, without browser bars.
- **Phone-sized.** Tabs scroll sideways, wide tables scroll within the page,
  and everything fits a phone's width.

## v1.0.0 — 2026-11-24

The first release: Oura Ring data collected, charted, displayed, tapped
into, and watched over.

- **Collect.** A daemon fetches every Oura collection the login grants
  into SQLite, within about a minute of each ring sync through Oura's
  webhooks, with a catch-up on a schedule and retries after a failure.
  OAuth with single-use refresh tokens, saved safely.
- **Chart.** Some thirty screens for e-ink and screens alike: the daily
  brief, last night as a WolfePhase moon, sleep stages and schedule,
  bedtime payoff, readiness and sleep breakdowns, vital signs, body
  signals, an early warning for signals drifting toward illness, blood
  oxygen, heart rate, trends, fitness, weekly and monthly reviews and
  calendars, and the habit log's weeks and effects. Every chart keys its
  own marks.
- **Display.** A TRMNL "bring your own server" server on a daily schedule
  of dayparts, and an emulator page for a tablet or phone that fits any
  screen.
- **Interact.** Each screen's detail views: tapped into on a tablet or
  phone (with tap zones on the daily brief, week and month views opening a
  day's brief), stepped through with a TRMNL's button. The daily brief is
  home, a tap away from everywhere.
- **Notify.** Phone alerts through an existing Home Assistant: the morning
  summary, an early warning when several signals are beyond normal for
  nights running, and the data going stale. Quiet hours and a daily cap.
- **Habits.** A habit log filled from a Siri shortcut, with "How did I
  sleep" read aloud and a "Check Wolfe Health" shortcut that alerts when
  the server is down.
- **Run it.** Every setting in `.env`; daily database snapshots; a quiet
  request log; each build stamped with its release and commit; a runbook
  for a Linux server.
