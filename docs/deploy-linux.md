# wolfe-health on a Linux server

A runbook for running the daemon on a small always-on Linux machine (an ARM
board, a Raspberry Pi, a PC), built and deployed from a Mac. Set up this
way, the server fetches Oura data within about a minute of each ring sync
(Oura webhooks, through Tailscale Funnel; see *Oura webhooks*), with a
catch-up fetch every few hours, and serves the charts at
**`http://<server>:8090`**.

The examples use SSH user `you` and the server at `192.168.1.20`; use
your own.

| | |
|---|---|
| SSH | `you@192.168.1.20` (set in `deploy.mk`) |
| Install directory | `/opt/wolfe-health/` (owned by the service user) |
| Service | `wolfe-health` (systemd) |
| Port | `8090` (the default; `install-service.sh --port` picks another if it's taken) |
| Build on | the Mac, in `~/git/wolfe-health` |

> **Only the server fetches.** Oura refresh tokens are single-use, so two
> fetchers sharing a token break each other. Keep the Mac's scheduled fetch
> off, and stop the service before any manual fetch on the server.

---

## Settings

Nothing about a particular server is hard-coded; each machine's details are
settings, set in one of two places:

- **On the Mac**, in `deploy.mk` (gitignored; copy `deploy.mk.example`):
  `DEPLOY_HOST` (SSH destination, e.g. `you@192.168.1.20`), `DEPLOY_DIR`
  (default `/opt/wolfe-health`), and `TARGET_ARCH`/`TARGET_ARM` for the
  server's CPU (`arm64` for a 64-bit ARM board or Pi, `amd64` for a PC).
- **On the server**, as options to `install-service.sh`: `--user` (default:
  whoever runs it with sudo), `--port` (default 8090), `--tz` (default: the
  system's), `--args` (extra daemon flags, e.g. `"-trmnl-bits 1"`). Rerun it
  any time to change them; `--dry-run` prints the unit without installing.

---

## Update to a new version

**Mac:**

```bash
cd ~/git/wolfe-health
make test && make deploy
```

`make deploy` builds for the server's CPU, copies the binary over, keeps the
old one as `wolfe-health.prev`, restarts the service and shows its log. It
asks for the SSH password (twice: copy, then restart). Then reload
`http://192.168.1.20:8090` and check "Last fetch" isn't red.

### Roll back

```bash
make rollback
```

### If the update also changed…

- **the service template or its settings:** copy `scripts/install-service.sh`
  and `scripts/wolfe-health.service.in` to the server's install directory and
  rerun `sudo ./install-service.sh` there (with the same options as before).
  It also enables `chrony-wait`, which holds the daemon's start until the
  clock is set (most small boards have no battery clock).
- **what data is stored:** backfill once while the service is stopped (see *Manual fetch*).
- **the Oura scopes:** replace the token (below).

New database tables need nothing; the app creates them on startup. A
changed table: drop just that one (see *Back up and restore*), never the
whole database.

---

## First-time setup

**Mac** — build a bundle (binary, `.env`, installer, plus the token and a
snapshot of your history if present), hand off the token, and copy it over:

```bash
cd ~/git/wolfe-health
make unschedule        # the Mac stops fetching: one fetcher per token
make bundle
scp -r bin/deploy you@192.168.1.20:/tmp/wolfe-health
```

No `oura-token.json`? Run `make auth` first. On a server where port 8080 is
free you can instead log in there over an SSH tunnel; where something else
uses 8080, log in on the Mac, or use `wolfe-health auth -port N` after
registering `http://localhost:N/callback` on the Oura app.

**Server:**

```bash
sudo mkdir -p /opt/wolfe-health
sudo cp -a /tmp/wolfe-health/. /opt/wolfe-health/ && rm -rf /tmp/wolfe-health
cd /opt/wolfe-health
sudo ./install-service.sh --tz America/Los_Angeles
```

The installer runs the daemon as the account you're logged in as (here
`you`; `--user` to choose another), sets ownership and permissions,
installs and starts the service. Expect `serving on :8090` and then a
`fetched …` line in `journalctl -u wolfe-health -n 20`. Once the page loads,
delete the Mac's copies of the token: `rm oura-token.json bin/deploy/oura-token.json`.

**Pin the DNS servers, if your router misbehaves.** Some home routers,
right after a power cut, hand out `127.0.0.1` as the DNS server for a
while. A server that runs no resolver of its own then fails every lookup,
and chrony can't set the clock either (it needs DNS to find its time
servers). If you see that, tell dhclient to ignore the router's choice,
with your router and a public resolver (here `192.168.1.1` and `1.1.1.1`):

```bash
echo 'supersede domain-name-servers 192.168.1.1, 1.1.1.1;' | sudo tee -a /etc/dhcp/dhclient.conf
```

It takes effect at the next lease renewal; to apply it now, write the same
two servers into `/etc/resolv.conf`.

---

## Pair a TRMNL display

1. Hold the TRMNL's button for 5 seconds; it shows setup instructions.
2. From a phone, join the **TRMNL** Wi-Fi network.
3. Choose **Custom server** and enter `http://192.168.1.20:8090`.
4. Choose your home Wi-Fi and enter its password.

The panel shows the Wolfe Health mark with a device ID, then its first screen
within the refresh interval (15 minutes). The index page's **Displays** section
lists it with battery, signal and last update.

What the panel shows, and when, is set under **Display schedule** on the
index page; changes take effect at the device's next wake.

If images don't appear (older firmware can't show 4-gray PNGs), switch to
black and white: rerun the installer with the flag:
`cd /opt/wolfe-health && sudo ./install-service.sh --tz America/Los_Angeles --args "-trmnl-bits 1"`.

---

## Oura webhooks (Tailscale Funnel)

With webhooks, new data arrives about a minute after the ring syncs,
instead of at the next poll. Oura must reach the server from the internet,
so Tailscale Funnel exposes **one** port: the webhook listener, which serves
only `/oura/webhook`. The charts on 8090 stay private.

**Before you start:** deploy a build with webhook support (`make deploy`; an
older binary refuses `-webhook-listen` and the service won't start).

**0. Put the server on your tailnet** (on the server): install Tailscale
(see tailscale.com/download/linux), then

```bash
sudo tailscale up
sudo tailscale set --accept-dns=false
getent hosts api.ouraring.com        # must print an address
```

**Turn off Tailscale's DNS (`--accept-dns=false`); this matters.** By default
Tailscale rewrites `/etc/resolv.conf` to its own resolver (100.100.100.100),
and on some systems lookups then time out. Every Oura call fails, ordinary
fetches included, with `lookup api.ouraring.com: i/o timeout`. Funnel
doesn't need Tailscale's DNS, so turning it off loses nothing.

**1. Find the server's Tailscale name** (on the server):

```bash
tailscale status --self --json | grep -m1 DNSName   # e.g. "wolfe-health.tail1234.ts.net."
```

Use it **without the trailing dot**, and don't add another `.ts.net`: the
URL is `https://wolfe-health.tail1234.ts.net/oura/webhook`. A mistyped name
installs fine but every subscription then fails verification.

**2. Turn on Funnel for the webhook port** (on the server). The first time,
it prints a link to enable HTTPS certificates and Funnel for the tailnet in
the admin console; open it, approve, and run it again:

```bash
sudo tailscale funnel --bg 8091
tailscale funnel status   # https://<name>.ts.net (Funnel on) -> http://127.0.0.1:8091
```

**3. Add the webhook flags to the service** (on the server), keeping
whatever `--tz` and `--args` it has (`systemctl cat wolfe-health | grep
ExecStart` shows them):

```bash
cd /opt/wolfe-health
sudo ./install-service.sh --tz America/Los_Angeles \
  --args "-every 6h -webhook-listen 127.0.0.1:8091 -webhook-url https://<name>.ts.net/oura/webhook"
```

If `sudo` answers `./install-service.sh: command not found`, the installer
is missing or not executable (`make deploy` copies only the binary). Run it
as `sudo bash ./install-service.sh ...`, or copy it and its template over
first: `scp scripts/install-service.sh scripts/wolfe-health.service.in
you@192.168.1.20:/tmp/`, then `sudo cp` both into `/opt/wolfe-health`.

The listener binds to 127.0.0.1, so only Funnel reaches it. On start the
daemon subscribes the URL to every data type it stores (26 subscriptions)
and renews them daily as needed; `journalctl -u wolfe-health | grep webhook`
shows how that went, and so does the index page.

**The first try may only partly work.** Tailscale fetches the Funnel
address's HTTPS certificate on first use, which takes a minute or so, and
Oura's verification calls in that window fail: the index page shows some
subscriptions made and the rest with `Callback verification failed`. The
daemon retries within the hour; to finish at once, `sudo systemctl restart
wolfe-health` and look for `26 subscriptions (... created, 0 renewed)` with
no errors. Subscriptions last about 90 days and are renewed a week before
they lapse.

**Is it safe?** Only the webhook port is public. It refuses verification
without the stored token and any notification not signed with the app's
client secret, and answers 404 to everything else; the charts, schedule
form, SSH and anything else on the server stay off the internet. The address will be found
(certificates are published in transparency logs), so expect stray 401s and
404s. Keep Tailscale updated: `sudo tailscale set --auto-update`.

**Check it:** sync the ring in the Oura app; within a minute or two the log
shows `webhook: update ...; fetching shortly` and a fetch, and the index
page's "last notification" updates.

**Turn it off:** `sudo tailscale funnel --https=443 off`, rerun
`install-service.sh` without the webhook flags, and remove the subscriptions
with `./wolfe-health webhooks delete all` (with the service's `.env`
loaded, as for a manual fetch).

## Replace the token

When the page shows a red token or refresh error, or after adding a scope:

```bash
# Mac
make auth
scp oura-token.json you@192.168.1.20:/tmp/ && rm oura-token.json

# Server
sudo install -o "$(stat -c %U /opt/wolfe-health)" -m 600 /tmp/oura-token.json /opt/wolfe-health/
rm /tmp/oura-token.json
```

It's picked up on the next fetch; `sudo systemctl restart wolfe-health` fetches now.

---

## Manual fetch

```bash
sudo systemctl stop wolfe-health
cd /opt/wolfe-health
sudo -u "$(stat -c %U .)" bash -c 'set -a; . ./.env; set +a; WH_DAYS=3650 ./wolfe-health'
sudo systemctl start wolfe-health
```

`WH_DAYS=3650` backfills everything; use a small number for a quick test.

---

## Day to day (on the server)

| | |
|---|---|
| Follow the log | `journalctl -u wolfe-health -f` |
| Status | `systemctl status wolfe-health --no-pager` |
| Restart (and fetch now) | `sudo systemctl restart wolfe-health` |
| Fetch status from anywhere | `curl -s http://192.168.1.20:8090/status` |
| Copy the habit log to the Mac | `curl -s -o habits.csv http://192.168.1.20:8090/log/export.csv` |
| Copy today's snapshot to the Mac | `scp you@192.168.1.20:/opt/wolfe-health/backups/wolfe-health-$(date +%F).db .` |

## Back up and restore

The daemon writes a snapshot of the database each day to
`/opt/wolfe-health/backups/` and keeps 14. The habit log, settings and
display pairings live only in the database, so:

```sh
# roll back to a snapshot
sudo systemctl stop wolfe-health
cp /opt/wolfe-health/backups/wolfe-health-2026-11-20.db /opt/wolfe-health/wolfe-health.db
rm -f /opt/wolfe-health/wolfe-health.db-wal /opt/wolfe-health/wolfe-health.db-shm
sudo systemctl start wolfe-health

# put habit logs back from a saved CSV (adds only what's missing)
cd /opt/wolfe-health && ./wolfe-health habits import ~/habits.csv

# a changed Oura table: drop just it (Oura's tables only), and the restart refetches a year
sudo systemctl stop wolfe-health
cd /opt/wolfe-health && ./wolfe-health reset sleep_periods
sudo systemctl start wolfe-health
```

---

## Troubleshooting

| Symptom | Fix |
|---|---|
| Page won't load | `systemctl status wolfe-health`; check the log |
| `certificate signed by unknown authority` | `sudo apt install ca-certificates`, restart |
| `lookup api.ouraring.com` / `dial tcp` | No DNS. `on 127.0.0.1:53 … connection refused` means `/etc/resolv.conf` says `nameserver 127.0.0.1` (the router handed that out after a power cut): `printf 'nameserver 192.168.1.1\nnameserver 1.1.1.1\n' \| sudo tee /etc/resolv.conf` (your router and a public resolver), `sudo systemctl restart chrony`, restart wolfe-health, and pin the servers (*First-time setup*). If it says "generated by tailscale", run `sudo tailscale set --accept-dns=false` and restart. Otherwise the server has no internet; check its network |
| Webhooks: `Callback verification failed` | Oura couldn't reach the URL: check `tailscale funnel status` (Funnel on, to `127.0.0.1:8091`) and the URL's spelling, then `sudo systemctl restart wolfe-health`. Right after first enabling Funnel, it's the certificate warming up; restart once |
| Webhooks: `flag provided but not defined: -webhook-listen` | the old binary; `make deploy` |
| Reaching the server by its tailnet name or 100.x address hangs, though Funnel works | something on the server has taken over `tailscale0` (moved it into a VRF, or removed its address); Funnel still works because `tailscaled` handles it itself. `ip -br addr show tailscale0` should show the 100.x address, and `ip link show tailscale0` no `vrf`. Stop whatever is claiming it, then `sudo tailscale down && sudo tailscale up` |
| `no Oura token yet` or a refresh error | *Replace the token*; make sure nothing else fetches |
| `permission denied` | rerun `sudo ./install-service.sh` in `/opt/wolfe-health` with your usual options; it resets ownership to the service user |
| Wrong "today" late in the evening | check `Environment=TZ=` in the service file |
| After a reboot: `x509: certificate has expired or is not yet valid: current time 2026-04-27…` | A board with no battery clock boots with a stale time until chrony sets it a few seconds later. The daemon retries within a minute, so this clears by itself; it can only stick on a build from before Nov 23, 2026, which waits the whole interval (`sudo systemctl restart wolfe-health`). Reinstalling the service (`sudo ./install-service.sh`, same options) also enables `chrony-wait`, so the daemon starts only once the clock is set. `timedatectl` says whether it is; `journalctl -u chrony \| grep stepped` shows each boot's correction |
