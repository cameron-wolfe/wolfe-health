#!/usr/bin/env bash
# release.sh vX.Y.Z — tag a release of what's on main, and push the tag.
#
# It refuses unless everything a release should be is true: the version is
# vMAJOR.MINOR.PATCH and newer than the last release, CHANGELOG.md has a
# section for it, the working tree is clean, main matches GitHub, and vet
# and the tests pass. Then it makes an annotated tag and pushes it. The
# GitHub release page (notes from CHANGELOG.md) is a step after, by hand.
#
# Run from the repository: make release VERSION=v1.0.1
set -euo pipefail

v="${1:-}"
fail() { echo "release: $*" >&2; exit 1; }

[[ "$v" =~ ^v[0-9]+\.[0-9]+\.[0-9]+$ ]] || fail "want a version like v1.2.3, got '${v}'"
git rev-parse --verify --quiet "refs/tags/$v" >/dev/null && fail "$v is already tagged"

# newer than the newest release so far
last="$(git tag --list 'v[0-9]*' | sort -V | tail -1)"
if [[ -n "$last" && "$(printf '%s\n%s\n' "$last" "$v" | sort -V | tail -1)" != "$v" ]]; then
	fail "$v isn't newer than the last release, $last"
fi

grep -q "^## $v\b" CHANGELOG.md || fail "CHANGELOG.md has no '## $v' section: say what's in it first"

[[ "$(git rev-parse --abbrev-ref HEAD)" == main ]] || fail "not on main"
[[ -z "$(git status --porcelain)" ]] || fail "the working tree isn't clean: commit or stash first"
git fetch --quiet origin main
[[ "$(git rev-parse HEAD)" == "$(git rev-parse origin/main)" ]] || fail "main differs from GitHub's: push or pull first"

echo "release: vet and tests…"
go vet ./... && go test ./... >/dev/null || fail "vet or tests failed"

git tag -a "$v" -m "wolfe-health ${v#v}"
git push origin "$v"
echo "release: tagged and pushed $v ($(git rev-parse --short HEAD))"
echo "release: next, the release page: https://github.com/cameron-wolfe/wolfe-health/releases/new?tag=$v (notes from CHANGELOG.md)"
