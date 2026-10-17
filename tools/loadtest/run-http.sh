#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
users="${1:-20}"
duration="${2:-1m}"
profile="${3:-http}"
fixtures="${LANSURFER_LOADTEST_FIXTURES_FILE:-$HOME/lansurfer-loadtest-fixtures.json}"
sessions="${LANSURFER_LOADTEST_SESSIONS_FILE:-$HOME/lansurfer-loadtest-sessions.jsonl}"
reports="${LANSURFER_LOADTEST_REPORT_DIR:-$HOME/lansurfer-loadtest-reports}"
base_url="${BASE_URL:-http://lansurfer:8080}"
loadtest_network="${LOADTEST_NETWORK:-lansurfer-loadtest_default}"
workload="${WORKLOAD:-writes}"
session_offset="${SESSION_OFFSET:-0}"
seed="${SEED:-139}"

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
session_count="$(wc -l < "$sessions")"
if [[ "$profile" == writes && -z "${SESSION_OFFSET+x}" ]]; then
  session_offset=5
fi
if ! [[ "$session_offset" =~ ^[0-9]+$ ]] || (( session_offset + users > session_count )); then
  echo "SESSION_OFFSET plus USERS exceeds available sessions" >&2
  exit 2
fi
if [[ "$base_url" != http://lansurfer:8080 && "$base_url" != http://proxy:8080 ]]; then
  echo 'BASE_URL must be http://lansurfer:8080 or http://proxy:8080' >&2
  exit 2
fi
if ! [[ "$seed" =~ ^[0-9]+$ ]]; then
  echo 'SEED must be a non-negative integer' >&2
  exit 2
fi
if [[ "$profile" != http && "$profile" != writes ]]; then
  echo 'PROFILE must be http or writes' >&2
  exit 2
fi
if [[ ! -f "$root/tools/loadtest/$profile.js" ]]; then
  echo 'Load-test script is missing; run from the feature branch' >&2
  exit 2
fi
mounts=()
if [[ "$profile" == http ]]; then
  if [[ ! -f "$fixtures" || ! -r "$fixtures" ]]; then
    echo "Cannot read fixtures file (run task loadtest:fixtures first): $fixtures" >&2
    exit 2
  fi
  mounts+=(--mount "type=bind,src=$(realpath "$fixtures"),dst=/fixtures.json,readonly")
fi
if [[ "$profile" == writes ]]; then
  if [[ ! -f "$fixtures" || ! -r "$fixtures" ]]; then
    echo "Cannot read fixtures file (run task loadtest:fixtures first): $fixtures" >&2
    exit 2
  fi
  mounts+=(--mount "type=bind,src=$(realpath "$fixtures"),dst=/fixtures.json,readonly")
  mounts+=(--mount "type=bind,src=$root/tools/loadtest/outcomes.mjs,dst=/work/outcomes.mjs,readonly")
fi
# Only connect to the private test-stack network, never a public URL.
if ! docker network inspect "$loadtest_network" >/dev/null 2>&1; then
  echo "Load-test stack network is absent: $loadtest_network" >&2
  exit 2
fi

umask 077
mkdir -p "$reports"
output="$profile-$(date -u +%Y%m%dT%H%M%SZ)-${users}users.json"
echo "$profile HTTP profile: $users virtual players for $duration"
echo "Result: $reports/$output"
# The session file is mode 0600. Match the host UID to read it in Docker.
docker run --rm --user "$(id -u):$(id -g)" \
  --network "$loadtest_network" \
  --label com.lansurfer.loadtest.role=k6 \
  --mount "type=bind,src=$(realpath "$sessions"),dst=/sessions.jsonl,readonly" \
  --mount "type=bind,src=$root/tools/loadtest/$profile.js,dst=/work/script.js,readonly" \
  --mount "type=bind,src=$(realpath "$reports"),dst=/reports" \
  "${mounts[@]}" \
-e "USERS=$users" -e "DURATION=$duration" -e "BASE_URL=$base_url" -e "SESSION_OFFSET=$session_offset" -e "SEED=$seed" -e "WORKLOAD=$workload" \
  grafana/k6:1.8.1 run --summary-export="/reports/$output" /work/script.js
