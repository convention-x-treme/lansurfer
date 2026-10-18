#!/usr/bin/env bash
set -euo pipefail
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
tmp="$(mktemp -d)"
trap 'rm -rf -- "$tmp"' EXIT
required=(lansurfer LICENSE README.md THIRD_PARTY_NOTICES.md ASSETS-LICENSE.md)
printf 'synthetic executable\n' > "$tmp/lansurfer"
for name in LICENSE README.md THIRD_PARTY_NOTICES.md ASSETS-LICENSE.md; do cp "$root/$name" "$tmp/$name"; done
make_archive() { tar -czf "$2" -C "$tmp" "${@:3}"; }
check_reject() {
  if bash "$root/scripts/release/verify_archive_contents.sh" "$1"; then
    echo "Archive verifier accepted invalid archive: $2" >&2
    exit 1
  fi
}
make_archive "$tmp" "$tmp/good.tar.gz" "${required[@]}"
bash "$root/scripts/release/verify_archive_contents.sh" "$tmp/good.tar.gz"
for missing in "${required[@]}"; do
  members=()
  for name in "${required[@]}"; do [[ "$name" == "$missing" ]] || members+=("$name"); done
  make_archive "$tmp" "$tmp/missing-$missing.tar.gz" "${members[@]}"
  check_reject "$tmp/missing-$missing.tar.gz" "missing $missing"
done
: > "$tmp/THIRD_PARTY_NOTICES.md"
make_archive "$tmp" "$tmp/empty-notice.tar.gz" "${required[@]}"
check_reject "$tmp/empty-notice.tar.gz" 'empty notice'
cp "$root/THIRD_PARTY_NOTICES.md" "$tmp/THIRD_PARTY_NOTICES.md"
printf 'wrong notice\n' > "$tmp/THIRD_PARTY_NOTICES.md"
make_archive "$tmp" "$tmp/wrong-notice.tar.gz" "${required[@]}"
check_reject "$tmp/wrong-notice.tar.gz" 'wrong notice content'
cp "$root/THIRD_PARTY_NOTICES.md" "$tmp/THIRD_PARTY_NOTICES.md"
mv "$tmp/lansurfer" "$tmp/lansurfer.file"
mkdir "$tmp/lansurfer"
make_archive "$tmp" "$tmp/directory.tar.gz" "${required[@]}"
check_reject "$tmp/directory.tar.gz" 'directory member'
rmdir "$tmp/lansurfer"
mv "$tmp/lansurfer.file" "$tmp/lansurfer"
mv "$tmp/lansurfer" "$tmp/lansurfer.file"
ln -s lansurfer.file "$tmp/lansurfer"
make_archive "$tmp" "$tmp/symlink.tar.gz" "${required[@]}"
check_reject "$tmp/symlink.tar.gz" 'symlink member'
rm "$tmp/lansurfer"
mv "$tmp/lansurfer.file" "$tmp/lansurfer"
tar -czf "$tmp/duplicate.tar.gz" -C "$tmp" "${required[@]}" lansurfer
check_reject "$tmp/duplicate.tar.gz" 'duplicate member'
printf 'not tar' | gzip -c > "$tmp/invalid-tar.tar.gz"
check_reject "$tmp/invalid-tar.tar.gz" 'invalid tar'
printf 'not gzip' > "$tmp/invalid.tar.gz"
check_reject "$tmp/invalid.tar.gz" 'invalid gzip/tar'
head -c -8 "$tmp/good.tar.gz" > "$tmp/truncated.tar.gz"
check_reject "$tmp/truncated.tar.gz" 'truncated gzip'
