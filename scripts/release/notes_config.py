#!/usr/bin/env python3
"""Prepare an OSS GoReleaser config with cumulative stable release notes.

Prereleases keep GoReleaser's normal previous-tag selection. Stable releases
ignore reachable prerelease tags, so the first stable version includes the
entire history and later stable versions compare to the last reachable stable
version. The generated config is local-only and does not change Git refs.
"""

import json
import re
import subprocess
import sys
from pathlib import Path

from release_policy import Version, parse_tag


def git(*args: str) -> str:
    return subprocess.check_output(["git", *args], text=True).strip()


def plan_notes(tag: str, reachable_tags: list[str]) -> tuple[list[str], str | None]:
    current, is_prerelease = parse_tag(tag)
    if is_prerelease:
        return [], None

    # Selection is based on reachability from the current revision, not the
    # newest repository-wide tag or release date. This also works for v1
    # backports when main has moved on to v2.
    ignored = []
    stable = []
    for candidate_tag in reachable_tags:
        if candidate_tag == tag:
            continue
        try:
            candidate, candidate_is_prerelease = parse_tag(candidate_tag)
        except ValueError:
            continue  # Non-release tags are not part of our SemVer policy.
        if candidate_is_prerelease:
            if candidate <= current:
                ignored.append(candidate_tag)
        elif candidate >= current:
            raise ValueError(
                f"Unexpected reachable stable tag {candidate_tag} at or after "
                f"{tag}; refuse ambiguous changelog range"
            )
        else:
            stable.append((candidate, candidate_tag))

    previous = max(stable)[1] if stable else None
    return sorted(ignored), previous


def write_config(tag: str, base_path: Path, output_path: Path) -> str | None:
    # Fail closed when the caller is not building the exact tagged commit.
    tagged_commit = git("rev-parse", "--verify", f"refs/tags/{tag}^{{commit}}")
    if tagged_commit != git("rev-parse", "HEAD"):
        raise ValueError(f"Current HEAD differs from version tag {tag}")

    reachable_tags = git("tag", "--merged", "HEAD").splitlines()
    ignored, previous = plan_notes(tag, reachable_tags)

    source = base_path.read_text(encoding="utf-8")
    if ignored:
        if re.search(r"^git\s*:", source, re.MULTILINE):
            raise ValueError("Base GoReleaser config already contains a git section")
        # Exact tag names are supported in GoReleaser OSS; globs and smart
        # SemVer tag sorting require Pro, so do not use those features.
        source += "\n# Generated only for this stable build; not committed or pushed.\n"
        source += "git:\n  ignore_tags:\n"
        source += "".join(f"    - {json.dumps(name)}\n" for name in ignored)

    output_path.write_text(source, encoding="utf-8")
    return previous


def main() -> int:
    if len(sys.argv) != 4:
        print("usage: notes_config.py TAG BASE_CONFIG OUTPUT_CONFIG", file=sys.stderr)
        return 2
    try:
        previous = write_config(sys.argv[1], Path(sys.argv[2]), Path(sys.argv[3]))
    except (ValueError, OSError, subprocess.CalledProcessError) as exc:
        print(f"Release notes selection rejected: {exc}", file=sys.stderr)
        return 1
    print(previous or "")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
