#!/usr/bin/env bash
# CI-only: boot the actual release image using the self-hosted runner Docker daemon.
# Probe through project Docker DNS, never through the LXC runner's host loopback.
set -euo pipefail

: "${GITHUB_RUN_ID:?Run only in GitHub Actions, never on an operator host}"
: "${GITHUB_ACTOR:?GitHub actor is required for GHCR}"
: "${GHCR_TOKEN:?The job needs packages:read for the existing release image}"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
project="lansurfer-ci-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT:-1}-production"
network="${project}_default"
config_file="$(mktemp)"
chmod 600 "$config_file"
docker_config="$(mktemp -d)"
chmod 700 "$docker_config"
export DOCKER_CONFIG="$docker_config"
compose=(docker compose --project-name "$project" --env-file "$config_file" -f "$root/deploy/compose.production.yaml")

cleanup() {
  result=$?
  trap - EXIT
  if (( result != 0 )); then
    "${compose[@]}" ps >&2 || true
    "${compose[@]}" logs --no-color --tail=80 >&2 || true
  fi
  "${compose[@]}" down --volumes --remove-orphans >/dev/null || true
  rm -f "$config_file"
  rm -rf -- "$docker_config"
  exit "$result"
}
trap cleanup EXIT

# Resolve both release references from the single checked-in example instead of
# duplicating version or digest pins in CI. Explicitly clear host overrides.
unset LANSURFER_IMAGE POSTGRES_IMAGE
image_refs="$(grep -E '^(LANSURFER_IMAGE|POSTGRES_IMAGE)=' "$root/deploy/production.env.example")"
[[ "$(printf '%s\n' "$image_refs" | wc -l)" -eq 2 ]] || {
  echo 'Production image references are missing from production.env.example' >&2
  exit 1
}
cat > "$config_file" <<EOF
$image_refs
POSTGRES_PASSWORD=$(openssl rand -hex 32)
LANSURFER_BASE_URL=https://lansurfer.example.invalid
LANSURFER_HTTP_PORT=$((20000 + GITHUB_RUN_ID % 30000))
LANSURFER_DISCORD_CLIENT_ID=ci-only
LANSURFER_DISCORD_CLIENT_SECRET=ci-only
LANSURFER_LEGAL_ENTITY_NAME=CI fixture
LANSURFER_LEGAL_CONTACT_EMAIL=ci@example.invalid
EOF

# The runner host is intentionally bare (no native Python dependency).
# Pipe synthetic effective configuration to a disposable, network-isolated Python container.
"${compose[@]}" config --format json |
  docker run --rm -i --network none python:3.13-alpine \
    python3 -c "$(cat "$root/scripts/test-production-compose.py")" --test-mutations
# GHCR read token lives in an ephemeral Docker config, never the runner's home.
printf '%s' "$GHCR_TOKEN" | docker login ghcr.io --username "$GITHUB_ACTOR" --password-stdin >/dev/null

# All containers are isolated to the uniquely named CI Compose project.
"${compose[@]}" pull --quiet

wait_ready() {
  # Docker lives behind the runner's isolated daemon. Do not assume
  # host-published 127.0.0.1 ports are reachable from GitHub's LXC worker.
  docker run --rm --network "$network" --entrypoint /bin/sh curlimages/curl:8.16.0 -ec '
    attempt=0
    while [ "$attempt" -lt 90 ]; do
      health="$(curl --fail --silent --max-time 3 http://lansurfer:8080/healthz || true)"
      readiness="$(curl --fail --silent --max-time 3 http://lansurfer:8080/readyz || true)"
      if [ "$health" = ok ] && [ "$readiness" = ready ]; then exit 0; fi
      attempt=$((attempt + 1))
      sleep 2
    done
    echo "LANsurfer did not become ready inside the private Docker network" >&2
    exit 1
  '
}

"${compose[@]}" up -d --wait --wait-timeout 120
wait_ready

# Explicit Compose restarts of PostgreSQL must restart the dependent application.
app_container="$("${compose[@]}" ps -q lansurfer)"
started_before="$(docker inspect --format '{{.State.StartedAt}}' "$app_container")"
"${compose[@]}" restart postgres >/dev/null
wait_ready
started_after="$(docker inspect --format '{{.State.StartedAt}}' "$app_container")"
[[ "$started_before" != "$started_after" ]] || {
  echo 'Explicit PostgreSQL restart did not restart the dependent application' >&2
  exit 1
}

# Seed one disposable record. Recreating all containers must keep named volumes.
"${compose[@]}" exec -T postgres psql -U lansurfer -d lansurfer -v ON_ERROR_STOP=1 \
  -c 'CREATE TABLE ci_persistence_probe (id integer PRIMARY KEY); INSERT INTO ci_persistence_probe (id) VALUES (29);' >/dev/null
"${compose[@]}" down
"${compose[@]}" up -d --wait --wait-timeout 120
wait_ready
count="$("${compose[@]}" exec -T postgres psql -U lansurfer -d lansurfer -At \
  -c 'SELECT count(*) FROM ci_persistence_probe WHERE id = 29;')"
[[ "$count" == 1 ]] || { echo 'Named PostgreSQL volume did not retain the CI probe record' >&2; exit 1; }
echo 'Production Compose: pinned images boot, authenticate DB, restart dependents and retain data after recreation.'
