#!/usr/bin/env bash
set -euo pipefail

if [[ $# -ne 1 ]]; then
  printf 'Usage: %s "type(scope): summary"\n' "$0" >&2
  exit 2
fi

# Keep the grammar small: release notes will consume final squash subjects.
# PR titles are untrusted input, so never evaluate or execute their contents.
LC_ALL=C
title=$1
pattern='^(feat|fix|perf|refactor|docs|test|build|ci|chore|revert)\([a-z][a-z0-9]*(-[a-z0-9]+)*\)!?: '

if [[ $title =~ $pattern ]]; then
  summary=${title#"${BASH_REMATCH[0]}"}
  if [[ -n $summary && $summary != [[:space:]]* && $summary != *[[:space:]] && $summary != *'.' && $summary != *$'\n'* && $summary != *$'\r'* ]]; then
    printf 'Valid Conventional Commit PR title.\n'
    exit 0
  fi
fi

printf '::error::PR title must use type(scope): summary or type(scope)!: summary.\n' >&2
printf 'Allowed types: feat, fix, perf, refactor, docs, test, build, ci, chore, revert.\n' >&2
printf 'Use a lowercase, hyphen-separated scope and a one-line summary without a trailing period.\n' >&2
printf 'Example: fix(gamerequest): prevent duplicate joins\n' >&2
exit 1
