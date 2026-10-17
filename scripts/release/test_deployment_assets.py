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
                self.assertNotIn("__LANSURFER_DEPLOYMENT_RELEASE_HEADER__", env)
                self.assertIn(f"# LANsurfer {tag} production configuration.", env)
                self.assertIn(f"https://github.com/convention-x-treme/lansurfer/blob/{tag}/docs/CONFIGURATION.md", env)
                self.assertNotIn("unrendered source", env)
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


    def test_all_production_env_settings_have_a_reference_row(self):
        """Ensure each release env entry is explained, including Traefik."""
        template = (ROOT / "deploy" / "production.env.example").read_text(encoding="utf-8")
        reference = (ROOT / "docs" / "CONFIGURATION.md").read_text(encoding="utf-8")
        keys = re.findall(r"(?m)^([A-Z][A-Z0-9_]*)=", template)
        proxy_keys = re.findall(r"(?m)^# (COMPOSE_FILE|TRAEFIK_[A-Z_]+)=", template)
        rows = re.findall(r"(?m)^[|] `([A-Z][A-Z0-9_]*)` [|]", reference)

        self.assertEqual(len(keys), len(set(keys)), "Duplicate env keys")
        self.assertEqual(len(proxy_keys), len(set(proxy_keys)), "Duplicate Traefik keys")
        self.assertEqual(len(rows), len(set(rows)), "Duplicate reference rows")
        self.assertEqual(set(keys) | set(proxy_keys), set(rows),
                         "Every env entry needs exactly one reference row")

        compose = (ROOT / "deploy" / "compose.production.yaml").read_text(encoding="utf-8")
        overlay = (ROOT / "deploy" / "compose.traefik.yaml").read_text(encoding="utf-8")
        # Docker Compose escapes service-side env references as $${...};
        # these are not .env interpolation variables.
        interpolated = re.sub(r"\$\$\{[A-Z][A-Z0-9_]*\}", "", compose + overlay)
        used = set(re.findall(r"\$\{([A-Z][A-Z0-9_]*)", interpolated))
        documented = (set(keys) | set(proxy_keys)) - {"COMPOSE_FILE"}
        self.assertEqual(
            documented, used,
            "Production Compose and .env must agree in BOTH directions",
        )


if __name__ == "__main__":
    unittest.main()
