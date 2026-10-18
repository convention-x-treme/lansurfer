#!/usr/bin/env bash
# Check archive payloads and bundled licensing against this checkout.
set -euo pipefail
archive="${1:?Usage: verify_archive_contents.sh ARCHIVE}"
root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
python3 - "$archive" "$root" <<'PY'
import gzip
import hashlib
import pathlib
import sys
import tarfile

archive = pathlib.Path(sys.argv[1])
root = pathlib.Path(sys.argv[2])
required = ("lansurfer", "LICENSE", "README.md", "THIRD_PARTY_NOTICES.md", "ASSETS-LICENSE.md")
documents = required[1:]

def fail(message):
    print(message, file=sys.stderr)
    raise SystemExit(1)

try:
    with gzip.open(archive, "rb") as stream:
        while stream.read(1024 * 1024):
            pass
    with tarfile.open(archive, "r:gz") as tar:
        members = tar.getmembers()
        by_name = {}
        for member in members:
            by_name.setdefault(member.name.rstrip("/"), []).append(member)
        for name in required:
            found = by_name.get(name, [])
            if len(found) != 1:
                fail(f"Release archive must contain exactly one {name}")
            member = found[0]
            if not member.isfile() or member.size <= 0:
                fail(f"Release archive member is not a non-empty regular file: {name}")
            source = tar.extractfile(member)
            if source is None:
                fail(f"Cannot read release archive member: {name}")
            digest = hashlib.sha256()
            size = 0
            while chunk := source.read(1024 * 1024):
                size += len(chunk)
                digest.update(chunk)
            if size != member.size:
                fail(f"Release archive member payload is truncated: {name}")
            if name in documents:
                expected = hashlib.sha256((root / name).read_bytes()).digest()
                if digest.digest() != expected:
                    fail(f"Release archive document differs from checkout: {name}")
except (OSError, EOFError, tarfile.TarError) as error:
    fail(f"Cannot read release archive: {type(error).__name__}")
print("Verified release archive contents.")
PY
