#!/usr/bin/env bash
# Release-only gate: boot the freshly published tag image with its exact
# release-rendered Compose files BEFORE the draft becomes public.
# Everything is disposable and never touches operator infrastructure.
set -euo pipefail

: "${GITHUB_RUN_ID:?Only run in release CI}"
deployment_dir="${1:?Provide rendered release assets directory}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
deployment_dir="$(cd "$deployment_dir" && pwd)"
[[ -s "$deployment_dir/compose.yaml" && -s "$deployment_dir/production.env.example" ]] ||
  { echo "Missing release Compose assets" >&2; exit 1; }

project="lansurfer-release-smoke-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT:-1}"
network="${project}_default"
compose=(docker compose --project-name "$project" --env-file "$deployment_dir/production.env.example" -f "$deployment_dir/compose.yaml")

cleanup() {
  result=$?
  trap - EXIT
  if (( result != 0 )); then
    "${compose[@]}" ps >&2 || true
    "${compose[@]}" logs --no-color --tail=80 >&2 || true
  fi
  "${compose[@]}" down --volumes --remove-orphans >/dev/null || true
  exit "$result"
}
trap cleanup EXIT

# All release image references are loaded from the generated asset; only
# otherwise-unconfigured, synthetic operator credentials are supplied here.
export POSTGRES_PASSWORD="$(python3 -c 'import secrets; print(secrets.token_hex(32))')"
export LANSURFER_BASE_URL=https://lansurfer.example.invalid
export LANSURFER_DISCORD_CLIENT_ID=release-smoke
export LANSURFER_DISCORD_CLIENT_SECRET=release-smoke
export LANSURFER_HTTP_PORT="$((55000 + GITHUB_RUN_ID % 10000))"
export LANSURFER_IMPRINT_ENABLED=true

"${compose[@]}" config --format json | python3 "$root/scripts/test-production-compose.py"
"${compose[@]}" pull --quiet
"${compose[@]}" up -d --wait --wait-timeout 120

wait_ready() {
  docker run --rm --network "$network" --entrypoint /bin/sh curlimages/curl:8.16.0 -ec '
    attempt=0
    while [ "$attempt" -lt 60 ]; do
      health="$(curl --fail --silent --max-time 3 http://lansurfer:8080/healthz || true)"
      ready="$(curl --fail --silent --max-time 3 http://lansurfer:8080/readyz || true)"
      if [ "$health" = ok ] && [ "$ready" = ready ]; then exit 0; fi
      attempt=$((attempt + 1))
      sleep 2
    done
    echo "Tagged release image did not become ready" >&2
    exit 1
  '
}
imprint_status() {
  docker run --rm --network "$network" --entrypoint /bin/sh curlimages/curl:8.16.0 \
    -ec 'curl -sS -o /dev/null -w "%{http_code}" http://lansurfer:8080/imprint'
}

wait_ready
version_output="$("${compose[@]}" exec -T lansurfer /lansurfer version)"
[[ "$version_output" =~ ^LANsurfer\ v[^[:space:]]+\ \([0-9a-f]{40}\)$ ]] ||
  { echo "Tagged release image returned invalid CLI version metadata" >&2; exit 1; }
"${compose[@]}" exec -T lansurfer /lansurfer healthcheck
help_output="$("${compose[@]}" exec -T lansurfer /lansurfer help)"
[[ "$help_output" == *"admin notifications generate-secrets"* ]] ||
  { echo "Tagged release image help omits admin commands" >&2; exit 1; }
[[ "$(imprint_status)" == "200" ]] ||
  { echo "New release did not serve /imprint when enabled" >&2; exit 1; }

# Validate v0.2-specific env wiring: a recreated app must stop serving /imprint.
export LANSURFER_IMPRINT_ENABLED=false
"${compose[@]}" up -d --wait --wait-timeout 120
wait_ready
[[ "$(imprint_status)" == "404" ]] ||
  { echo "New release still serves /imprint when disabled" >&2; exit 1; }

echo "Tagged release image and packaged Compose passed startup, DB migration, health and imprint checks."
