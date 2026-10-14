#!/usr/bin/env bash
set -euo pipefail

repo=$(git rev-parse --show-toplevel)
tmp=$(mktemp -d)
signal_pid=
cleanup() {
  if [ -n "$signal_pid" ]; then kill -TERM -- "-$signal_pid" 2>/dev/null || true; fi
  rm -rf "$tmp"
}
trap cleanup EXIT
bin="$tmp/bin"
mkdir -p "$bin"

cat >"$bin/npm" <<'EOF'
#!/bin/sh
printf 'npm %s\n' "$*" >>"$TASK_TEST_LOG"
exit 0
EOF
cat >"$bin/go" <<'EOF'
#!/bin/sh
printf 'go %s|%s|%s|%s\n' "$*" "${LANSURFER_DATABASE_URL-}" "${LANSURFER_OIDC_ISSUER-}" "${LANSURFER_OIDC_CLIENT_ID-}" >>"$TASK_TEST_LOG"
case "$*" in
  'run ./cmd/lansurfer'|'run ./cmd/lansurfer admin users'|'run ./cmd/lansurfer admin promote --user-id '*)
    if [ "${TASK_TEST_BLOCK-}" = 1 ]; then exec sleep 30; fi
    exit 37
    ;;
esac
EOF
cat >"$bin/docker" <<'EOF'
#!/bin/sh
printf 'docker %s|port=%s\n' "$*" "${LANSURFER_POSTGRES_PORT-}" >>"$TASK_TEST_LOG"
[ "${TASK_TEST_DOCKER_FAIL-}" != 1 ]
EOF
chmod +x "$bin/npm" "$bin/go" "$bin/docker"
real_task=$(command -v task)
cat >"$bin/task" <<'EOF'
#!/bin/sh
exec "$TASK_TEST_REAL_TASK" --dir "$TASK_TEST_DIR" --taskfile "$TASK_TEST_TASKFILE" "$@"
EOF
chmod +x "$bin/task"

TASK_TEST_TASKFILE="$repo/Taskfile.yml"


new_case() {
  case_dir="$tmp/$1"
  mkdir -p "$case_dir"
  : >"$tmp/$1.log"
}
run_dev() {
  local name=$1 expected_status=$2
  shift 2
  set +e
  env PATH="$bin:$PATH" TASK_TEST_LOG="$tmp/$name.log" TASK_TEST_REAL_TASK="$real_task" TASK_TEST_DIR="$case_dir" TASK_TEST_TASKFILE="$TASK_TEST_TASKFILE" "$@" "$real_task" --dir "$case_dir" --taskfile "$TASK_TEST_TASKFILE" dev >"$tmp/$name.out" 2>&1
  local status=$?
  set -e
  if [ "$expected_status" = nonzero ]; then
    if [ "$status" -eq 0 ]; then
      printf 'case %s exited successfully, expected failure\n' "$name" >&2
      cat "$tmp/$name.out" >&2
      exit 1
    fi
  elif [ "$status" -ne "$expected_status" ]; then
    printf 'case %s exited %s, expected %s\n' "$name" "$status" "$expected_status" >&2
    cat "$tmp/$name.out" >&2
    exit 1
  fi
}
assert_log() {
  if ! grep -F -- "$2" "$tmp/$1.log" >/dev/null; then
    printf 'case %s missing expected command: %s\n' "$1" "$2" >&2
    cat "$tmp/$1.log" >&2
    exit 1
  fi
}
assert_no_log() {
  if grep -F -- "$2" "$tmp/$1.log" >/dev/null; then
    printf 'case %s unexpectedly ran: %s\n' "$1" "$2" >&2
    exit 1
  fi
}

new_case managed
run_dev managed nonzero
assert_log managed 'go generate ./internal/web/branding'
assert_log managed 'npm run build'
assert_log managed 'docker compose up -d --wait postgres'
assert_log managed 'go run ./cmd/lansurfer|postgres://lansurfer:lansurfer@127.0.0.1:5432/lansurfer?sslmode=disable|https://auth.convention-x-treme.de/realms/local-dev|lansurfer'
assert_no_log managed 'docker compose down'
if ! grep -F 'Using managed PostgreSQL at 127.0.0.1:5432.' "$tmp/managed.out" >/dev/null; then
  echo 'managed mode status is missing' >&2
  exit 1
fi

new_case external
external_url='postgres://user:secret@db.example.test:5432/custom?sslmode=require'
run_dev external nonzero LANSURFER_DATABASE_URL="$external_url"
assert_log external 'go generate ./internal/web/branding'
assert_log external 'npm run build'
assert_log external "go run ./cmd/lansurfer|$external_url|https://auth.convention-x-treme.de/realms/local-dev|lansurfer"
assert_no_log external 'docker '
if ! grep -F 'Using external PostgreSQL from LANSURFER_DATABASE_URL.' "$tmp/external.out" >/dev/null; then
  echo 'external mode status is missing' >&2
  exit 1
fi
if grep -F "$external_url" "$tmp/external.out" >/dev/null; then
  echo 'external URL leaked to task output' >&2
  exit 1
fi

new_case dotenv
printf '%s\n' 'LANSURFER_DATABASE_URL=postgres://dotenv:secret@dotenv.example.test/db' 'LANSURFER_POSTGRES_PORT=5544' >"$case_dir/.env"
run_dev dotenv nonzero
assert_log dotenv 'go run ./cmd/lansurfer|postgres://dotenv:secret@dotenv.example.test/db|https://auth.convention-x-treme.de/realms/local-dev|lansurfer'
assert_no_log dotenv 'docker '

new_case shell-wins
printf '%s\n' 'LANSURFER_DATABASE_URL=postgres://dotenv:secret@dotenv.example.test/db' >"$case_dir/.env"
run_dev shell-wins nonzero LANSURFER_DATABASE_URL=postgres://shell:secret@shell.example.test/db
assert_log shell-wins 'go run ./cmd/lansurfer|postgres://shell:secret@shell.example.test/db|https://auth.convention-x-treme.de/realms/local-dev|lansurfer'
assert_no_log shell-wins 'docker '

new_case port
printf '%s\n' 'LANSURFER_POSTGRES_PORT=5544' >"$case_dir/.env"
run_dev port nonzero
assert_log port 'docker compose up -d --wait postgres'
assert_log port 'go run ./cmd/lansurfer|postgres://lansurfer:lansurfer@127.0.0.1:5544/lansurfer?sslmode=disable|'
assert_log port 'docker compose up -d --wait postgres|port=5544'

new_case docker-failure
run_dev docker-failure nonzero TASK_TEST_DOCKER_FAIL=1
assert_log docker-failure 'docker compose up -d --wait postgres'
assert_no_log docker-failure 'go run ./cmd/lansurfer'

new_case signal
set +e
setsid env PATH="$bin:$PATH" TASK_TEST_LOG="$tmp/signal.log" TASK_TEST_BLOCK=1 TASK_TEST_REAL_TASK="$real_task" TASK_TEST_DIR="$case_dir" TASK_TEST_TASKFILE="$TASK_TEST_TASKFILE" "$real_task" --dir "$case_dir" --taskfile "$TASK_TEST_TASKFILE" dev >"$tmp/signal.out" 2>&1 &
pid=$!
signal_pid=$pid
set -e
started=0
for _ in $(seq 1 100); do
  if grep -F 'go run ./cmd/lansurfer|' "$tmp/signal.log" >/dev/null 2>&1; then started=1; break; fi
  sleep 0.05
done
if [ "$started" -ne 1 ]; then
  echo 'task dev did not start the application before signal test' >&2
  cat "$tmp/signal.out" >&2
  exit 1
fi
kill -INT -- "-$pid"
set +e
wait "$pid"
status=$?
signal_pid=
set -e
if [ "$status" -eq 0 ] || kill -0 "$pid" 2>/dev/null; then
  echo 'Ctrl+C did not stop task dev' >&2
  exit 1
fi
assert_no_log signal 'docker compose down'

new_case admin-default
printf '%s\n' 'LANSURFER_POSTGRES_PORT=5544' >"$case_dir/.env"
set +e
env PATH="$bin:$PATH" TASK_TEST_LOG="$tmp/admin-default.log" "$real_task" --dir "$case_dir" --taskfile "$TASK_TEST_TASKFILE" admin:users >"$tmp/admin-default.out" 2>&1
status=$?
set -e
if [ "$status" -eq 0 ]; then
  echo 'admin-default unexpectedly succeeded with fake Go process' >&2
  exit 1
fi
assert_log admin-default 'go run ./cmd/lansurfer admin users|postgres://lansurfer:lansurfer@127.0.0.1:5544/lansurfer?sslmode=disable|'
assert_no_log admin-default 'docker '


new_case run
set +e
env PATH="$bin:$PATH" TASK_TEST_LOG="$tmp/run.log" "$real_task" --dir "$case_dir" --taskfile "$TASK_TEST_TASKFILE" run >"$tmp/run.out" 2>&1
status=$?
set -e
if [ "$status" -eq 0 ]; then
  echo 'task run unexpectedly succeeded with fake Go process' >&2
  exit 1
fi
assert_log run 'go run ./cmd/lansurfer|||'
assert_no_log run 'docker '

new_case db-down
env PATH="$bin:$PATH" TASK_TEST_LOG="$tmp/db-down.log" "$real_task" --dir "$case_dir" --taskfile "$TASK_TEST_TASKFILE" db:down >"$tmp/db-down.out" 2>&1
assert_log db-down 'docker compose down'
assert_no_log db-down --volumes
new_case admin-external
external_url='postgres://admin:secret@shared.example.test/lansurfer'
set +e
env PATH="$bin:$PATH" TASK_TEST_LOG="$tmp/admin-external.log" LANSURFER_DATABASE_URL="$external_url" "$real_task" --dir "$case_dir" --taskfile "$TASK_TEST_TASKFILE" admin:promote USER_ID=example >"$tmp/admin-external.out" 2>&1
status=$?
set -e
if [ "$status" -eq 0 ]; then
  echo 'admin-external unexpectedly succeeded with fake Go process' >&2
  exit 1
fi
assert_log admin-external "go run ./cmd/lansurfer admin promote --user-id example|$external_url|"
assert_no_log admin-external 'docker '

printf 'task dev behavior checks passed\n'
