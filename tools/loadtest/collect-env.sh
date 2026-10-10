#!/usr/bin/env bash
# Prints non-secret environment facts for a load-test report. Safe to paste into
# an issue: it never reads session files, tokens, or credentials.
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
compose="$root/tools/loadtest/compose.yaml"

echo "date_utc: $(date -u +%Y-%m-%dT%H:%M:%SZ)"
echo "git_revision: $(git -C "$root" rev-parse HEAD)"
echo "git_branch: $(git -C "$root" branch --show-current)"
echo "git_dirty: $([ -n "$(git -C "$root" status --porcelain -- . ':!docs/superpowers')" ] && echo yes || echo no)"
echo "kernel: $(uname -sr)"
echo "cpu: $(grep -m1 'model name' /proc/cpuinfo | cut -d: -f2- | sed 's/^ //') x$(nproc)"
echo "memory_mib: $(awk '/MemTotal/ {printf "%d", $2/1024}' /proc/meminfo)"
echo "go: $(go version | awk '{print $3}')"
echo "docker: $(docker version --format '{{.Server.Version}}' 2>/dev/null || echo unknown)"
echo "postgres_image: $(docker compose -f "$compose" ps postgres --format '{{.Image}}' 2>/dev/null || echo unknown)"
echo "postgres_image: $(docker compose -f "$compose" images postgres --format '{{.Repository}}:{{.Tag}}' 2>/dev/null || echo unknown)"
echo "--- container resources (docker stats, one sample) ---"
docker stats --no-stream --format '{{.Name}} cpu={{.CPUPerc}} mem={{.MemUsage}} pids={{.PIDs}}' \
  $(docker compose -f "$compose" ps -q 2>/dev/null) 2>/dev/null || echo "stack not running"
echo "--- container limits (0 = unlimited) ---"
for id in $(docker compose -f "$compose" ps -q 2>/dev/null); do
  docker inspect --format '{{.Name}} memory_limit={{.HostConfig.Memory}} nano_cpus={{.HostConfig.NanoCpus}}' "$id"
done
echo "note: /metrics needs an operator-generated Metrics API key and is not scraped by this harness"
