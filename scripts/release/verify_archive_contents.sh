#!/usr/bin/env bash
# Check the contents of an ACTUAL GoReleaser archive, not the source checkout.
set -euo pipefail

archive="${1:?Usage: verify_archive_contents.sh ARCHIVE}"
contents="$(tar -tzf "$archive")" || {
  echo "Cannot read release archive: $archive" >&2
  exit 1
}
for required in lansurfer LICENSE README.md THIRD_PARTY_NOTICES.md ASSETS-LICENSE.md; do
  if ! grep -Fxq -- "$required" <<< "$contents"; then
    echo "Release archive $archive is missing $required" >&2
    exit 1
  fi
done
echo "Verified release archive members: $archive"
