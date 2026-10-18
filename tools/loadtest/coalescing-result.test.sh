#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$root/coalescing-result.sh"
out="$(mktemp -d)"
trap 'rm -rf "$out"' EXIT
printf 'proxy and app logs\n' >"$out/compose.log"
printf 'container states\n' >"$out/container-state.json"
printf 'container details\n' >"$out/container-inspect.txt"
profile_status=0
finish_profile 7 "$out" printf '%s\n' '{"audit":"ok"}' || profile_status=$?
if (( profile_status == 0 )); then
  echo 'failed calibration returned success after a successful audit' >&2
  exit 1
fi
[[ -f "$out/compose.log" && -f "$out/container-state.json" && -f "$out/container-inspect.txt" && -f "$out/audit.json" ]]
[[ "$(cat "$out/audit.json")" == '{"audit":"ok"}' ]]
[[ "$(cat "$out/calibration-status.json")" == '{"valid":false,"calibrationExitCode":7}' ]]
if verify_proxy_image_id 'sha256:expected' 'sha256:actual' 2>/dev/null; then
  echo 'different proxy image IDs passed verification' >&2
  exit 1
fi
verify_proxy_image_id 'sha256:same' 'sha256:same'
