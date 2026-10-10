#!/usr/bin/env bash
set -euo pipefail

users="${1:-120}"
duration="${2:-2h}"
browsers="${BROWSERS:-5}"
sessions="${LANSURFER_LOADTEST_SESSIONS_FILE:-$HOME/lansurfer-loadtest-sessions.jsonl}"
reports="${LANSURFER_LOADTEST_REPORT_DIR:-$HOME/lansurfer-loadtest-reports}"
metrics_key="$HOME/lansurfer-loadtest-metrics-key"
control_file="$HOME/lansurfer-loadtest-control.json"

if ! [[ "$users" =~ ^[1-9][0-9]*$ ]] || (( users > 400 )); then
  echo 'USERS must be an integer between 1 and 400' >&2
  exit 2
fi
if ! [[ "$browsers" =~ ^[0-9]+$ ]] || (( browsers >= users )); then
  echo 'BROWSERS must be an integer between 0 and USERS-1' >&2
  exit 2
fi
writers=$((users - browsers))
if ! [[ "$duration" =~ ^[1-9][0-9]*(s|m|h)$ ]]; then
  echo 'DURATION must be a positive duration such as 30s, 2m, or 2h' >&2
  exit 2
fi
if [[ ! -f "$sessions" || ! -r "$sessions" || ! -f "$metrics_key" || ! -r "$metrics_key" ]]; then
  echo 'Seed sessions and run task loadtest:prepare before soak' >&2
  exit 2
fi
session_count="$(wc -l < "$sessions")"
if (( users > session_count )); then
  echo 'USERS exceeds available seeded sessions' >&2
  exit 2
fi
bash tools/loadtest/guard-compose.sh

umask 077
mkdir -p "$reports"
stamp="$(date -u +%Y%m%dT%H%M%SZ)"
http_log="$reports/soak-http-$stamp.log"
sse_log="$reports/soak-sse-$stamp.log"
audit_output="$reports/soak-audit-$stamp.json"
audit_log="$reports/soak-audit-$stamp.log"
pids=()
cleanup() {
  status=$?
  trap - EXIT INT TERM
  for pid in "${pids[@]}"; do
    kill -TERM -- "-$pid" 2>/dev/null || true
  done
  for pid in "${pids[@]}"; do
    wait "$pid" 2>/dev/null || true
  done
  exit "$status"
}
trap cleanup EXIT INT TERM

setsid env SESSION_OFFSET="$browsers" WORKLOAD=soak bash tools/loadtest/run-http.sh "$writers" "$duration" writes >"$http_log" 2>&1 &
pids+=("$!")
setsid go run ./tools/loadtest/sse -clients "$writers" -duration "$duration" -refresh-mode browser-like -refresh-query 'q=Warcraft+III' -sessions "$sessions" -base-url http://127.0.0.1:18080 -session-offset "$browsers" >"$sse_log" 2>&1 &
pids+=("$!")
request_id="$(node -p "JSON.parse(require('fs').readFileSync(process.argv[1], 'utf8')).request_id" "$control_file")"
browser_log="$reports/soak-browsers-$stamp.log"
setsid env BROWSER_ONLY=1 BASE_URL=http://127.0.0.1:18080 REQUEST="$request_id" SIMULATED_USERS="$writers" BROWSER_CLIENTS="$browsers" DURATION="$duration" TRIGGER=0 VIEW=discovery LANSURFER_LOADTEST_REPORT_DIR="$reports" node tools/loadtest/browser-calibration.mjs >"$browser_log" 2>&1 &
pids+=("$!")

duration_value="${duration%[smh]}"
duration_unit="${duration: -1}"
case "$duration_unit" in
  s) remaining_seconds=$duration_value ;;
  m) remaining_seconds=$((duration_value * 60)) ;;
  h) remaining_seconds=$((duration_value * 3600)) ;;
esac
window=0
failure_status=0
while (( remaining_seconds > 0 )); do
  chunk=$remaining_seconds
  if (( chunk > 600 )); then chunk=600; fi
  profile_output="$reports/soak-profile-$stamp-window-$(printf '%03d' "$window").jsonl"
  profile_log="$reports/soak-profile-$stamp-window-$(printf '%03d' "$window").log"
  if ! go run ./tools/loadtest/profile -duration "${chunk}s" -interval 5s -metrics-key-file "$metrics_key" -output "$profile_output" >"$profile_log" 2>&1; then
    echo "Profile window $window failed; see $profile_log" >&2
    if (( failure_status == 0 )); then failure_status=1; fi
  fi
  remaining_seconds=$((remaining_seconds - chunk))
  window=$((window + 1))
done

remaining=${#pids[@]}
while (( remaining > 0 )); do
  if wait -n; then
    :
  else
    status=$?
    echo "Soak child failed ($status); logs: $http_log $sse_log" >&2
    if (( failure_status == 0 )); then failure_status=$status; fi
  fi
  remaining=$((remaining - 1))
done

echo "Soak workload complete: participants=$users browsers=$browsers writers=$writers duration=$duration"
echo "Cooldown: 300s"
sleep 300
if ! go run ./tools/loadtest/state -mode audit -output "$control_file" >"$audit_output" 2>"$audit_log"; then
  echo "Soak database audit failed; see $audit_log" >&2
  if (( failure_status == 0 )); then failure_status=1; fi
fi
trap - EXIT INT TERM
if (( failure_status != 0 )); then
  echo "Soak ended with errors. Inspect logs: $http_log $sse_log $audit_log" >&2
  exit "$failure_status"
fi
echo "Soak complete. HTTP: $http_log; SSE: $sse_log; audit: $audit_output; profile windows: $window"
