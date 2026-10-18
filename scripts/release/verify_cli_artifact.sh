#!/usr/bin/env bash
set -euo pipefail

archive="${1:?Provide release archive path}"
mode="${2:?Provide snapshot or release mode}"
expected_version="${3:-}"
expected_commit="${4:?Provide full source commit}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
tar -xzf "$archive" -C "$tmp" lansurfer
output="$("$tmp/lansurfer" version)"

case "$mode" in
  snapshot)
    prefix="LANsurfer "
    suffix=" ($expected_commit)"
    [[ "$output" == "$prefix"*"$suffix" ]] || {
      echo "Snapshot CLI version output does not match source commit" >&2
      exit 1
    }
    snapshot_version="${output#"$prefix"}"
    snapshot_version="${snapshot_version%"$suffix"}"
    [[ "$snapshot_version" == *SNAPSHOT* ]] || {
      echo "Snapshot CLI version is missing SNAPSHOT marker" >&2
      exit 1
    }
    ;;
  release)
    [[ "$output" == "LANsurfer $expected_version ($expected_commit)" ]] || {
      echo "Release CLI version output does not match release tag and source commit" >&2
      exit 1
    }
    ;;
  *)
    echo "Unknown CLI artifact verification mode" >&2
    exit 2
    ;;
esac

echo "Verified CLI artifact: $output"
