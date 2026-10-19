#!/usr/bin/env bash
# CI-only configuration check. No GHCR login, app image pull or running stack.
set -euo pipefail

root="$(cd "$(dirname "$0")/../.." && pwd)"
env_file="$(mktemp)"
trap 'rm -f -- "$env_file"' EXIT
chmod 600 "$env_file"

# Read the PostgreSQL pin from the source template; the app digest below is
# deliberately synthetic because Docker Compose only resolves the config.
[[ "$(grep -c '^POSTGRES_IMAGE=' "$root/deploy/production.env.example")" == 1 ]] || {
  echo "Expected one PostgreSQL pin in production.env.example" >&2
  exit 1
}
postgres_image="$(sed -n 's/^POSTGRES_IMAGE=//p' "$root/deploy/production.env.example")"
[[ "$postgres_image" == postgres:* ]] || {
  echo "Missing PostgreSQL image reference in production.env.example" >&2
  exit 1
}

cat > "$env_file" <<EOF
LANSURFER_IMAGE=ghcr.io/convention-x-treme/lansurfer:v0.0.0@sha256:$(printf 'a%.0s' {1..64})
POSTGRES_IMAGE=$postgres_image
POSTGRES_PASSWORD=$(printf 'b%.0s' {1..64})
LANSURFER_BASE_URL=https://ci.example.invalid
LANSURFER_HTTP_PORT=28080
LANSURFER_DISCORD_CLIENT_ID=ci-only
LANSURFER_DISCORD_CLIENT_SECRET=ci-only
TRAEFIK_HOST=ci.example.invalid
TRAEFIK_NETWORK=ci-traefik-net
TRAEFIK_CERT_RESOLVER=ci-resolver
EOF

# Prevent any inherited CI/operator environment variables overriding this
# isolated fixture. config does not contact the Docker daemon.
compose_config() {
  env -i PATH="$PATH" HOME="$HOME" docker compose \
    --project-name lansurfer-ci-config --env-file "$env_file" \
    -f "$root/deploy/compose.production.yaml" "$@" config --format json
}

# This runner does not require host Python. Use a disposable, networkless
# interpreter, not a new app image or Compose stack.
validator="$(cat "$root/scripts/test-production-compose.py")"
compose_config |
  docker run --rm -i --network none python:3.13-alpine \
    python3 -c "$validator" --test-mutations
compose_config -f "$root/deploy/compose.traefik.yaml" |
  docker run --rm -i --network none python:3.13-alpine \
    python3 -c "$validator" --test-traefik
echo "Current production Compose and Traefik models passed static CI checks."
