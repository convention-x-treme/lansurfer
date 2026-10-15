#!/usr/bin/env bash
set -euo pipefail

# Nested runner architecture: GitHub's job-level container lacked /__e/node24.
# Checkout is on the runner; build/test tools are invoked inside Docker images.
# Copy source through the Docker API rather than assuming daemon host mount paths.
job="${1:?Usage: runner.sh quality|integration|browser|build|smoke|snapshot|validate|title}"
: "${GITHUB_WORKSPACE:?}"
: "${GITHUB_RUN_ID:?}"
: "${GITHUB_RUN_ATTEMPT:?}"
cd "$GITHUB_WORKSPACE"

case "$job" in
  quality|integration|build) target=go ;;
  browser) target=browser ;;
  snapshot|validate) target=release ;;
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

docker build -f tools/ci/Dockerfile --target "$target" --tag "$image" .

args=(
  --name "$container" --workdir /workspace
  -e CI=true -e GOTOOLCHAIN=local
  -e "GITHUB_REF=$GITHUB_REF" -e "GITHUB_RUN_ID=$GITHUB_RUN_ID"
  -e "GITHUB_RUN_ATTEMPT=$GITHUB_RUN_ATTEMPT"
  -e GOPATH=/go
  -v lansurfer-ci-go-mod:/go/pkg/mod
  -v lansurfer-ci-go-build:/root/.cache/go-build
)

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

if [[ "$job" == snapshot ]]; then
  # Read-only snapshot tooling needs Docker Buildx; the isolated CI socket is used.
  args+=(-v /var/run/docker.sock:/var/run/docker.sock)
fi
if [[ "$job" == title ]]; then
  args+=(-e "PR_TITLE=$PR_TITLE")
fi

docker create "${args[@]}" --entrypoint /bin/bash "$image" \
  /workspace/tools/ci/container.sh "$job" >/dev/null
docker cp "$GITHUB_WORKSPACE/." "$container:/workspace/"

set +e
docker start --attach "$container"
attach_status=$?
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
exit "$result"
