#!/usr/bin/env bash
set -euo pipefail

# Exercise GoReleaser's real grouping engine without pushing tags or releases.
# This disposable Git repository contains no project data and no credentials.
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
fixture="$(mktemp -d)"
config_dir="$(mktemp -d)"
trap 'rm -rf "$fixture" "$config_dir"' EXIT

cd "$fixture"
git init -q -b main
git config user.name "LANsurfer release fixture"
git config user.email "fixture@example.invalid"
git remote add origin https://github.com/convention-x-treme/lansurfer.git
cp "$root/.goreleaser.yaml" ./
cp "$root/Dockerfile.release" ./
mkdir -p cmd/lansurfer
printf 'module example.invalid/release-fixture\n\ngo 1.27.1\n' > go.mod
printf 'package main\nfunc main() {}\n' > cmd/lansurfer/main.go
printf 'Fixture license for release-note tests\n' > LICENSE
printf 'Fixture project for release-note tests\n' > README.md
printf 'Fixture third-party license notices\n' > THIRD_PARTY_NOTICES.md
printf 'Fixture brand rights inventory\n' > ASSETS-LICENSE.md
printf '/dist/\n/last-build.log\n' > .gitignore
git add .
git commit -qm 'Import original legacy history'

tag() { git tag "$1"; }
commit() { git commit --allow-empty -qm "$1"; }
notes() {
  local release_tag="$1" previous_stable
  previous_stable="$(python3 "$root/scripts/release/notes_config.py" \
    "$release_tag" "$fixture/.goreleaser.yaml" "$config_dir/release.yaml")"
  if [[ -n "$previous_stable" ]]; then
    export GORELEASER_PREVIOUS_TAG="$previous_stable"
  else
    unset GORELEASER_PREVIOUS_TAG
  fi
  # --skip=publish prevents GitHub/GHCR publication; --skip=docker also
  # excludes Docker in full-release mode. Snapshot mode skips changelogs.
  goreleaser release --config "$config_dir/release.yaml" --clean --skip=publish,docker > "$fixture/last-build.log" 2>&1 || {
    cat "$fixture/last-build.log" >&2
    exit 1
  }
  test -s dist/CHANGELOG.md || {
    printf 'GoReleaser did not generate dist/CHANGELOG.md\n' >&2
    exit 1
  }
}
contains() {
  grep -Fq -- "$1" dist/CHANGELOG.md || {
    printf 'Expected release notes to contain: %s\n' "$1" >&2
    cat dist/CHANGELOG.md >&2
    exit 1
  }
}
absent() {
  if grep -Fq -- "$1" dist/CHANGELOG.md; then
    printf 'Release notes unexpectedly contain: %s\n' "$1" >&2
    cat dist/CHANGELOG.md >&2
    exit 1
  fi
}

# The first stable release must include the entire project history even
# though a prerelease already exists (not just the post-RC fixes).
tag v1.0.0-rc.1
notes v1.0.0-rc.1
contains 'Import original legacy history'
commit 'fix(auth): finish first stable release'
tag v1.0.0
notes v1.0.0
contains '### Other changes'
contains 'Import original legacy history'
contains 'fix(auth): finish first stable release'

# Next release: conventional scopes, breaking marker, and internal-only noise.
commit 'feat(gamerequest): add request filters'
commit 'fix(auth): handle expired login'
commit 'perf(db): speed up queries'
commit 'refactor(auth)!: remove legacy login'
commit 'docs(repo): adjust contributor guidance'
commit 'ci(release): update workflow tests'
tag v1.1.0
notes v1.1.0
contains '### Breaking changes'
contains 'refactor(auth)!: remove legacy login'
contains '### Features'
contains 'feat(gamerequest): add request filters'
contains '### Bug fixes'
contains 'fix(auth): handle expired login'
contains '### Performance'
contains 'perf(db): speed up queries'
absent 'docs(repo): adjust contributor guidance'
absent 'ci(release): update workflow tests'
absent 'Import original legacy history'

# Prerelease uses only commits since the last reachable release tag.
git branch release/v1
commit 'feat(game): prepare next major version'
tag v2.0.0-rc.1
notes v2.0.0-rc.1
contains 'feat(game): prepare next major version'
absent 'feat(gamerequest): add request filters'

# A backport on the older major branch must not acquire v2 changes.
git switch -q release/v1
commit 'fix(auth): backport expiry handling'
tag v1.1.1
notes v1.1.1
contains 'fix(auth): backport expiry handling'
absent 'feat(game): prepare next major version'
absent 'feat(gamerequest): add request filters'

# Internal-only release: should not invent user-facing release items.
commit 'chore(repo): adjust internal metadata'
tag v1.1.2
notes v1.1.2
absent 'chore(repo): adjust internal metadata'

# Stable maintenance release includes its prerelease feature and new fix,
# without leaking unrelated v2 commits into the v1 branch.
commit 'feat(game): prepare maintenance version'
tag v1.2.0-rc.1
notes v1.2.0-rc.1
commit 'fix(game): finish maintenance version'
tag v1.2.0
notes v1.2.0
contains 'feat(game): prepare maintenance version'
contains 'fix(game): finish maintenance version'
absent 'feat(game): prepare next major version'
absent 'feat(gamerequest): add request filters'

# Mainline v2 includes its RC commit as well as final-stage changes, but no
# previously published stable commits or maintenance-branch backports.
git switch -q main
commit 'feat(game): finish next major version'
tag v2.0.0
notes v2.0.0
contains 'feat(game): prepare next major version'
contains 'feat(game): finish next major version'
absent 'feat(game): prepare maintenance version'
absent 'feat(gamerequest): add request filters'

printf 'GoReleaser changelog fixtures passed (first stable after RC, groups, cumulative stable, maintenance, empty).\n'
