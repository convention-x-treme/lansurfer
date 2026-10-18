"""Offline safety regressions for the content-addressed release image index."""

import hashlib
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path

from verify_image_index import verify_image_index

ROOT = Path(__file__).resolve().parent


def descriptor(arch: str, os_name: str = "linux") -> dict:
    return {
        "mediaType": "application/vnd.oci.image.manifest.v1+json",
        "digest": "sha256:" + ("a" if arch == "amd64" else "b") * 64,
        "size": 1024,
        "platform": {"architecture": arch, "os": os_name},
    }


def raw_index(*descriptors: dict, media_type: str = "application/vnd.oci.image.index.v1+json") -> bytes:
    return json.dumps(
        {"schemaVersion": 2, "mediaType": media_type, "manifests": list(descriptors)},
        separators=(",", ":"),
    ).encode("utf-8")


class VerifyImageIndexTest(unittest.TestCase):
    def test_digest_is_of_the_exact_raw_manifest_not_reformatted_json(self):
        raw = raw_index(descriptor("amd64"), descriptor("arm64"))
        self.assertEqual(verify_image_index(raw), "sha256:" + hashlib.sha256(raw).hexdigest())
        spaced = raw + b"\n"
        self.assertEqual(verify_image_index(spaced), "sha256:" + hashlib.sha256(spaced).hexdigest())
        self.assertNotEqual(verify_image_index(spaced), verify_image_index(raw))

    def test_docker_manifest_list_is_accepted(self):
        raw = raw_index(
            descriptor("amd64"), descriptor("arm64"),
            media_type="application/vnd.docker.distribution.manifest.list.v2+json",
        )
        self.assertTrue(verify_image_index(raw).startswith("sha256:"))

    def test_attestation_unknown_unknown_does_not_change_platform_requirements(self):
        raw = raw_index(descriptor("amd64"), descriptor("arm64"), descriptor("unknown", "unknown"))
        self.assertTrue(verify_image_index(raw).startswith("sha256:"))

    def test_malformed_index_or_missing_platform_is_rejected(self):
        examples = (
            b"",
            b"garbage",
            b"[]",
            raw_index(descriptor("amd64")),
            raw_index(descriptor("amd64"), descriptor("amd64")),
            raw_index(descriptor("amd64"), descriptor("arm64"), descriptor("arm")),
            raw_index(descriptor("amd64"), descriptor("arm64"), media_type="application/vnd.oci.image.manifest.v1+json"),
        )
        for raw in examples:
            with self.subTest(raw=raw[:70]), self.assertRaises(ValueError):
                verify_image_index(raw)

    def test_invalid_child_manifest_digest_is_rejected(self):
        bad = descriptor("amd64")
        bad["digest"] = "sha256:short"
        with self.assertRaisesRegex(ValueError, "child manifest digest"):
            verify_image_index(raw_index(bad, descriptor("arm64")))

    def test_cli_fails_closed_on_invalid_index(self):
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / "raw.json"
            path.write_bytes(raw_index(descriptor("amd64")))
            result = subprocess.run(
                [sys.executable, str(ROOT / "verify_image_index.py"), str(path)],
                text=True, capture_output=True, check=False,
            )
        self.assertEqual(result.returncode, 1)
        self.assertEqual(result.stdout, "")
        self.assertIn("Release image-index verification failed", result.stderr)


if __name__ == "__main__":
    unittest.main()
