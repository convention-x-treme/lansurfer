#!/usr/bin/env bash
set -euo pipefail
umask 077

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
workspace_name="${WORKSPACE:-}"
[[ "$workspace_name" =~ ^[A-Za-z0-9][A-Za-z0-9_-]{0,31}$ ]] || { echo 'WORKSPACE must be a 1-32 character name' >&2; exit 2; }
slug="${workspace_name,,}"
workspace_root="${HOME}/lansurfer-loadtest-coalescing"
workspace="${workspace_root}/${slug}"
compose=(docker compose -p lansurfer-loadtest -f "$root/tools/loadtest/compose.yaml")
chaos_compose=("${compose[@]}" -f "$root/tools/loadtest/compose.chaos.yaml")
database_url='postgres://lansurfer_loadtest:local-loadtest-only@127.0.0.1:55433/lansurfer_loadtest?sslmode=disable'

revision() {
  git -C "$root" rev-parse --verify "$1^{commit}"
}

build_image() {
  local rev="$1" variant="$2" short_rev="${1:0:12}"
  local archive="$workspace/source-${variant}-${short_rev}.tar" context="$workspace/build-${variant}-${short_rev}"
  local tag="lansurfer-loadtest-coalescing:${slug}-${variant}-${short_rev}"
  if [[ ! -f "$archive" ]]; then git -C "$root" archive --format=tar "$rev" >"$archive"; fi
  local digest
  digest="$(tar -xOf "$archive" internal/web/assets/dist/app.js | sha256sum | cut -d ' ' -f1)"
  if ! docker image inspect "$tag" >/dev/null 2>&1; then
    mkdir -p -m 700 "$context"
    tar -xf "$archive" -C "$context"
    docker build -t "$tag" "$context" >&2
  fi
  printf '%s %s\n' "$tag" "$digest"
}
compose_override() {
  local tag="$1" output="$2"
  printf 'services:\n  lansurfer:\n    image: %s\n' "$tag" >"$output"
}

wait_ready() {
  for _ in $(seq 1 60); do
    if curl -fsS http://127.0.0.1:18080/readyz >/dev/null; then return 0; fi
    sleep 1
  done
  echo 'isolated app did not become ready' >&2
  return 1
}
verify_bundle() {
  local expected="$1" actual
  actual="$(curl -fsS -H 'Accept-Encoding: identity' http://127.0.0.1:18080/assets/app.js | sha256sum | cut -d ' ' -f1)"
  [[ "$actual" == "$expected" ]] || {
    echo "served app.js checksum $actual does not match image bundle $expected" >&2
    return 1
  }
}

prepare() {
  local product_revision harness_revision baseline_image baseline_sha
  product_revision="$(revision "${PRODUCT_REVISION:-}")"
  harness_revision="$(revision "${HARNESS_REVISION:-}")"
  git -C "$root" merge-base --is-ancestor "$harness_revision" "$product_revision" || {
    echo 'HARNESS_REVISION must be an ancestor of PRODUCT_REVISION' >&2; exit 2;
  }
  mkdir -m 700 "$workspace"
  read -r baseline_image baseline_sha < <(build_image "$product_revision" baseline)
  printf 'harness_revision=%s\nbaseline_revision=%s\nbaseline_image=%s\nbaseline_sha=%s\n' \
    "$harness_revision" "$product_revision" "$baseline_image" "$baseline_sha" >"$workspace/manifest"
  chmod 600 "$workspace/manifest"

  "${compose[@]}" down -v --remove-orphans
  "${compose[@]}" up -d --wait postgres
  compose_override "$baseline_image" "$workspace/compose-baseline.yaml"
  "${compose[@]}" -f "$workspace/compose-baseline.yaml" up -d --wait --no-build lansurfer
  wait_ready

  export LANSURFER_LOADTEST_DATABASE_URL="$database_url"
  export LANSURFER_LOADTEST_SESSIONS_FILE="$workspace/sessions.jsonl"
  export LANSURFER_LOADTEST_FIXTURES_FILE="$workspace/fixtures.json"
  export LANSURFER_LOADTEST_CONTROL_FILE="$workspace/control.json"
  export LANSURFER_LOADTEST_METRICS_KEY_FILE="$workspace/metrics-key"
  local prefix="C$(date +%s | cut -c 5-10)"
  go -C "$root" run ./tools/loadtest/seed -users 507 -prefix "$prefix" -output "$LANSURFER_LOADTEST_SESSIONS_FILE"
  go -C "$root" run ./tools/loadtest/fixtures -games 20 -requests 60 -sessions "$LANSURFER_LOADTEST_SESSIONS_FILE" -output "$LANSURFER_LOADTEST_FIXTURES_FILE"
  go -C "$root" run ./tools/loadtest/state -mode prepare -sessions "$LANSURFER_LOADTEST_SESSIONS_FILE" -fixtures "$LANSURFER_LOADTEST_FIXTURES_FILE" -output "$LANSURFER_LOADTEST_CONTROL_FILE"
  go -C "$root" run ./tools/loadtest/state -mode metrics-key -output "$LANSURFER_LOADTEST_METRICS_KEY_FILE"
  "${compose[@]}" exec -T postgres pg_dump -U lansurfer_loadtest -Fc lansurfer_loadtest >"$workspace/database.dump"
  chmod 600 "$workspace/database.dump"
  "${chaos_compose[@]}" up -d --build --wait proxy
  bash "$root/tools/loadtest/collect-env.sh" >"$workspace/environment.txt"
  chmod 600 "$workspace/environment.txt"
  echo "Prepared private paired workspace: $workspace"
  echo 'The isolated lansurfer-loadtest database was reset and seeded.'
}

restore_snapshot() {
  "${compose[@]}" stop lansurfer >/dev/null
  "${compose[@]}" exec -T postgres psql -U lansurfer_loadtest -d postgres -c 'DROP DATABASE lansurfer_loadtest WITH (FORCE)'
  "${compose[@]}" exec -T postgres psql -U lansurfer_loadtest -d postgres -c 'CREATE DATABASE lansurfer_loadtest OWNER lansurfer_loadtest'
  cat "$workspace/database.dump" | "${compose[@]}" exec -T postgres pg_restore -U lansurfer_loadtest --no-owner --no-privileges -d lansurfer_loadtest
}

run_profile() {
  local variant="$1" revision="$2" image="$3" digest="$4" repetition="$5" profile="$6"
  local out="$workspace/results/${variant}/rep-${repetition}/${profile}"
  mkdir -p "$out"
  chmod 700 "$out"
  restore_snapshot
  compose_override "$image" "$workspace/compose-${variant}.yaml"
  "${chaos_compose[@]}" -f "$workspace/compose-${variant}.yaml" up -d --no-build --force-recreate lansurfer proxy
  wait_ready
  verify_bundle "$digest"
  local image_id runner_revision dirty
  image_id="$(docker image inspect --format '{{.Id}}' "$image")"
  runner_revision="$(git -C "$root" rev-parse HEAD)"
  dirty=false
  [[ -z "$(git -C "$root" status --porcelain -- . ':!docs/superpowers')" ]] || dirty=true
  printf '{"schemaVersion":1,"variant":"%s","repetition":%s,"profile":"%s","productRevision":"%s","harnessRevision":"%s","runnerRevision":"%s","dirty":%s,"image":"%s","imageID":"%s","appBundleSHA256":"%s","runtimeBundleVerified":true,"refreshMode":"browser-like"}\n' \
    "$variant" "$repetition" "$profile" "$revision" "$HARNESS_REVISION" "$runner_revision" "$dirty" "$image" "$image_id" "$digest" >"$out/manifest.json"
  chmod 600 "$out/manifest.json"
  local simulated_users writers sim_offset writer_offset refresh_mode duration profile_pid=""
  if [[ "$profile" == '120' ]]; then
    simulated_users=120 writers=0 sim_offset=5 writer_offset=125 duration=90s
  else
    simulated_users=400 writers=100 sim_offset=5 writer_offset=405 duration=90s
  fi
  refresh_mode=browser-like
  [[ "$variant" == treatment ]] && refresh_mode=coalesced
  export BASE_URL=http://127.0.0.1:18081 REQUEST="$(node -p "require('$workspace/control.json').request_id")"
  export SIMULATED_USERS="$simulated_users" SIMULATED_SESSION_OFFSET="$sim_offset" BROWSER_CLIENTS=5
  export WRITERS="$writers" WRITER_SESSION_OFFSET="$writer_offset" WRITER_DURATION="$duration"
  export DURATION="$duration" PROFILE_DURATION="$duration" PROFILE_INTERVAL=5s TRIGGER=20 TRIGGER_INTERVAL=500ms TRIGGER_DELAY=10s
  export COALESCING=1 REFRESH_MODE="$refresh_mode" VIEW=mixed FILTER_GAME='Counter-Strike 2' PROBE=0
  export LANSURFER_LOADTEST_REPORT_DIR="$out" PRODUCT_REVISION="$revision" HARNESS_REVISION="$HARNESS_REVISION"
  export LANSURFER_LOADTEST_METRICS_KEY_FILE="$workspace/metrics-key"
  export BUNDLE_SHA256="$digest" VARIANT="$variant" REPETITION="$repetition"
  if [[ "$profile" == '120' ]]; then
    go -C "$root" run ./tools/loadtest/profile -duration "$duration" -interval 5s -metrics-key-file "$workspace/metrics-key" -output "$out/database-profile.jsonl" >"$out/profile-console.log" 2>&1 &
    profile_pid=$!
  fi
  local calibration_exit=0 profile_exit=0
  if node "$root/tools/loadtest/browser-calibration.mjs" >"$out/console.log" 2>&1; then
    :
  else
    calibration_exit=$?
  fi
  local reports=("$out"/browser-calibration-*.json)
  if [[ ! -f "${reports[0]}" ]]; then
    cat "$out/console.log" >&2
    [[ -z "$profile_pid" ]] || wait "$profile_pid" || true
    return 1
  fi
  if [[ -n "$profile_pid" ]]; then
    if wait "$profile_pid"; then :; else profile_exit=$?; fi
  fi
  if (( profile_exit )); then
    printf 'profiler exited %s\n' "$profile_exit" >>"$out/profile-console.log"
  fi
  if (( calibration_exit || profile_exit )); then
    printf '%s profile recorded findings: calibration=%d profiler=%d\n' "$profile" "$calibration_exit" "$profile_exit"
  fi
  go -C "$root" run ./tools/loadtest/state -mode audit -output "$workspace/control.json" >"$out/audit.json"
}

run() {
  local product_revision requested_harness_revision variant repetition baseline_revision baseline_image baseline_sha image digest
  product_revision="$(revision "${PRODUCT_REVISION:-}")"
  requested_harness_revision="$(revision "${HARNESS_REVISION:-}")"
  variant="${VARIANT:-}"
  repetition="${REPETITION:-}"
  [[ "$variant" == baseline || "$variant" == treatment ]] || { echo 'VARIANT must be baseline or treatment' >&2; exit 2; }
  [[ "$repetition" =~ ^[1-9][0-9]*$ ]] || { echo 'REPETITION must be a positive integer' >&2; exit 2; }
  [[ -d "$workspace" && -O "$workspace" ]] || { echo 'paired workspace is missing or not owned by current user' >&2; exit 2; }
  # shellcheck disable=SC1090
  source "$workspace/manifest"
  [[ "$harness_revision" == "$requested_harness_revision" ]] || { echo 'HARNESS_REVISION differs from prepared workspace' >&2; exit 2; }
  export HARNESS_REVISION="$harness_revision"
  if [[ "$variant" == baseline ]]; then
    [[ "$product_revision" == "$baseline_revision" ]] || { echo 'baseline run must use prepared PRODUCT_REVISION' >&2; exit 2; }
    image="$baseline_image" digest="$baseline_sha"
  else
    [[ "$product_revision" != "$baseline_revision" ]] || { echo 'treatment revision must differ from baseline' >&2; exit 2; }
    git -C "$root" merge-base --is-ancestor "$harness_revision" "$product_revision" || { echo 'HARNESS_REVISION must be an ancestor of treatment PRODUCT_REVISION' >&2; exit 2; }
    read -r image digest < <(build_image "$product_revision" treatment)
  fi
  export LANSURFER_LOADTEST_DATABASE_URL="$database_url"
  export LANSURFER_LOADTEST_SESSIONS_FILE="$workspace/sessions.jsonl"
  export LANSURFER_LOADTEST_FIXTURES_FILE="$workspace/fixtures.json"
  export LANSURFER_LOADTEST_CONTROL_FILE="$workspace/control.json"
  export LANSURFER_LOADTEST_METRICS_KEY_FILE="$workspace/metrics-key"
  run_profile "$variant" "$product_revision" "$image" "$digest" "$repetition" 120
  run_profile "$variant" "$product_revision" "$image" "$digest" "$repetition" 400-writer
  echo "Completed $variant repetition $repetition; reports: $workspace/results/$variant/rep-$repetition"
}
mkdir -p "$workspace_root"
chmod 700 "$workspace_root"
exec 9>"${workspace_root}/${slug}.lock"
flock -n 9 || { echo 'paired workspace is already in use' >&2; exit 2; }
cd "$root"
case "${1:-}" in
  prepare) [[ $# == 1 ]] || { echo 'prepare takes no arguments' >&2; exit 2; }; prepare ;;
  run) [[ $# == 1 ]] || { echo 'run takes no arguments' >&2; exit 2; }; run ;;
  *) echo 'usage: coalescing.sh prepare|run' >&2; exit 2 ;;
esac
