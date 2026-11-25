# Quick start

From a fresh clone to your own Oura data on charts in a browser, in about
ten minutes. Everything past that (webhooks, the habit log, Siri shortcuts,
an e-ink display, deploying to a server) is in the [README](../README.md).

## You need

- **Go 1.23 or newer** (`go version` to check) and `make`
- **An Oura account** with a ring that has synced at least one night

## 1. Clone and build

```bash
git clone https://github.com/cameron-wolfe/wolfe-health.git
cd wolfe-health
make build
```

## 2. Register an Oura application

Oura's API is reached through OAuth, so you register wolfe-health as your
own private application. It's free and needs no review from Oura.

Go to <https://developer.ouraring.com/applications>, create an application,
and set:

| Field | Value |
|---|---|
| Redirect URI | `http://localhost:8080/callback`, exactly as written |
| Scopes | tick **all** of them |
| Website, privacy policy, terms | any URL; the repo's is fine |

Keep the page open: you need its **client ID** and **client secret** next.

## 3. Add your credentials

```bash
cp .env.example .env
```

Open `.env` and fill in the two values:

```
OURA_CLIENT_ID=...
OURA_CLIENT_SECRET=...
```

## 4. Log in to Oura

```bash
make auth
```

A browser opens on Oura's approval page, listing each permission with a
checkbox. **Tick every one, then approve.** A permission left unticked is
never granted, and that data (blood oxygen, heart rate, workouts…) is
skipped for good until you log in again.

The browser returns to `localhost:8080` and the terminal prints
`saved token to oura-token.json (scope "…")`: the list should have eleven
scopes, from `extapi:email` to `extapi:heart_health`. You do this once: the
app refreshes its own token from then on.

## 5. Start it

```bash
make daemon
```

Open <http://localhost:8090>. The first start fetches your last year of
data, which takes a few minutes: the page's "Last fetch" line reads
"fetching now, back-filling after a start" until it's done; then reload.
After that it fetches every hour for as long as it runs. Stop it with
Ctrl-C; starting it again carries on where it left off.

That's it: every chart on the page is drawn from your data.

## If something goes wrong

| Problem | Fix |
|---|---|
| `make auth` fails with a redirect or "invalid request" error | the redirect URI on Oura's page must be `http://localhost:8080/callback` exactly: `127.0.0.1` or a trailing `/` won't match |
| Some charts stay empty (blood oxygen, heart rate, fitness) while others fill, or the saved scope list is short | a permission wasn't granted: run `make auth` again and tick every permission on the approval page, then restart `make daemon` so the new data back-fills. See [Turning on more data after registering](../README.md#turning-on-more-data-after-registering) |
| `make auth` can't listen on port 8080 | something else is using it; stop that and try again |
| Charts are blank or say there's nothing yet | the first fetch is still running: check "Last fetch" on the page, or <http://localhost:8090/status> |
| A token error after weeks away | run `make auth` again |

## Good to know

- **Your data stays on your machine**, in `wolfe-health.db`. Your Oura login
  is in `oura-token.json` and the app's secret in `.env`. All three are
  gitignored; don't share them.
- **Use one machine at a time.** Oura's refresh tokens work once, so a copy
  of `oura-token.json` used elsewhere logs the other out. Log in separately
  on each machine instead.
- **Data older than a year?** Fetch your whole history once with
  `WH_DAYS=3650 make run`.
