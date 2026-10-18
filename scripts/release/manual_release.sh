#!/usr/bin/env bash
# Plan a manual RC/stable tag, or create an immutable annotated tag after CI.
# GITHUB_TOKEN pushes do not start the separate tag-triggered release workflow.
set -euo pipefail

die() { printf 'Release selection rejected: %s\n' "$*" >&2; exit 1; }
[[ "$#" -ge 1 ]] || die "usage: manual_release.sh plan VERSION rc|stable | create TAG TESTED_SHA"

case "$1" in
  plan)
    [[ "$#" -eq 3 ]] || die "expected plan VERSION rc|stable"
    base="$2"
    channel="$3"
    [[ "$base" =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$ ]] ||
      die "version must be MAJOR.MINOR.PATCH without v, suffix or leading zeros"
    [[ "$channel" == rc || "$channel" == stable ]] ||
      die "release type must be rc or stable"
    stable_tag="v$base"
    if git show-ref --verify --quiet "refs/tags/$stable_tag"; then
      die "$stable_tag already exists; never reuse or move a release tag"
    fi
    if [[ "$channel" == stable ]]; then
      printf '%s\n' "$stable_tag"
      exit 0
    fi
    escaped_base="${base//./\\.}"
    max=0
    while IFS= read -r existing; do
      if [[ "$existing" =~ ^v$escaped_base-rc\.([1-9][0-9]{0,8})$ ]]; then
        number="${BASH_REMATCH[1]}"
        (( number > max )) && max="$number"
      fi
    done < <(git tag --list "v$base-rc.*")
    (( max < 999999999 )) || die "RC sequence exhausted"
    printf 'v%s-rc.%d\n' "$base" "$((max+1))"
    ;;
  create)
    [[ "$#" -eq 3 ]] || die "expected create TAG TESTED_SHA"
    tag="$2"
    tested_sha="$3"
    [[ "$tag" =~ ^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-rc\.([1-9][0-9]*))?$ ]] ||
      die "invalid stable/RC tag"
    [[ "$tested_sha" =~ ^[0-9a-f]{40}$ ]] || die "invalid tested commit SHA"
    [[ "${GITHUB_EVENT_NAME:-}" == workflow_dispatch && "${GITHUB_REF:-}" == refs/heads/main ]] ||
      die "tag creation only permitted by manual workflow on main"
    [[ "${GITHUB_SHA:-}" == "$tested_sha" ]] ||
      die "the manual workflow is not running on the tested commit"
    [[ "$(git rev-parse HEAD)" == "$tested_sha" ]] ||
      die "checkout differs from the CI-tested commit"
    [[ -z "$(git status --porcelain)" ]] || die "checkout must be clean"
    [[ -n "${GH_TOKEN:-}" ]] || die "no temporary GitHub token supplied"
    if git show-ref --verify --quiet "refs/tags/$tag"; then
      die "tag $tag already exists locally"
    fi

    # Only temporary Git askpass; no token in git config, logs or argv.
    umask 077
    auth_dir="$(mktemp -d)"
    trap 'rm -rf "$auth_dir"' EXIT
    cat > "$auth_dir/askpass" <<'ASKPASS'
#!/bin/sh
case "$1" in
  *Username*) printf 'x-access-token\n' ;;
  *Password*) printf '%s\n' "${GH_TOKEN:?}" ;;
  *) exit 1 ;;
esac
ASKPASS
    chmod 700 "$auth_dir/askpass"
    authenticated_git() {
      GIT_TERMINAL_PROMPT=0 GIT_ASKPASS="$auth_dir/askpass" git "$@"
    }
    remote_main="$(authenticated_git ls-remote origin refs/heads/main)"
    remote_main="${remote_main%%$'\t'*}"
    [[ "$remote_main" == "$tested_sha" ]] ||
      die "main moved after CI; refusing to tag untested source"
    if [[ -n "$(authenticated_git ls-remote --refs origin "refs/tags/$tag")" ]]; then
      die "remote tag $tag already exists; never overwrite"
    fi
    git -c user.name='LANsurfer Release Bot' \
      -c user.email='41898282+github-actions[bot]@users.noreply.github.com' \
      tag -a "$tag" -m "LANsurfer $tag" "$tested_sha"
    authenticated_git push origin "refs/tags/$tag:refs/tags/$tag"
    local_object="$(git rev-parse "refs/tags/$tag")"
    remote_object="$(authenticated_git ls-remote --refs origin "refs/tags/$tag")"
    remote_object="${remote_object%%$'\t'*}"
    [[ "$remote_object" == "$local_object" ]] ||
      die "remote annotated tag does not match the local object"
    printf 'Created immutable release tag %s on %s\n' "$tag" "$tested_sha"
    ;;
  *)
    die "usage: manual_release.sh plan VERSION rc|stable | create TAG TESTED_SHA"
    ;;
esac
