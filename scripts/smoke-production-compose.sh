#!/usr/bin/env bash
# CI-only: boot the real, pinned release image on a disposable Compose project.
set -euo pipefail

: "${GITHUB_RUN_ID:?Run only in GitHub Actions, never on an operator host}"
: "${GITHUB_ACTOR:?GitHub actor is required for GHCR}"
: "${GHCR_TOKEN:?The job needs packages:read for the existing release image}"

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
project="lansurfer-ci-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT:-1}"
config_file="$(mktemp)"
chmod 600 "$config_file"
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
  exit "$result"
}
trap cleanup EXIT

cat > "$config_file" <<EOF
POSTGRES_PASSWORD=$(openssl rand -hex 32)
LANSURFER_BASE_URL=https://lansurfer.example.invalid
LANSURFER_HTTP_PORT=18081
LANSURFER_DISCORD_CLIENT_ID=ci-only
LANSURFER_DISCORD_CLIENT_SECRET=ci-only
LANSURFER_LEGAL_ENTITY_NAME=CI fixture
LANSURFER_LEGAL_CONTACT_EMAIL=ci@example.invalid
EOF

"${compose[@]}" config --format json | python3 "$root/scripts/test-production-compose.py" --test-mutations
printf '%s' "$GHCR_TOKEN" | docker login ghcr.io --username "$GITHUB_ACTOR" --password-stdin >/dev/null

# All containers are isolated to the uniquely named CI Compose project.
"${compose[@]}" pull --quiet

wait_ready() {
  local health readiness
  for attempt in {1..90}; do
    health="$(curl --fail --silent --max-time 3 http://127.0.0.1:18081/healthz || true)"
    readiness="$(curl --fail --silent --max-time 3 http://127.0.0.1:18081/readyz || true)"
    if [[ "$health" == ok && "$readiness" == ready ]]; then
      return 0
    fi
    sleep 2
  done
  echo 'LANsurfer never became ready on the pinned release image' >&2
  return 1
}

"${compose[@]}" up -d --wait --wait-timeout 120
wait_ready

# Seed one disposable record. Recreating all containers must keep named volumes.
"${compose[@]}" exec -T postgres psql -U lansurfer -d lansurfer -v ON_ERROR_STOP=1 \
  -c 'CREATE TABLE ci_persistence_probe (id integer PRIMARY KEY); INSERT INTO ci_persistence_probe (id) VALUES (29);' >/dev/null
"${compose[@]}" down
"${compose[@]}" up -d --wait --wait-timeout 120
wait_ready
count="$("${compose[@]}" exec -T postgres psql -U lansurfer -d lansurfer -At \
  -c 'SELECT count(*) FROM ci_persistence_probe WHERE id = 29;')"
[[ "$count" == 1 ]] || { echo 'Named PostgreSQL volume did not retain the CI probe record' >&2; exit 1; }
echo 'Production Compose: pinned release boots, migrates, passes readiness and retains data after recreation.'
