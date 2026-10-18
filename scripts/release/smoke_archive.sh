#!/usr/bin/env bash
set -euo pipefail
archive="${1:?Usage: smoke_archive.sh ARCHIVE}"
: "${GITHUB_RUN_ID:?Missing isolated run identifier}"
: "${GITHUB_RUN_ATTEMPT:?Missing isolated attempt identifier}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
bash "$root/scripts/release/verify_archive_contents.sh" "$archive"
tmp="$(mktemp -d)"
image="lansurfer-archive-smoke:${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
cleanup() {
  status=$?
  trap - EXIT INT TERM
  docker image rm "$image" >/dev/null 2>&1 || true
  rm -rf -- "$tmp"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
mkdir -p "$tmp/linux/amd64"
tar -xzf "$archive" -C "$tmp" lansurfer LICENSE README.md THIRD_PARTY_NOTICES.md ASSETS-LICENSE.md
cp "$tmp/lansurfer" "$tmp/linux/amd64/lansurfer"
docker build --builder default --load --platform linux/amd64 \
  --build-arg TARGETPLATFORM=linux/amd64 \
  -f "$root/Dockerfile.release" --tag "$image" "$tmp"
LANSURFER_SMOKE_IMAGE="$image" GITHUB_WORKSPACE="$root" \
  BUILDX_BUILDER=default bash "$root/tools/ci/runner.sh" smoke
