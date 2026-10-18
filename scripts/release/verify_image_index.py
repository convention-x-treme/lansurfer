#!/usr/bin/env python3
"""Verify the exact OCI/Docker image-index bytes returned by buildx --raw.

The registry's image-index digest is the SHA-256 hash of the original
manifest bytes. Do not parse and re-serialize the manifest before hashing:
that would change the digest even though the JSON has the same meaning.
"""

import hashlib
import json
import re
import sys
from collections import Counter
from pathlib import Path

INDEX_MEDIA_TYPES = frozenset(
    {
        "application/vnd.oci.image.index.v1+json",
        "application/vnd.docker.distribution.manifest.list.v2+json",
    }
)
DIGEST_RE = re.compile(r"sha256:[a-f0-9]{64}\Z")
EXPECTED_PLATFORMS = Counter({"amd64": 1, "arm64": 1})


def verify_image_index(raw: bytes) -> str:
    """Return the registry index digest only if the two Linux images exist."""
    if not raw:
        raise ValueError("Empty registry image-index manifest")
    try:
        index = json.loads(raw)
    except (ValueError, UnicodeDecodeError) as exc:
        raise ValueError("Invalid registry image-index JSON") from exc
    if not isinstance(index, dict):
        raise ValueError("Registry image index must be a JSON object")
    if index.get("schemaVersion") != 2 or index.get("mediaType") not in INDEX_MEDIA_TYPES:
        raise ValueError("Expected an OCI index or Docker manifest list")

    descriptors = index.get("manifests")
    if not isinstance(descriptors, list):
        raise ValueError("Registry index does not include a manifest list")
    platforms = Counter()
    for desc in descriptors:
        if not isinstance(desc, dict) or not isinstance(desc.get("platform"), dict):
            raise ValueError("Invalid registry index descriptor")
        digest = desc.get("digest")
        if not isinstance(digest, str) or not DIGEST_RE.fullmatch(digest):
            raise ValueError("Invalid child manifest digest")
        platform = desc["platform"]
        os_name = platform.get("os")
        architecture = platform.get("architecture")
        if os_name == "unknown" and architecture == "unknown":
            # Optional build attestations can be listed as unknown/unknown.
            continue
        if os_name != "linux" or architecture not in EXPECTED_PLATFORMS:
            raise ValueError("Unexpected image platform in registry index")
        platforms[architecture] += 1

    if platforms != EXPECTED_PLATFORMS:
        raise ValueError("Expected one linux/amd64 and one linux/arm64 image")
    return "sha256:" + hashlib.sha256(raw).hexdigest()


def main() -> int:
    if len(sys.argv) != 2:
        print("usage: verify_image_index.py RAW_MANIFEST_PATH", file=sys.stderr)
        return 2
    try:
        digest = verify_image_index(Path(sys.argv[1]).read_bytes())
    except (OSError, ValueError) as exc:
        print(f"Release image-index verification failed: {exc}", file=sys.stderr)
        return 1
    print(digest)
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
