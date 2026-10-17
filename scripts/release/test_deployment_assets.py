"""Offline regression tests for version-matched production Compose assets."""

import hashlib
import re
import tempfile
import unittest
from pathlib import Path

from render_deployment import ASSETS, MARKER, render_deployment

ROOT = Path(__file__).resolve().parents[2]
DIGEST = "sha256:" + "a" * 64


class DeploymentReleaseAssetsTest(unittest.TestCase):
    def test_matching_image_and_checksum_assets(self):
        for tag in ("v0.2.0", "v0.2.0-rc.1", "v1.0.0"):
            with self.subTest(tag=tag), tempfile.TemporaryDirectory() as temp:
                output = Path(temp) / "package"
                render_deployment(tag, DIGEST, output)
                env = (output / "production.env.example").read_text(encoding="utf-8")
                image = f"ghcr.io/convention-x-treme/lansurfer:{tag}@{DIGEST}"
                self.assertEqual(re.findall(r"(?m)^LANSURFER_IMAGE=(.*)$", env), [image])
                self.assertNotIn(MARKER, env)
                self.assertIn("POSTGRES_IMAGE=postgres:", env)
                self.assertIn("services:", (output / "compose.yaml").read_text())
                self.assertIn("TRAEFIK_HOST", (output / "compose.traefik.yaml").read_text())
                lines = (output / "deployment-checksums.txt").read_text().splitlines()
                self.assertEqual(len(lines), len(ASSETS))
                for line, filename in zip(lines, ASSETS):
                    sha, named = line.split()
                    self.assertEqual(named, filename)
                    self.assertEqual(sha, hashlib.sha256((output / filename).read_bytes()).hexdigest())

    def test_invalid_versions_and_digests_are_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            for tag, digest in (
                ("0.2.0", DIGEST),
                ("v0.2.0;bad", DIGEST),
                ("v0.2.0", "sha256:123"),
                ("v0.2.0", "sha256:" + "G" * 64),
            ):
                with self.subTest(tag=tag, digest=digest), self.assertRaises(ValueError):
                    render_deployment(tag, digest, Path(temp) / "unused")

    def test_missing_marker_and_overwrite_are_rejected(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp) / "fake-repo"
            deploy = root / "deploy"
            deploy.mkdir(parents=True)
            (deploy / "production.env.example").write_text("LANSURFER_IMAGE=\n")
            with self.assertRaisesRegex(ValueError, "Exactly one"):
                render_deployment("v0.2.0", DIGEST, Path(temp) / "absent", root=root)
            target = Path(temp) / "exists"
            render_deployment("v0.2.0", DIGEST, target, root=ROOT)
            with self.assertRaises(FileExistsError):
                render_deployment("v0.2.0", DIGEST, target, root=ROOT)


if __name__ == "__main__":
    unittest.main()
