#!/usr/bin/env bash
set -euo pipefail

# notes_config.py imports release_policy.py immediately before GoReleaser.
# Never generate untracked Python bytecode in the tagged checkout.
export PYTHONDONTWRITEBYTECODE=1

# Called ONLY by the privileged, serialized tag-release job after reusable CI.
# No automatic rerun after a partial registry/GitHub draft publication: recovery
# requires deliberate manual verification or a NEW version (see CONTRIBUTING.md).
repo="${GITHUB_REPOSITORY:?GitHub repository is required}"
tag="${GITHUB_REF_NAME:?Release tag is required}"
image="ghcr.io/${repo,,}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
policy="$root/scripts/release/release_policy.py"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

if [[ "${GITHUB_REF:?}" != "refs/tags/$tag" ]]; then
  echo "::error::Release publication requires an immutable version tag push" >&2
  exit 1
fi
python3 "$policy" validate "$tag"

verify_source_tag() {
  local local_ref remote_ref
  local_ref="$(git rev-parse "refs/tags/$tag")"
  remote_ref="$(gh api "repos/$repo/git/ref/tags/$tag" --jq '.object.sha')"
  if [[ "$local_ref" != "$remote_ref" ]]; then
    echo "::error::Remote release tag moved since checkout; refusing to publish" >&2
    exit 1
  fi
  if [[ "$(git rev-parse "refs/tags/$tag^{commit}")" != "$(git rev-parse HEAD)" ]]; then
    echo "::error::Working tree HEAD is not the tagged commit" >&2
    exit 1
  fi
}

# Enumerating ALL pages, including drafts, prevents accidental overwrites and
# compares true semantic versions instead of timestamps or lexical strings.
verify_source_tag
gh api --paginate --slurp "repos/$repo/releases?per_page=100" > "$tmp/releases.json"
python3 "$policy" check-new "$tag" "$tmp/releases.json"

# Authenticate ONLY in this release job. GITHUB_TOKEN has package write here,
# not in pull request CI or release snapshot workflows.
printf '%s' "${GH_TOKEN:?}" |
  docker login ghcr.io --username "${GITHUB_ACTOR:?}" --password-stdin

# Exact version tags are immutable. Distinguish expected "not found" from
# registry/network/permission failures and fail closed on uncertain responses.
if docker buildx imagetools inspect "$image:$tag" > "$tmp/version-probe.txt" 2> "$tmp/probe-error.txt"; then
  echo "::error::Exact GHCR image tag already exists: $image:$tag" >&2
  exit 1
elif ! grep -Eiq 'not found|manifest unknown|name unknown|404' "$tmp/probe-error.txt"; then
  echo "::error::Unable to confirm exact image tag is unused (not a 404)" >&2
  cat "$tmp/probe-error.txt" >&2
  exit 1
fi

# Stable release notes must cover everything since the preceding stable
# release, not just changes after a release candidate. On the first stable
# version, excluding local prerelease tags makes GoReleaser include all history.
# This produces a temporary OSS config (no Git ref changes, no Pro features).
notes_config="$tmp/goreleaser-notes.yaml"
previous_stable="$(python3 "$root/scripts/release/notes_config.py" \
  "$tag" "$root/.goreleaser.yaml" "$notes_config")"
if [[ -n "$previous_stable" ]]; then
  export GORELEASER_PREVIOUS_TAG="$previous_stable"
  echo "Changelog base (previous reachable stable release): $previous_stable"
else
  unset GORELEASER_PREVIOUS_TAG
  echo "Changelog base: no previous stable release, or normal prerelease mode"
fi

# GoReleaser uploads a GitHub DRAFT, Linux archives and checksums, then pushes
# the exact-version multi-arch container image. The GitHub Release remains
# unpublished until downstream verification and alias promotion finish.
goreleaser release --config "$notes_config" --clean --fail-fast

test -s dist/checksums.txt
(cd dist && sha256sum --check checksums.txt)
jq -e '[.[] | select(.type == "Archive") | .goarch] | sort == ["amd64","arm64"]' dist/artifacts.json

verify_source_tag
# GitHub's /releases/tags/{tag} lookup can return HTTP 404 for an unpublished
# DRAFT even though it exists. Find the exact version in the paginated release
# collection instead; fail closed if missing or duplicated.
gh api --paginate --slurp "repos/$repo/releases?per_page=100" > "$tmp/releases.json"
python3 "$policy" lookup "$tag" "$tmp/releases.json" > "$tmp/release.json"
if [[ "$tag" == *-* ]]; then
  expected_prerelease=true
else
  expected_prerelease=false
fi

jq -e --arg tag "$tag" --argjson prerelease "$expected_prerelease" '
  .tag_name == $tag and .draft == true and .prerelease == $prerelease and
  ([.assets[] | select(.name == "checksums.txt" and .size > 0)] | length == 1) and
  ([.assets[] | select((.name | endswith(".tar.gz")) and .size > 0)] | length == 2) and
  (.body | contains("Changelog"))
' "$tmp/release.json"

docker buildx imagetools inspect --raw "$image:$tag" > "$tmp/image.json"
jq -e '
  ([.manifests[]?.platform | select(.os == "linux") | .architecture] | unique | sort)
    == ["amd64", "arm64"]
' "$tmp/image.json"

# Resolve the actual multi-architecture index digest before attaching ANY
# self-hosting files. Never publish an old image pin for a new release.
exact_digest="$(docker buildx imagetools inspect "$image:$tag" | awk '/^Digest:/ { print $2; exit }')"
[[ "$exact_digest" =~ ^sha256:[a-f0-9]{64}$ ]] || {
  echo "::error::Unable to verify GHCR multi-platform digest" >&2
  exit 1
}
deployment_dir="$tmp/deployment"
python3 "$root/scripts/release/render_deployment.py" "$tag" "$exact_digest" "$deployment_dir"
(cd "$deployment_dir" && sha256sum --check deployment-checksums.txt)
# A tag release has a freshly built image. Boot THAT exact image (including
# migrations and the new imprint setting) before the draft can be published.
# This is separate from PR CI's existing v0.1.0 compatibility regression.
bash "$root/scripts/release/smoke_deployment.sh" "$deployment_dir"

# Upload deployment assets to the unpublished draft via its known release ID.
# Drafts are not reliably addressable through GET /releases/tags/{tag}.
# Fail closed if an asset cannot be uploaded or checksummed.
release_id="$(jq -r '.id' "$tmp/release.json")"
for name in compose.yaml compose.traefik.yaml production.env.example deployment-checksums.txt; do
  # Use the upload_url host directly; gh api --hostname uploads.github.com
  # incorrectly targets api.uploads.github.com. Supply the token via curl's
  # stdin config rather than making it visible in process command arguments.
  printf 'header = "Authorization: Bearer %s"\n' "$GH_TOKEN" |
    curl --fail-with-body --silent --show-error \
      --config - -X POST \
      -H 'Content-Type: application/octet-stream' \
      --data-binary "@$deployment_dir/$name" \
      "https://uploads.github.com/repos/$repo/releases/$release_id/assets?name=$name" \
      > "$tmp/upload-$name.json"
  expected_sha="$(sha256sum "$deployment_dir/$name" | awk '{ print $1 }')"
  jq -e --arg name "$name" --arg digest "sha256:$expected_sha" '
    .name == $name and .state == "uploaded" and .size > 0 and .digest == $digest
  ' "$tmp/upload-$name.json" >/dev/null
done
gh api "repos/$repo/releases/$release_id/assets?per_page=100" > "$tmp/deployment-assets.json"
for name in compose.yaml compose.traefik.yaml production.env.example deployment-checksums.txt; do
  jq -e --arg name "$name" '
    [.[] | select(.name == $name and .state == "uploaded" and .size > 0)] | length == 1
  ' "$tmp/deployment-assets.json" >/dev/null
done

# Re-read published releases after the draft is created. The policy excludes
# drafts and prereleases and refuses to move aliases backwards when an older
# major/minor release ships after a newer one.
gh api --paginate --slurp "repos/$repo/releases?per_page=100" > "$tmp/releases.json"
python3 "$policy" plan "$tag" "$tmp/releases.json" > "$tmp/plan.json"
jq -r '.aliases[]' "$tmp/plan.json" > "$tmp/aliases.txt"
jq -e --arg tag "$tag" '.tag == $tag' "$tmp/plan.json" >/dev/null

verify_source_tag
alias_args=()
while IFS= read -r alias; do
  [[ -n "$alias" ]] || continue
  alias_args+=(--tag "$image:$alias")
done < "$tmp/aliases.txt"
if (("${#alias_args[@]}" > 0)); then
  docker buildx imagetools create "${alias_args[@]}" "$image:$tag"
  while IFS= read -r alias; do
    [[ -n "$alias" ]] || continue
    alias_digest="$(docker buildx imagetools inspect "$image:$alias" | awk '/^Digest:/ { print $2; exit }')"
    if [[ "$alias_digest" != "$exact_digest" ]]; then
      echo "::error::Alias $alias does not point to exact version digest" >&2
      exit 1
    fi
  done < "$tmp/aliases.txt"
fi

# Public visibility is the LAST transition. Do not claim success while the
# release is still a draft. The 'latest' GitHub badge follows the SAME SemVer
# decision as GHCR latest, never the publishing timestamp or older backports.
verify_source_tag
release_id="$(jq -r '.id' "$tmp/release.json")"
make_latest="$(jq -r '.make_latest | tostring' "$tmp/plan.json")"
gh api --method PATCH "repos/$repo/releases/$release_id" \
  -F draft=false -f make_latest="$make_latest" > "$tmp/published.json"
jq -e --arg tag "$tag" --argjson prerelease "$expected_prerelease" '
  .tag_name == $tag and .draft == false and .prerelease == $prerelease
' "$tmp/published.json" >/dev/null

{
  echo "## LANsurfer release $tag published"
  echo
  echo "- Source: \`$(git rev-parse HEAD)\`"
  echo "- GitHub Release: https://github.com/$repo/releases/tag/$tag"
  echo "- Multi-arch image: \`$image:$tag\` (linux/amd64, linux/arm64)"
  echo '- Self-hosting downloads: compose.yaml, compose.traefik.yaml, production.env.example, deployment-checksums.txt'
  echo "- Updated GHCR aliases: $(paste -sd ', ' "$tmp/aliases.txt" || true)"
  echo "- GitHub latest: \`$make_latest\`"
} >> "${GITHUB_STEP_SUMMARY:?}"
