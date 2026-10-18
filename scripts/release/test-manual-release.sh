#!/usr/bin/env bash
# Offline release-selection regression; no API writes or credentials.
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
dir="$(mktemp -d)"
trap 'rm -rf "$dir"' EXIT
cd "$dir"
git init -q
git config user.name "Release Selector Test"
git config user.email "release-test@example.invalid"
git -c commit.gpgsign=false commit --allow-empty -qm "Fixture"
plan() { bash "$root/scripts/release/manual_release.sh" plan "$@"; }
rejected() {
  if plan "$@" >/dev/null 2>&1; then
    echo "Input should be rejected: $*" >&2
    exit 1
  fi
}
[[ "$(plan 0.2.0 rc)" == v0.2.0-rc.1 ]]
[[ "$(plan 0.2.0 stable)" == v0.2.0 ]]
git -c tag.gpgsign=false tag -a v0.2.0-rc.1 -m "RC1"
git -c tag.gpgsign=false tag -a v0.2.0-rc.2 -m "RC2"
[[ "$(plan 0.2.0 rc)" == v0.2.0-rc.3 ]]
[[ "$(plan 0.3.0 rc)" == v0.3.0-rc.1 ]]
rejected v0.2.0 rc
rejected 0.2 rc
rejected 0.02.0 rc
rejected 0.2.0-rc.4 rc
rejected 0.2.0 alpha
rejected 0.2.0 'rc;false'
git -c tag.gpgsign=false tag -a v0.2.0 -m Stable
rejected 0.2.0 rc
rejected 0.2.0 stable
if bash "$root/scripts/release/manual_release.sh" create v0.3.0 "$(git rev-parse HEAD)" >/dev/null 2>&1; then
  echo "Tag creation succeeded outside the privileged manual workflow" >&2
  exit 1
fi
echo "Manual release planner and guard tests passed."
