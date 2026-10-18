finish_profile() {
  local exit_code="$1" out="$2" calibration_status=0 valid=true
  shift 2
  if (( exit_code != 0 )); then
    valid=false
    calibration_status=1
    printf 'browser calibration failed with exit code %d\n' "$exit_code" >&2
  fi
  printf '{"valid":%s,"calibrationExitCode":%d}\n' "$valid" "$exit_code" >"$out/calibration-status.json"
  "$@" >"$out/audit.json" || return
  (( calibration_status == 0 )) || return "$calibration_status"
}

verify_proxy_image_id() {
  [[ "$1" == "$2" ]] || {
    echo 'proxy image ID differs from prepared image' >&2
    return 1
  }
}
