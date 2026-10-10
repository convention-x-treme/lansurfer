#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
users="${1:-20}"
duration="${2:-1m}"
sessions="${LANSURFER_LOADTEST_SESSIONS_FILE:-$HOME/lansurfer-loadtest-sessions.jsonl}"
reports="${LANSURFER_LOADTEST_REPORT_DIR:-$HOME/lansurfer-loadtest-reports}"

if ! [[ "$users" =~ ^[1-9][0-9]*$ ]] || (( users > 2000 )); then
  echo 'USERS must be an integer between 1 and 2000' >&2
  exit 2
fi
if ! [[ "$duration" =~ ^[1-9][0-9]*(s|m|h)$ ]]; then
  echo 'DURATION must be a positive duration such as 30s, 2m, or 1h' >&2
  exit 2
fi
if [[ ! -f "$sessions" || ! -r "$sessions" ]]; then
  echo "Cannot read session file: $sessions" >&2
  exit 2
fi
if [[ ! -f "$root/tools/loadtest/http.js" ]]; then
  echo 'Load-test script is missing; run from the feature branch' >&2
  exit 2
fi
# Only connect to the private test-stack network, never a public URL.
if ! docker network inspect lansurfer-loadtest_default >/dev/null 2>&1; then
  echo 'Load-test stack network is absent. Start the isolated Compose stack first.' >&2
  exit 2
fi

mkdir -p "$reports"
output="http-$(date -u +%Y%m%dT%H%M%SZ)-${users}users.json"
echo "Read-only HTTP profile: $users virtual players for $duration"
echo "Result: $reports/$output"
# The session file is mode 0600. Match the host UID to read it in Docker.
docker run --rm --user "$(id -u):$(id -g)" \
  --network lansurfer-loadtest_default \
  --mount "type=bind,src=$(realpath "$sessions"),dst=/sessions.jsonl,readonly" \
  --mount "type=bind,src=$root/tools/loadtest/http.js,dst=/work/http.js,readonly" \
  --mount "type=bind,src=$(realpath "$reports"),dst=/reports" \
  -e "USERS=$users" -e "DURATION=$duration" \
  grafana/k6:1.8.1 run --summary-export "/reports/$output" /work/http.js
