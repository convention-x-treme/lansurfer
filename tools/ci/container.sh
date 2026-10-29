#!/usr/bin/env bash
set -euo pipefail

job="$1"
cd /workspace
if command -v git >/dev/null 2>&1; then
  git config --global --add safe.directory /workspace
fi

case "$job" in
  quality)
    bash .github/scripts/test-validate-pr-title.sh
    unformatted="$(gofmt -l .)"
    if [[ -n "$unformatted" ]]; then
      printf 'The following files need gofmt:\n%s\n' "$unformatted"
      exit 1
    fi
    go mod tidy -diff
    go vet ./...
    go run github.com/sqlc-dev/sqlc/cmd/sqlc@v1.31.1 generate
    if [[ -n "$(git status --short)" ]]; then
      git status --short
      git diff
      exit 1
    fi
    go test -race ./...
    ;;

  integration)
    go test -race -count=1 -tags=integration ./internal/platform/postgres
    ;;

  browser)
    npm ci
    go generate ./internal/web/branding
    go run github.com/a-h/templ/cmd/templ@v0.3.1100 generate
    npm run build
    if [[ -n "$(git status --short)" ]]; then
      git status --short
      git diff
      exit 1
    fi
    # The Playwright image contains all browsers and native dependencies.
    npx playwright test
    ;;

  build)
    go build -trimpath ./...
    ;;

  snapshot)
    python3 -c 'import sys; sys.exit(0 if sys.version_info >= (3, 10) else "Python 3.10+ required")'
    goreleaser check
    bash -n scripts/release/publish.sh
    bash -n scripts/release/smoke_deployment.sh
    bash -n scripts/release/manual_release.sh
    bash scripts/release/test-manual-release.sh
    docker compose version
    python3 -m unittest discover -s scripts/release -p 'test_release_policy.py' -v
    python3 -m unittest discover -s scripts/release -p 'test_deployment_assets.py' -v
    python3 -m unittest discover -s scripts/release -p 'test_verify_image_index.py' -v
    bash scripts/release/test-changelog.sh
    bash scripts/release/test_cli_artifact.sh
    bash -n scripts/release/smoke_backup_restore.sh
    bash -n scripts/release/smoke_archive.sh
    bash -n scripts/release/verify_archive_contents.sh
    bash -n scripts/release/test_archive_contents.sh
    bash scripts/release/test_archive_contents.sh
    builder="lansurfer-snapshot-$GITHUB_RUN_ID-$GITHUB_RUN_ATTEMPT"
    docker buildx create --driver docker-container --name "$builder" --use
    trap 'docker buildx rm --force "$builder" >/dev/null 2>&1 || true' EXIT
    docker buildx inspect --bootstrap
    goreleaser release --snapshot --clean
    test -s dist/checksums.txt
    (cd dist && sha256sum --check checksums.txt)
    jq -e '[.[] | select(.type == "Archive")] | length == 2' dist/artifacts.json
    jq -e '[.[] | select(.type == "Archive") | [.goos, .goarch]] | sort == [["linux","amd64"],["linux","arm64"]]' dist/artifacts.json
    amd64_archive="$(jq -er '[.[] | select(.type == "Archive" and .goos == "linux" and .goarch == "amd64") | .path] | if length == 1 then .[0] else error("expected one Linux AMD64 archive") end' dist/artifacts.json)"
    bash scripts/release/verify_cli_artifact.sh "$amd64_archive" snapshot "" "$(git rev-parse HEAD)"
    mapfile -t archives < <(jq -r '.[] | select(.type == "Archive") | .path' dist/artifacts.json)
    [[ "${#archives[@]}" == 2 ]] || { echo "Expected two Linux release archives" >&2; exit 1; }
    for archive in "${archives[@]}"; do
      bash scripts/release/verify_archive_contents.sh "$archive"
    done
    bash scripts/release/smoke_archive.sh "$amd64_archive"
    # Python imports must not dirty the exact tagged working tree.
    git check-ignore -q scripts/release/__pycache__/release_policy.cpython-312.pyc
    ;;

  validate)
    python3 -c 'import sys; sys.exit(0 if sys.version_info >= (3, 10) else "Python 3.10+ required")'
    tag="${GITHUB_REF#refs/tags/}"
    python3 scripts/release/release_policy.py validate "$tag"
    test "$GITHUB_REF" = "refs/tags/$tag"
    test "$(git rev-parse HEAD)" = "$(git rev-parse "refs/tags/$tag^{commit}")"
    if ! git for-each-ref --contains HEAD --format='%(refname:short)' refs/remotes/origin |
        grep -Eq '^origin/(main|release/v(0|[1-9][0-9]*))$'; then
      echo "::error::Tag source must be reachable from main or release/vN" >&2
      exit 1
    fi
    ;;

  title)
    bash .github/scripts/validate-pr-title.sh "$PR_TITLE"
    ;;

  *)
    echo "::error::Unknown CI container job: $job" >&2
    exit 2
    ;;
esac
