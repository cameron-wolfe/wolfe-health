#!/bin/sh
# Scheduled fetch: load .env and run the prebuilt binary from the repo root.
#
# Used by the launchd agent on macOS (make schedule) and by cron on the Pi.
# Schedulers run with a minimal environment and no Go toolchain on PATH, so
# this runs bin/wolfe-health as built by `make build` rather than `make run`.
# Rebuild after pulling changes, or the schedule keeps running the old binary.
set -eu

# repo root = the directory above this script, wherever the scheduler starts us
cd "$(dirname "$0")/.."

if [ ! -x bin/wolfe-health ]; then
	echo "$(date '+%F %T') bin/wolfe-health missing: run make build" >&2
	exit 1
fi

set -a
. ./.env
set +a

# Recent days only: a scheduled run is catching new data and Oura's revisions
# to the last few days, not backfilling. .env can override.
: "${WH_DAYS:=7}"
export WH_DAYS

echo "=== $(date '+%F %T')"
exec ./bin/wolfe-health
