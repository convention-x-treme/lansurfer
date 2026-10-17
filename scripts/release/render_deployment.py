#!/usr/bin/env python3
"""Produce release-matched, immutable Docker Compose download assets.

The source env file intentionally contains an unusable image marker. The
privileged tag publisher injects the verified multi-platform index digest
and uploads these files to the corresponding GitHub Release before publishing.
"""

import hashlib
import re
import shutil
import sys
from pathlib import Path

from release_policy import parse_tag

MARKER = "__LANSURFER_RELEASE_IMAGE__"
HEADER_MARKER = "# __LANSURFER_DEPLOYMENT_RELEASE_HEADER__"
IMAGE_REPO = "ghcr.io/convention-x-treme/lansurfer"
ASSETS = ("compose.yaml", "compose.traefik.yaml", "production.env.example")


def render_deployment(tag: str, digest: str, output: Path, *, root: Path | None = None) -> None:
    parse_tag(tag)
    if not re.fullmatch(r"sha256:[a-f0-9]{64}", digest):
        raise ValueError("Invalid multi-platform image digest")
    if root is None:
        root = Path(__file__).resolve().parents[2]
    deploy = root / "deploy"
    template = (deploy / "production.env.example").read_text(encoding="utf-8")
    if template.count(f"LANSURFER_IMAGE={MARKER}\n") != 1 or template.count(MARKER) != 1:
        raise ValueError("Exactly one release image marker is required in source env template")
    if template.count(HEADER_MARKER + "\n") != 1:
        raise ValueError("Exactly one release header marker is required in source env template")
    image = f"{IMAGE_REPO}:{tag}@{digest}"
    operator_header = (
        f"# LANsurfer {tag} production configuration.\n"
        "# Download compose.yaml and this file from the SAME GitHub Release.\n"
        "# Copy to .env, fill in credentials, and restrict permissions: chmod 600 .env\n"
        f"# Complete configuration reference: "
        f"https://github.com/convention-x-treme/lansurfer/blob/{tag}/docs/CONFIGURATION.md"
    )
    rendered = template.replace(HEADER_MARKER, operator_header).replace(MARKER, image)
    if MARKER in rendered or HEADER_MARKER in rendered:
        raise ValueError("Unresolved release template marker")
    output.mkdir(parents=True, exist_ok=False)
    shutil.copyfile(deploy / "compose.production.yaml", output / "compose.yaml")
    shutil.copyfile(deploy / "compose.traefik.yaml", output / "compose.traefik.yaml")
    (output / "production.env.example").write_text(rendered, encoding="utf-8")
    lines = []
    for filename in ASSETS:
        blob = (output / filename).read_bytes()
        if not blob:
            raise ValueError(f"Empty deployment asset: {filename}")
        lines.append(f"{hashlib.sha256(blob).hexdigest()}  {filename}\n")
    (output / "deployment-checksums.txt").write_text("".join(lines), encoding="ascii")


def main() -> int:
    if len(sys.argv) != 4:
        print("usage: render_deployment.py TAG DIGEST OUTPUT_DIRECTORY", file=sys.stderr)
        return 2
    try:
        render_deployment(sys.argv[1], sys.argv[2], Path(sys.argv[3]))
    except (ValueError, OSError) as exc:
        print(f"Deployment asset generation failed: {exc}", file=sys.stderr)
        return 1
    print(f"Generated release-matched Docker Compose assets for {sys.argv[1]}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
