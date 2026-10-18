#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf -- "$tmp"' EXIT
for required in lansurfer LICENSE README.md THIRD_PARTY_NOTICES.md ASSETS-LICENSE.md; do
  printf 'synthetic %s\n' "$required" > "$tmp/$required"
done
tar -czf "$tmp/good.tar.gz" -C "$tmp" \
  lansurfer LICENSE README.md THIRD_PARTY_NOTICES.md ASSETS-LICENSE.md
bash "$root/scripts/release/verify_archive_contents.sh" "$tmp/good.tar.gz"
tar -czf "$tmp/missing-notices.tar.gz" -C "$tmp" \
  lansurfer LICENSE README.md ASSETS-LICENSE.md
if bash "$root/scripts/release/verify_archive_contents.sh" "$tmp/missing-notices.tar.gz"; then
  echo "Archive verifier accepted missing third-party notices" >&2
  exit 1
fi
