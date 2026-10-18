#!/usr/bin/env bash
set -euo pipefail

root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
cat > "$tmp/lansurfer" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "${VERSION_OUTPUT:?}"
EOF
chmod +x "$tmp/lansurfer"
tar -czf "$tmp/lansurfer.tar.gz" -C "$tmp" lansurfer

commit=0123456789abcdef0123456789abcdef01234567
VERSION_OUTPUT="LANsurfer v0.2.0 ($commit)" \
  bash "$root/scripts/release/verify_cli_artifact.sh" "$tmp/lansurfer.tar.gz" release v0.2.0 "$commit"
if VERSION_OUTPUT="LANsurfer v0.2.1 ($commit)" \
  bash "$root/scripts/release/verify_cli_artifact.sh" "$tmp/lansurfer.tar.gz" release v0.2.0 "$commit"; then
  echo "Release verifier accepted mismatched version" >&2
  exit 1
fi
if VERSION_OUTPUT="LANsurfer v0.2.0 (fedcba9876543210fedcba9876543210fedcba98)" \
  bash "$root/scripts/release/verify_cli_artifact.sh" "$tmp/lansurfer.tar.gz" release v0.2.0 "$commit"; then
  echo "Release verifier accepted mismatched commit" >&2
  exit 1
fi
VERSION_OUTPUT="LANsurfer v0.0.0-SNAPSHOT-20261018 ($commit)" \
  bash "$root/scripts/release/verify_cli_artifact.sh" "$tmp/lansurfer.tar.gz" snapshot "" "$commit"
if VERSION_OUTPUT="LANsurfer v0.2.0 ($commit)" \
  bash "$root/scripts/release/verify_cli_artifact.sh" "$tmp/lansurfer.tar.gz" snapshot "" "$commit"; then
  echo "Snapshot verifier accepted non-snapshot version" >&2
  exit 1
fi
