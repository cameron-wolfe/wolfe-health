#!/bin/sh
# Install (or reinstall) the wolfe-health systemd service on this machine.
#
# Run it with sudo from the install directory, where the bundle was copied:
#
#   sudo ./install-service.sh [--user NAME] [--port N] [--tz ZONE] [--args "FLAGS"]
#
# Every machine-specific setting is an option, so nothing about any one
# host lives in the repo:
#
#   --user  account the daemon runs as. Default: whoever ran sudo. It must be
#           able to write the directory (token refreshes, the database).
#   --dir   install directory. Default: the directory this script is in.
#   --port  HTTP port. Default: 8090.
#   --tz    timezone for "today" on the daily screen, e.g. America/Los_Angeles.
#           Default: the system's own timezone.
#   --args  extra daemon flags, e.g. "-trmnl-bits 1".
#   --no-time-wait  don't enable the clock-sync wait service (below).
#   --dry-run  print the unit instead of installing it.
#
# It also enables the service that holds boot until the clock is set from the
# network (chrony-wait under chrony, systemd-time-wait-sync under
# systemd-timesyncd), so the daemon doesn't start on a board without a
# battery clock still thinking it's months ago.
#
# Expects in the directory: wolfe-health (the binary), .env, and
# wolfe-health.service.in (this script's template). oura-token.json and
# wolfe-health.db are optional: the daemon creates the database, and a token
# can be dropped in later without a restart.
set -eu

here=$(cd "$(dirname "$0")" && pwd)
user=${SUDO_USER:-$(id -un)}
dir=$here
port=8090
tz=
args=
dry=
timewait=1

while [ $# -gt 0 ]; do
	case $1 in
	--user) user=$2; shift 2 ;;
	--dir) dir=$2; shift 2 ;;
	--port) port=$2; shift 2 ;;
	--tz) tz=$2; shift 2 ;;
	--args) args=$2; shift 2 ;;
	--dry-run) dry=1; shift ;;
	--no-time-wait) timewait=; shift ;;
	-h|--help) sed -n '2,/^set -eu/p' "$0" | sed 's/^# \{0,1\}//;/^set -eu/d'; exit 0 ;;
	*) echo "unknown option: $1 (see --help)" >&2; exit 2 ;;
	esac
done

die() { echo "install-service: $*" >&2; exit 1; }

# validate before anything is written; values go into the unit via sed
case $user in ''|*[!A-Za-z0-9._-]*) die "bad --user '$user'" ;; esac
[ "$user" = root ] && [ -z "${ALLOW_ROOT:-}" ] && \
	die "refusing to run the daemon as root; pass --user, or set ALLOW_ROOT=1 if you mean it"
id -u "$user" >/dev/null 2>&1 || die "no such user '$user'"
case $port in ''|*[!0-9]*) die "bad --port '$port'" ;; esac
[ "$port" -ge 1 ] && [ "$port" -le 65535 ] || die "--port must be 1-65535"
case $dir in /*) ;; *) die "--dir must be an absolute path" ;; esac
case $dir$tz$args in *'|'*|*'\'*) die "settings can't contain | or \\" ;; esac
if [ -n "$tz" ] && [ ! -e "/usr/share/zoneinfo/$tz" ]; then die "unknown timezone '$tz'"; fi
for f in wolfe-health .env; do [ -e "$dir/$f" ] || die "missing $dir/$f"; done
template=$dir/wolfe-health.service.in
[ -e "$template" ] || template=$here/wolfe-health.service.in
[ -e "$template" ] || die "missing wolfe-health.service.in"

tz_line="# TZ: the system timezone applies"
[ -n "$tz" ] && tz_line="Environment=TZ=$tz"

unit=$(sed -e "s|@USER@|$user|g" -e "s|@DIR@|$dir|g" -e "s|@PORT@|$port|g" \
	-e "s|@TZ_LINE@|$tz_line|g" -e "s|@ARGS@|$args|g" "$template")

if [ -n "$dry" ]; then
	printf '%s\n' "$unit"
	exit 0
fi
[ "$(id -u)" -eq 0 ] || die "run with sudo (it writes /etc/systemd/system)"

chown -R "$user:$(id -gn "$user")" "$dir"
chmod 755 "$dir/wolfe-health"
chmod 600 "$dir/.env"
[ -e "$dir/oura-token.json" ] && chmod 600 "$dir/oura-token.json"
printf '%s\n' "$unit" > /etc/systemd/system/wolfe-health.service
# hold time-sync.target, which the service waits for, until the clock is set
waiter=
if [ -n "$timewait" ]; then
	if systemctl cat chrony-wait.service >/dev/null 2>&1; then
		waiter=chrony-wait.service
	elif systemctl cat systemd-time-wait-sync.service >/dev/null 2>&1; then
		waiter=systemd-time-wait-sync.service
	fi
	if [ -n "$waiter" ]; then
		systemctl enable "$waiter" >/dev/null 2>&1 || echo "install-service: couldn't enable $waiter" >&2
	else
		echo "install-service: no clock-sync wait service found; the daemon's fetch retries cover it" >&2
	fi
fi
systemctl daemon-reload
systemctl enable wolfe-health >/dev/null 2>&1
systemctl restart wolfe-health
echo "wolfe-health installed: runs as $user from $dir on port $port"
[ -n "$waiter" ] && echo "  starts after the clock is set ($waiter enabled)"
echo "  log:    journalctl -u wolfe-health -f"
echo "  status: systemctl status wolfe-health"
