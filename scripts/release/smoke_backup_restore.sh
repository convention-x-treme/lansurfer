#!/usr/bin/env bash
# CI-only regression: dump the migrated application database, restore it to
# a fresh disposable database, and prove representative references survived.
# The caller must supply its isolated Compose project; never run on an operator stack.
set -euo pipefail

if [[ ! "${GITHUB_RUN_ID:-}" =~ ^[0-9]+$ ||
      ! "${GITHUB_RUN_ATTEMPT:-}" =~ ^[0-9]+$ ]]; then
  echo "Invalid isolated test run identifier" >&2
  exit 2
fi
project="${1:?Usage: smoke_backup_restore.sh PROJECT docker compose ...}"
shift
allowed_projects=(
  "lansurfer-ci-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}-smoke"
  "lansurfer-ci-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}-artifact-smoke"
  "lansurfer-release-smoke-${GITHUB_RUN_ID}-${GITHUB_RUN_ATTEMPT}"
)
if [[ ! " ${allowed_projects[*]} " == *" $project "* ]]; then
  echo "Refusing restore test outside the current CI project" >&2
  exit 2
fi
if [[ "${1:-}" != docker || "${2:-}" != compose ]]; then
  echo "Expected a Docker Compose command" >&2
  exit 2
fi
compose=("$@")
[[ "${compose[2]:-}" == --project-name && "${compose[3]:-}" == "$project" ]] || {
  echo "Compose project does not match the isolated test project" >&2
  exit 2
}
for ((i = 4; i < ${#compose[@]}; i++)); do
  case "${compose[$i]}" in
    -p|-p*|--project-name|--project-name=*)
      echo "Compose project does not match the isolated test project" >&2
      exit 2
      ;;
  esac
done
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

# Compute the expected schema version from the same checkout that built the app.
expected=0
for migration in "$root"/internal/platform/postgres/migrations/[0-9]*.sql; do
  [[ -f "$migration" ]] || continue
  filename="${migration##*/}"
  number="${filename%%_*}"
  current=$((10#$number))
  if (( current > expected )); then expected="$current"; fi
done
(( expected > 0 )) || { echo "No embedded migration files found" >&2; exit 1; }

db_version() {
  "${compose[@]}" exec -T postgres psql -X -U lansurfer -d "$1" -At \
    -v ON_ERROR_STOP=1 -c \
    "SELECT coalesce(max(version_id),0) FROM goose_db_version WHERE is_applied"
}
assert_current_schema() {
  local actual
  actual="$(db_version "$1")"
  [[ "$actual" == "$expected" ]] || {
    echo "Database $1 has migration $actual; checkout expects $expected" >&2
    return 1
  }
}
assert_current_schema lansurfer
"${compose[@]}" stop lansurfer

# Every ID, name and delivery here is synthetic. A backfill activity and
# completed delivery avoid any outbound notification side effects.
"${compose[@]}" exec -T postgres psql -X -U lansurfer -d lansurfer \
  -v ON_ERROR_STOP=1 >/dev/null <<'SQL'
BEGIN;
INSERT INTO users (id, game_nick) VALUES
  ('00000000-0000-4000-8000-000000000029', 'RestoreHost'),
  ('00000000-0000-4000-8000-000000000030', 'RestoreGuest');
INSERT INTO games (id, name, slug) VALUES
  ('00000000-0000-4000-8000-000000000031', 'Restore Fixture Game', 'restore-fixture-game');
INSERT INTO game_requests (id, game_id, creator_id, starts_at, description) VALUES
  ('00000000-0000-4000-8000-000000000032',
   '00000000-0000-4000-8000-000000000031',
   '00000000-0000-4000-8000-000000000029',
   now() + interval '3 days', 'Synthetic restore regression request');
INSERT INTO game_request_members (game_request_id, user_id) VALUES
  ('00000000-0000-4000-8000-000000000032',
   '00000000-0000-4000-8000-000000000030');
INSERT INTO game_request_messages (game_request_id, author_id, body) VALUES
  ('00000000-0000-4000-8000-000000000032',
   '00000000-0000-4000-8000-000000000030', 'Synthetic restore regression message');
INSERT INTO game_request_activity (game_request_id, actor_id, kind, origin) VALUES
  ('00000000-0000-4000-8000-000000000032',
   '00000000-0000-4000-8000-000000000029', 'created', 'backfill');
INSERT INTO notification_work
  (activity_sequence, game_request_id, actor_id, kind, occurred_at, baseline)
SELECT sequence, game_request_id, actor_id, kind, occurred_at, true
FROM game_request_activity
WHERE game_request_id = '00000000-0000-4000-8000-000000000032';
INSERT INTO notification_delivery (work_id, transport, target_key, status)
SELECT id, 'webpush', 'restore-fixture', 'succeeded'
FROM notification_work
WHERE game_request_id = '00000000-0000-4000-8000-000000000032';
INSERT INTO notification_push_preference (user_id, web_push_enabled) VALUES
  ('00000000-0000-4000-8000-000000000030', false);
COMMIT;
SQL

# Prove referential integrity and the restored VALUES, not just table counts.
assert_records() {
  local found
  found="$("${compose[@]}" exec -T postgres psql -X -U lansurfer -d "$1" -At \
    -v ON_ERROR_STOP=1 -c "
SELECT count(*)
FROM users host
JOIN game_requests req ON req.creator_id = host.id
JOIN games game ON game.id = req.game_id
JOIN game_request_members member ON member.game_request_id = req.id
JOIN users guest ON guest.id = member.user_id
JOIN game_request_messages message ON message.game_request_id = req.id AND message.author_id = guest.id
JOIN game_request_activity activity ON activity.game_request_id = req.id AND activity.actor_id = host.id
JOIN notification_work work ON work.activity_sequence = activity.sequence AND work.game_request_id = req.id
  AND work.actor_id = activity.actor_id AND work.kind = activity.kind
JOIN notification_delivery delivery ON delivery.work_id = work.id
JOIN notification_push_preference preference ON preference.user_id = guest.id
WHERE req.id = '00000000-0000-4000-8000-000000000032'
  AND host.game_nick = 'RestoreHost' AND guest.game_nick = 'RestoreGuest'
  AND game.slug = 'restore-fixture-game'
  AND req.description = 'Synthetic restore regression request'
  AND message.body = 'Synthetic restore regression message'
  AND activity.kind = 'created' AND activity.origin = 'backfill'
  AND work.baseline AND delivery.status = 'succeeded'
  AND delivery.target_key = 'restore-fixture' AND NOT preference.web_push_enabled")"
  [[ "$found" == 1 ]] || {
    echo "Database $1 did not preserve the synthetic user/game/request/notification graph" >&2
    return 1
  }
}
assert_records lansurfer

database_fingerprint() {
  "${compose[@]}" exec -T postgres psql -X -qAt -U lansurfer -d "$1" \
    -v ON_ERROR_STOP=1 <<'SQL' | sha256sum | cut -d ' ' -f 1
SELECT format(
  'SELECT %L, coalesce(jsonb_agg(to_jsonb(t) ORDER BY to_jsonb(t)::text), %L::jsonb) FROM %I.%I AS t;',
  schemaname || '.' || tablename, '[]', schemaname, tablename
)
FROM pg_tables
WHERE schemaname = 'public'
ORDER BY schemaname, tablename;
\gexec
SELECT schemaname, sequencename, last_value
FROM pg_sequences
WHERE schemaname = 'public'
ORDER BY schemaname, sequencename;
SQL
}

umask 077
tmp="$(mktemp -d)"
target="lansurfer_restore_ci"
created=false
cleanup() {
  status=$?
  trap - EXIT INT TERM
  if [[ "$created" == true ]] &&
    ! "${compose[@]}" exec -T postgres dropdb -U lansurfer "$target" >/dev/null 2>&1; then
    echo "Failed to remove isolated restore database" >&2
    (( status != 0 )) || status=1
  fi
  rm -rf -- "$tmp"
  exit "$status"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

source_before="$(database_fingerprint lansurfer)"
# pg_dump uses one transactionally consistent snapshot.
"${compose[@]}" exec -T postgres pg_dump -U lansurfer -d lansurfer \
  --format=custom --no-owner --no-acl > "$tmp/backup.dump"
[[ -s "$tmp/backup.dump" ]] || { echo "pg_dump returned an empty backup" >&2; exit 1; }
"${compose[@]}" exec -T postgres pg_restore --list < "$tmp/backup.dump" >/dev/null

# pg_restore only targets an empty NEW database, never the source.
"${compose[@]}" exec -T postgres createdb -U lansurfer "$target"
created=true
"${compose[@]}" exec -T postgres pg_restore -U lansurfer -d "$target" \
  --exit-on-error --no-owner --no-acl < "$tmp/backup.dump"
assert_current_schema "$target"
assert_records "$target"
[[ "$(database_fingerprint "$target")" == "$source_before" ]] || {
  echo "Restored database differs from source snapshot" >&2
  exit 1
}
[[ "$(database_fingerprint lansurfer)" == "$source_before" ]] || {
  echo "Source database changed during backup/restore" >&2
  exit 1
}
assert_current_schema lansurfer
assert_records lansurfer
echo "Current migration $expected and isolated pg_dump/pg_restore regression passed."
