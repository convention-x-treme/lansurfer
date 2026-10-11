#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../../.." && pwd)"
if (( $# < 2 )); then
  echo 'usage: report/run.sh SCENARIO COMMAND [ARG...]' >&2
  exit 2
fi
scenario="$1"
shift
cd "$root"
run_dir="$(go run ./tools/loadtest/report -mode start -scenario "$scenario")"
umask 077
export LANSURFER_LOADTEST_REPORT_DIR="$run_dir"
set +e
"$@" 2>&1 | tee "$run_dir/console.log"
pipeline_status=("${PIPESTATUS[@]}")
set -e
status="${pipeline_status[0]}"
if (( pipeline_status[1] != 0 && status == 0 )); then
  status="${pipeline_status[1]}"
fi
if ! go run ./tools/loadtest/report -mode finish -run-dir "$run_dir" -exit-code "$status"; then
  if (( status == 0 )); then
    status=1
  fi
fi
exit "$status"
