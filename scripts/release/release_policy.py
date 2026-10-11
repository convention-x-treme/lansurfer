#!/usr/bin/env python3
"""SemVer release admission and monotonic GHCR alias planning (no API writes)."""

import argparse
import json
import re
import sys
from dataclasses import dataclass
from pathlib import Path

# Deliberately narrower than unrestricted SemVer: Docker tags cannot contain '+',
# and LANsurfer only supports numbered alpha/beta/rc prereleases.
TAG_RE = re.compile(
    r"^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)"
    r"(?:-(alpha|beta|rc)\.(0|[1-9][0-9]*))?$"
)


@dataclass(frozen=True, order=True)
class Version:
    major: int
    minor: int
    patch: int


def parse_tag(tag: str) -> tuple[Version, bool]:
    match = TAG_RE.fullmatch(tag)
    if not match:
        raise ValueError(
            "Expected vMAJOR.MINOR.PATCH or "
            "vMAJOR.MINOR.PATCH-(alpha|beta|rc).N "
            "(no leading zeros or build metadata)"
        )
    version = Version(*(int(x) for x in match.group(1, 2, 3)))
    return version, match.group(4) is not None


def load_releases(path: str) -> list[dict]:
    data = json.loads(Path(path).read_text(encoding="utf-8"))
    if not isinstance(data, list):
        raise ValueError("GitHub release response must be an array")

    # 'gh api --paginate --slurp' wraps multiple GitHub release-list pages in
    # an outer array; tests also accept a single flat array.
    if all(isinstance(item, list) for item in data):
        flattened = [release for page in data for release in page]
    else:
        flattened = data
    if not all(isinstance(item, dict) for item in flattened):
        raise ValueError("GitHub release response contains non-object entries")
    return flattened


def check_new(tag: str, releases: list[dict]) -> None:
    parse_tag(tag)
    if any(release.get("tag_name") == tag for release in releases):
        raise ValueError(
            f"{tag} already has a GitHub Release (including a draft); "
            "never overwrite or retry an immutable version automatically"
        )


def plan_aliases(tag: str, releases: list[dict]) -> dict:
    candidate, prerelease = parse_tag(tag)
    # 0.x is LANsurfer's internal/pilot line: publish only the exact tag.
    # No stable GHCR aliases or GitHub's "latest" marker until 1.0.
    if prerelease or candidate.major == 0:
        return {"tag": tag, "aliases": [], "make_latest": False}

    existing_stable = []
    for release in releases:
        # Include stable-version drafts conservatively: an interrupted newer
        # release may already have promoted GHCR aliases before publication.
        # Blocking old versions here avoids overwriting those aliases.
        if release.get("prerelease"):
            continue
        old_tag = release.get("tag_name")
        if not isinstance(old_tag, str):
            continue
        try:
            version, is_prerelease = parse_tag(old_tag)
        except ValueError:
            continue
        if not is_prerelease:
            existing_stable.append(version)

    aliases = []
    for alias, same_line in (
        (f"v{candidate.major}.{candidate.minor}", lambda v: v.major == candidate.major and v.minor == candidate.minor),
        (f"v{candidate.major}", lambda v: v.major == candidate.major),
        ("latest", lambda v: True),
    ):
        if not any(same_line(v) and v > candidate for v in existing_stable):
            aliases.append(alias)

    return {"tag": tag, "aliases": aliases, "make_latest": "latest" in aliases}


def main() -> int:
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("command", choices=("validate", "check-new", "plan"))
    parser.add_argument("tag")
    parser.add_argument("releases_json", nargs="?")
    args = parser.parse_args()

    try:
        parse_tag(args.tag)
        if args.command == "validate":
            print(f"Valid release tag: {args.tag}")
            return 0
        if not args.releases_json:
            parser.error("check-new and plan require releases_json")
        releases = load_releases(args.releases_json)
        if args.command == "check-new":
            check_new(args.tag, releases)
            print(f"No existing release for {args.tag}")
        else:
            print(json.dumps(plan_aliases(args.tag, releases), separators=(",", ":")))
    except (ValueError, OSError, json.JSONDecodeError) as exc:
        print(f"Release policy rejected: {exc}", file=sys.stderr)
        return 1

    return 0


if __name__ == "__main__":
    raise SystemExit(main())
