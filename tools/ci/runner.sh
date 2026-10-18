#!/usr/bin/env bash
set -euo pipefail

# Nested runner architecture: GitHub's job-level container lacked /__e/node24.
# Checkout is on the runner; build/test tools are invoked inside Docker images.
# Copy source through the Docker API rather than assuming daemon host mount paths.
job="${1:?Usage: runner.sh quality|integration|browser|build|smoke|snapshot|validate|title|publish-check|publish}"
: "${GITHUB_WORKSPACE:?}"
: "${GITHUB_RUN_ID:?}"
: "${GITHUB_RUN_ATTEMPT:?}"
cd "$GITHUB_WORKSPACE"

case "$job" in
  quality|integration|build) target=go ;;
  browser) target=browser ;;
  snapshot|validate|publish-check|publish) target=release ;;
  title) target=title ;;
  smoke) target=smoke ;;
  *) printf 'Unknown job: %s\n' "$job" >&2; exit 2 ;;
esac

# Every Compose invocation is scoped to one workflow attempt.
project="lansurfer-ci-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT-smoke"
if [[ "$job" == smoke ]]; then
  export COMPOSE_PROJECT_NAME="$project"
  export LANSURFER_POSTGRES_PORT=0 LANSURFER_HTTP_PORT=0
  cleanup_smoke() {
    code=$?
    trap - EXIT
    if (( code != 0 )); then
      docker compose -f compose.yaml -f tools/ci/compose.smoke.yaml logs --no-color || true
    fi
    docker compose -f compose.yaml -f tools/ci/compose.smoke.yaml down --volumes --remove-orphans || true
    exit "$code"
  }
  trap cleanup_smoke EXIT
  docker compose -f compose.yaml -f tools/ci/compose.smoke.yaml up --detach --build --wait postgres lansurfer
  docker compose -f compose.yaml -f tools/ci/compose.smoke.yaml build smoke
  docker compose -f compose.yaml -f tools/ci/compose.smoke.yaml run --rm --no-deps smoke
  # The source-built app migrated this disposable DB; verify its current
  # schema and a real pg_dump/pg_restore with synthetic relational data.
  bash scripts/release/smoke_backup_restore.sh "$project" docker compose \
    --project-name "$project" -f compose.yaml -f tools/ci/compose.smoke.yaml
  exit 0
fi

image="lansurfer-ci-$target:$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT-$job"
container="lansurfer-ci-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT-$job"
network=""
postgres=""

cleanup() {
  code=$?
  trap - EXIT
  docker rm --force --volumes "$container" >/dev/null 2>&1 || true
  if [[ -n "$postgres" ]]; then
    docker rm --force --volumes "$postgres" >/dev/null 2>&1 || true
  fi
  if [[ -n "$network" ]]; then
    docker network rm "$network" >/dev/null 2>&1 || true
  fi
  docker image rm "$image" >/dev/null 2>&1 || true
  exit "$code"
}
trap cleanup EXIT

if [[ "$job" == publish ]]; then
  if [[ "${GITHUB_EVENT_NAME:-}" == push && "${GITHUB_REF:-}" == refs/tags/v* ]]; then
    : "${GITHUB_REF_NAME:?}"
  elif [[ "${GITHUB_EVENT_NAME:-}" == workflow_dispatch &&
          "${GITHUB_REF:-}" == refs/heads/main &&
          "${LANSURFER_RELEASE_TAG:-}" == v* ]]; then
    : # Manual button created this immutable tag after full CI.
  else
    echo "::error::Publisher requires a tag push or guarded manual release on main" >&2
    exit 1
  fi
  : "${GH_TOKEN:?Only the privileged release job may supply GH_TOKEN}"
  : "${GITHUB_ACTOR:?}" "${GITHUB_REPOSITORY:?}" "${GITHUB_STEP_SUMMARY:?}"
  # An untrusted CI job can taint a shared Docker cache; force fresh layers and
  # resolve the pinned GoReleaser base image from the registry.
  docker build --pull --no-cache -f tools/ci/Dockerfile --target "$target" --tag "$image" .
else
  docker build -f tools/ci/Dockerfile --target "$target" --tag "$image" .
fi

args=(
  --name "$container" --workdir /workspace
  -e CI=true -e GOTOOLCHAIN=local
  -e "GITHUB_REF=$GITHUB_REF" -e "GITHUB_RUN_ID=$GITHUB_RUN_ID"
  -e "GITHUB_RUN_ATTEMPT=$GITHUB_RUN_ATTEMPT"
  -e GOPATH=/go
)
# Do not share Go build/module-cache volumes with the privileged tag publisher.
if [[ "$job" != publish ]]; then
  args+=(-v lansurfer-ci-go-mod:/go/pkg/mod -v lansurfer-ci-go-build:/root/.cache/go-build)
fi

if [[ "$job" == integration ]]; then
  network="lansurfer-ci-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT-integration"
  postgres="$network-postgres"
  docker network create "$network"
  docker run --detach --name "$postgres" --network "$network" \
    --network-alias postgres \
    -e POSTGRES_DB=lansurfer_test -e POSTGRES_USER=lansurfer_test \
    -e POSTGRES_PASSWORD=lansurfer_test \
    --health-cmd="pg_isready -U lansurfer_test -d lansurfer_test" \
    --health-interval=2s --health-timeout=3s --health-retries=15 \
    postgres:18.6-alpine
  healthy=false
  for attempt in {1..30}; do
    status="$(docker inspect --format '{{.State.Health.Status}}' "$postgres")"
    if [[ "$status" == healthy ]]; then healthy=true; break; fi
    if [[ "$status" == unhealthy ]]; then break; fi
    sleep 2
  done
  if [[ "$healthy" != true ]]; then
    docker logs "$postgres" || true
    echo "::error::PostgreSQL service did not become healthy" >&2
    exit 1
  fi
  args+=(--network "$network" -e 'LANSURFER_TEST_DATABASE_URL=postgres://lansurfer_test:lansurfer_test@postgres:5432/lansurfer_test?sslmode=disable')
fi

if [[ "$job" == snapshot || "$job" == publish ]]; then
  # Docker builds need the isolated CI daemon. The same daemon handles PR jobs:
  # that is a temporary, explicitly accepted risk until #150 goes public.
  args+=(-v /var/run/docker.sock:/var/run/docker.sock)
fi
if [[ "$job" == publish ]]; then
  # Expose only non-secret release metadata in the container configuration.
  # The short-lived GitHub token is delivered on STDIN after container creation.
  args+=(
    -i
    -e "GITHUB_EVENT_NAME=$GITHUB_EVENT_NAME"
    -e "GITHUB_REF_NAME=$GITHUB_REF_NAME"
    -e "LANSURFER_RELEASE_TAG=${LANSURFER_RELEASE_TAG:-}"
    -e "GITHUB_ACTOR=$GITHUB_ACTOR"
    -e "GITHUB_REPOSITORY=$GITHUB_REPOSITORY"
    -e GITHUB_STEP_SUMMARY=/tmp/lansurfer-release-summary.md
  )
fi
if [[ "$job" == publish-check ]]; then
  # Exercise the publisher's exact interactive Docker stdin mechanism with a
  # harmless fixed marker, never real credentials or GitHub API mutations.
  args+=(-i)
fi
if [[ "$job" == title ]]; then
  args+=(-e "PR_TITLE=$PR_TITLE")
fi

docker create "${args[@]}" --entrypoint /bin/bash "$image" \
  /workspace/tools/ci/container.sh "$job" >/dev/null
docker cp "$GITHUB_WORKSPACE/." "$container:/workspace/"

set +e
if [[ "$job" == publish ]]; then
  # Never include the token in docker create args, inspectable container env,
  # command-line args, Docker layers, or a persisted Docker config on the host.
  printf '%s\n' "$GH_TOKEN" | docker start --attach --interactive "$container"
  attach_status=$?
  unset GH_TOKEN
elif [[ "$job" == publish-check ]]; then
  printf '%s\n' 'lansurfer-publish-stdin-smoke' |
    docker start --attach --interactive "$container"
  attach_status=$?
else
  docker start --attach "$container"
  attach_status=$?
fi
set -e
result="$(docker inspect --format '{{.State.ExitCode}}' "$container")"
if (( attach_status != 0 && result == 0 )); then result="$attach_status"; fi

if [[ "$job" == browser && "$result" -ne 0 ]]; then
  mkdir -p "$GITHUB_WORKSPACE/test-results"
  docker cp "$container:/workspace/test-results/." "$GITHUB_WORKSPACE/test-results/" 2>/dev/null || true
fi
if [[ "$job" == snapshot && "$result" -eq 0 ]]; then
  mkdir -p "$GITHUB_WORKSPACE/dist"
  docker cp "$container:/workspace/dist/." "$GITHUB_WORKSPACE/dist/"
fi
if [[ "$job" == publish && "$result" -eq 0 ]]; then
  # The release script wrote its GitHub Actions summary within the disposable
  # container. Copy it out only after the guarded publication completed.
  if ! docker cp "$container:/tmp/lansurfer-release-summary.md" "$GITHUB_STEP_SUMMARY"; then
    echo "::warning::Release published, but could not copy job summary" >&2
  fi
fi
exit "$result"
