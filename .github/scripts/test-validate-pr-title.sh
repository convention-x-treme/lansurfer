#!/usr/bin/env bash
set -euo pipefail

validator="$(dirname "${BASH_SOURCE[0]}")/validate-pr-title.sh"

accept() {
  if ! bash "$validator" "$1" >/dev/null 2>&1; then
    printf 'Expected valid PR title: %q\n' "$1" >&2
    exit 1
  fi
}

reject() {
  if bash "$validator" "$1" >/dev/null 2>&1; then
    printf 'Expected invalid PR title: %q\n' "$1" >&2
    exit 1
  fi
}

accept 'feat(gamerequest): add capacity filter'
accept 'fix(auth): handle expired sessions'
accept 'feat(auth)!: remove legacy callback'
accept 'chore(deps): Bump grouped dependencies'
accept 'ci(release-pipeline): validate tag inputs'
accept 'revert(web): restore previous layout'
accept 'docs(repo): clarify the contribution guide'
accept 'perf(db): optimize queries'
accept 'refactor(notification): simplify workers'
accept 'test(web): cover repeated clicks'
accept 'build(docker): reduce image size'

reject 'feat: add capacity filter'
reject 'feat(): add capacity filter'
reject 'feature(gamerequest): add capacity filter'
reject 'Feat(gamerequest): add capacity filter'
reject 'fix(GameRequest): handle invalid joins'
reject 'fix(bad_scope): handle invalid joins'
reject 'fix(scope-): handle invalid joins'
reject 'fix(auth):'
reject 'fix(auth): '
reject 'fix(auth):  leading whitespace'
reject 'fix(auth): trailing whitespace '
reject 'fix(auth): end with a period.'
reject $'fix(auth): two\nlines'
reject $'fix(auth): carriage\rreturn'
reject 'fix(auth): malformed  '
reject 'Merge pull request #84'

printf 'PR title validation: all positive and negative cases passed.\n'
