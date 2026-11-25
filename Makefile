BIN := wolfe-health
PKG := ./cmd/wolfe-health
OUT := bin

# Pure-Go only: with CGO disabled, cross-compiling to the Pi needs no toolchain
# beyond the Go distribution itself. This is why the SQLite driver is
# modernc.org/sqlite and not mattn/go-sqlite3.
export CGO_ENABLED := 0

# Each build is stamped: Go records the git commit, its time and whether the
# tree had uncommitted changes; this adds when it was built (internal/version).
# Shown in the daemon's start log, on the index page, at /status, and by
# `wolfe-health version`.
# The release comes from the newest v* tag (git describe): "v1.0.0" on the
# tag's commit, "v1.0.0-3-gabc1234" after it; empty before the first tag.
STAMP := -X github.com/cameron-wolfe/wolfe-health/internal/version.BuildTime=$(shell date -u +%Y-%m-%dT%H:%M:%SZ) \
	-X github.com/cameron-wolfe/wolfe-health/internal/version.Release=$(shell git describe --tags --match 'v[0-9]*' --dirty 2>/dev/null)

.PHONY: release all build run auth daemon chart daily night schedule unschedule logs pi arm64 linux bundle deploy rollback emulator shortcut decode test vet fmt clean

all: build

## build: host binary (macOS dev)
build:
	go build -ldflags="$(STAMP)" -o $(OUT)/$(BIN) $(PKG)

## run: fetch and store, loading the Oura app credentials from .env
run: build
	set -a; . ./.env; set +a; ./$(OUT)/$(BIN)

## auth: one-time browser login to Oura; writes oura-token.json
auth: build
	set -a; . ./.env; set +a; ./$(OUT)/$(BIN) auth

## daemon: fetch hourly and serve charts on :8090 (http://localhost:8090)
daemon: build
	set -a; . ./.env; set +a; ./$(OUT)/$(BIN) daemon

## chart: render the trends screen to charts/trends.png (no Oura access needed)
chart: build
	./$(OUT)/$(BIN) chart -view trends

## daily: render today's overview to charts/daily.png
daily: build
	./$(OUT)/$(BIN) chart -view daily

## night: render last night's sleep stages to charts/night.png
night: build
	./$(OUT)/$(BIN) chart -view night

# launchd agent for scheduled fetches on macOS (on the Pi, use cron; see README)
AGENT := com.wolfe-health.fetch
PLIST := $(HOME)/Library/LaunchAgents/$(AGENT).plist
LOG   := $(HOME)/Library/Logs/wolfe-health.log

## schedule: fetch every 3 hours via launchd (macOS); re-run after pulling to reinstall
schedule: build
	mkdir -p $(dir $(PLIST))
	sed -e 's|@REPO@|$(CURDIR)|g' -e 's|@LOG@|$(LOG)|g' scripts/$(AGENT).plist.in > $(PLIST)
	launchctl bootout gui/$$(id -u)/$(AGENT) 2>/dev/null || true
	launchctl bootstrap gui/$$(id -u) $(PLIST)
	launchctl kickstart gui/$$(id -u)/$(AGENT)
	@echo "scheduled; first run started now. Log: $(LOG)"

## unschedule: remove the launchd agent
unschedule:
	launchctl bootout gui/$$(id -u)/$(AGENT) 2>/dev/null || true
	rm -f $(PLIST)

## logs: show recent scheduled-fetch output
logs:
	tail -n 40 $(LOG)

## pi: Raspberry Pi 3/4/5 on 64-bit Raspberry Pi OS
pi:
	GOOS=linux GOARCH=arm64 go build -ldflags="$(STAMP)" -o $(OUT)/$(BIN)-linux-arm64 $(PKG)

## arm64: stripped linux/arm64 build for a server (a 64-bit ARM board or Pi)
arm64:
	GOOS=linux GOARCH=arm64 go build -ldflags="-s -w $(STAMP)" -o $(OUT)/$(BIN)-linux-arm64 $(PKG)

# --- deploying to a server ---------------------------------------------------
# Where and what to deploy is per machine, so it lives in deploy.mk (copy
# deploy.mk.example; it's gitignored), or on the command line:
#   make deploy DEPLOY_HOST=you@192.168.1.20
-include deploy.mk
DEPLOY_HOST ?=
DEPLOY_DIR  ?= /opt/wolfe-health
TARGET_ARCH ?= arm64
TARGET_ARM  ?=
TARGET_BIN   = $(OUT)/$(BIN)-linux-$(TARGET_ARCH)$(TARGET_ARM)

## linux: stripped Linux build for TARGET_ARCH (arm64; arm with TARGET_ARM=6|7 for 32-bit Pis; amd64)
linux:
	GOOS=linux GOARCH=$(TARGET_ARCH) GOARM=$(TARGET_ARM) go build -ldflags="-s -w $(STAMP)" -o $(TARGET_BIN) $(PKG)

## bundle: gather a first install in bin/deploy (binary, .env, service template and installer; token and history if present)
bundle: linux
	rm -rf $(OUT)/deploy && mkdir -p $(OUT)/deploy
	cp $(TARGET_BIN) $(OUT)/deploy/wolfe-health
	cp .env scripts/install-service.sh scripts/wolfe-health.service.in $(OUT)/deploy/
	@if [ -f oura-token.json ]; then cp oura-token.json $(OUT)/deploy/ && echo "included oura-token.json: delete the local copy once the server works (one fetcher per token)"; fi
	@if [ -f wolfe-health.db ]; then sqlite3 wolfe-health.db "VACUUM INTO '$(OUT)/deploy/wolfe-health.db'" && echo "included a snapshot of wolfe-health.db"; fi
	chmod 600 $(OUT)/deploy/.env
	@echo "bundle ready: scp -r $(OUT)/deploy <host>:/tmp/wolfe-health, then see docs/deploy-linux.md"

## deploy: build, copy to DEPLOY_HOST, swap in (keeping wolfe-health.prev) and restart
deploy: linux
	@test -n "$(DEPLOY_HOST)" || { echo "set DEPLOY_HOST in deploy.mk (see deploy.mk.example) or on the command line"; exit 1; }
	scp $(TARGET_BIN) $(DEPLOY_HOST):/tmp/wolfe-health.new
	ssh -t $(DEPLOY_HOST) 'set -e; d=$(DEPLOY_DIR); \
		owner=$$(stat -c %U:%G $$d); \
		sudo systemctl stop wolfe-health; \
		sudo cp -a $$d/wolfe-health $$d/wolfe-health.prev; \
		sudo install -m 755 -o $${owner%:*} -g $${owner#*:} /tmp/wolfe-health.new $$d/wolfe-health; \
		rm /tmp/wolfe-health.new; \
		sudo systemctl start wolfe-health; \
		sleep 3; journalctl -u wolfe-health -n 6 --no-pager'

## rollback: restore the previous binary on DEPLOY_HOST
rollback:
	@test -n "$(DEPLOY_HOST)" || { echo "set DEPLOY_HOST"; exit 1; }
	ssh -t $(DEPLOY_HOST) 'set -e; d=$(DEPLOY_DIR); sudo systemctl stop wolfe-health; \
		sudo cp -a $$d/wolfe-health.prev $$d/wolfe-health; sudo systemctl start wolfe-health; \
		sleep 3; journalctl -u wolfe-health -n 6 --no-pager'

## emulator: open the display emulator in its own window (macOS), from DEPLOY_HOST's server or EMULATOR_URL
SERVER_PORT  ?= 8090
EMULATOR_URL ?= http://$(lastword $(subst @, ,$(DEPLOY_HOST))):$(SERVER_PORT)/emulator
emulator:
	@test -n "$(DEPLOY_HOST)$(filter-out http://:%,$(EMULATOR_URL))" || { echo "set DEPLOY_HOST or EMULATOR_URL"; exit 1; }
	@if [ -d "/Applications/Google Chrome.app" ]; then \
		open -na "Google Chrome" --args --app="$(EMULATOR_URL)" --window-size=960,660 --user-data-dir="$$HOME/Library/Application Support/wolfe-health-emulator"; \
	else \
		open -a Safari "$(EMULATOR_URL)"; echo "In Safari, File > Add to Dock makes it a window of its own"; \
	fi

# The Siri shortcuts in docs/shortcuts: each .json is the source, a readable
# spec of the shortcut's steps. scripts/shortcut.py builds it into the
# readable .plist, checks it, then signs that into the importable .shortcut
# and verifies the signed copy matches (macOS, signed in to iCloud; phones
# refuse unsigned shortcuts). See scripts/shortcut.py for the spec format.
# The reverse: a shortcut (signed or not, from this repo or exported from a
# phone) back to a spec, to read or to edit and rebuild. Writes
# Name.decoded.json beside it unless SPEC names another file, and never
# overwrites one, so it can't clobber a hand-written spec.
#   make decode SHORTCUT="docs/shortcuts/How did I sleep.shortcut"
#   make decode SHORTCUT=~/Downloads/Mine.shortcut SPEC=docs/shortcuts/Mine.json
decode:
	@test -n "$(SHORTCUT)" || { echo 'usage: make decode SHORTCUT="path/Name.shortcut" [SPEC=out.json]'; exit 2; }
	@in="$(SHORTCUT)"; out="$(SPEC)"; [ -n "$$out" ] || out="$${in%.*}.decoded.json"; \
	python3 scripts/shortcut.py spec "$$in" "$$out"

# Only a shortcut whose spec (or the builder) is newer than its signed file is
# rebuilt: step IDs come from the spec, so an unchanged one builds the same
# plist, but every signing makes a new .shortcut. Done in the shell rather
# than as make rules because shortcut names have spaces ("How did I sleep").
shortcut:
	@for spec in docs/shortcuts/*.json; do \
		base="$${spec%.json}"; \
		if [ -e "$$base.plist" ] && [ -e "$$base.shortcut" ] && [ "$$base.shortcut" -nt "$$spec" ] && [ "$$base.shortcut" -nt scripts/shortcut.py ]; then \
			echo "up to date: $$base.shortcut"; continue; fi; \
		python3 scripts/shortcut.py build "$$spec" "$$base.plist" && \
		python3 scripts/shortcut.py sign "$$base.plist" "$$base.shortcut" && \
		chmod 644 "$$base.shortcut" || exit 1; \
	done

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -l -w .

clean:
	rm -rf $(OUT)

## release: tag what's on main as VERSION and push the tag, after checking
## it's ready (clean, pushed, CHANGELOG.md section, vet and tests pass):
##   make release VERSION=v1.0.1
release:
	@test -n "$(VERSION)" || { echo "usage: make release VERSION=vX.Y.Z"; exit 2; }
	@./scripts/release.sh "$(VERSION)"
