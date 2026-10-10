#!/usr/bin/env bash
set -euo pipefail

compose=(docker compose -p lansurfer-loadtest -f tools/loadtest/compose.yaml)
postgres="$("${compose[@]}" ps -q postgres)"
app="$("${compose[@]}" ps -q lansurfer)"
proxy="$("${compose[@]}" ps -q proxy 2>/dev/null || true)"
network=lansurfer-loadtest_default
[[ -n "$postgres" && -n "$app" ]] || { echo 'isolated load-test containers are not running' >&2; exit 2; }
for id in "$postgres" "$app"; do
  project="$(docker inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}' "$id" 2>/dev/null || true)"
  [[ "$project" == lansurfer-loadtest ]] || { echo 'container is outside isolated Compose project' >&2; exit 2; }
  connected="$(docker inspect --format '{{ range $name, $config := .NetworkSettings.Networks }}{{ $name }} {{ end }}' "$id" 2>/dev/null || true)"
  [[ " $connected " == *" $network "* ]] || { echo 'container is outside isolated load-test network' >&2; exit 2; }
done
network_project="$(docker network inspect --format '{{ index .Labels "com.docker.compose.project" }}' "$network" 2>/dev/null || true)"
network_label="$(docker network inspect --format '{{ index .Labels "com.docker.compose.network" }}' "$network" 2>/dev/null || true)"
[[ "$network_project" == lansurfer-loadtest && "$network_label" == default ]] || { echo 'network is outside isolated Compose project' >&2; exit 2; }
[[ "$(docker port "$postgres" 5432/tcp 2>/dev/null || true)" == 127.0.0.1:55433 ]] || { echo 'isolated PostgreSQL port binding mismatch' >&2; exit 2; }
[[ "$(docker port "$app" 8080/tcp 2>/dev/null || true)" == 127.0.0.1:18080 ]] || { echo 'isolated application port binding mismatch' >&2; exit 2; }
if [[ -n "$proxy" ]]; then
  project="$(docker inspect --format '{{ index .Config.Labels "com.docker.compose.project" }}' "$proxy" 2>/dev/null || true)"
  [[ "$project" == lansurfer-loadtest ]] || { echo 'proxy is outside isolated Compose project' >&2; exit 2; }
  connected="$(docker inspect --format '{{ range $name, $config := .NetworkSettings.Networks }}{{ $name }} {{ end }}' "$proxy" 2>/dev/null || true)"
  [[ " $connected " == *" $network "* ]] || { echo 'proxy is outside isolated load-test network' >&2; exit 2; }
  [[ "$(docker port "$proxy" 8080/tcp 2>/dev/null || true)" == 127.0.0.1:18081 ]] || { echo 'isolated proxy port binding mismatch' >&2; exit 2; }
  [[ "$(docker port "$proxy" 8081/tcp 2>/dev/null || true)" == 127.0.0.1:18082 ]] || { echo 'isolated proxy control binding mismatch' >&2; exit 2; }
fi
