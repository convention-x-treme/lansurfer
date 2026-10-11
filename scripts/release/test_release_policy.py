#!/usr/bin/env python3
"""Offline regression tests for release tag and alias safety rules."""

import json
import tempfile
import unittest
from pathlib import Path

from release_policy import Version, check_new, load_releases, parse_tag, plan_aliases


def release(tag: str, *, draft: bool = False, prerelease: bool = False) -> dict:
    return {"tag_name": tag, "draft": draft, "prerelease": prerelease}


class TagValidationTest(unittest.TestCase):
    def test_stable(self):
        self.assertEqual(parse_tag("v2.10.3"), (Version(2, 10, 3), False))

    def test_supported_prereleases(self):
        for suffix in ("alpha.1", "beta.0", "rc.12"):
            with self.subTest(suffix=suffix):
                self.assertEqual(parse_tag("v3.0.0-" + suffix), (Version(3, 0, 0), True))

    def test_reject_invalid_tags(self):
        invalid = [
            "v1.2", "1.2.3", "v01.2.3", "v1.02.3", "v1.2.03",
            "v1.2.3-rc.01", "v1.2.3-dev", "v1.2.3+metadata",
            "v1.2.3-rc.1+metadata", "v1.2.3;rm -rf /", "v1.2.3\n",
        ]
        for tag in invalid:
            with self.subTest(tag=repr(tag)), self.assertRaises(ValueError):
                parse_tag(tag)


class PreflightTest(unittest.TestCase):
    def test_fresh_tag_accepted(self):
        check_new("v1.3.0", [release("v1.2.0")])

    def test_reuse_of_published_version_rejected(self):
        with self.assertRaisesRegex(ValueError, "already has"):
            check_new("v1.2.0", [release("v1.2.0")])

    def test_reuse_of_draft_version_rejected(self):
        with self.assertRaisesRegex(ValueError, "already has"):
            check_new("v1.2.0", [release("v1.2.0", draft=True)])


class AliasPlanTest(unittest.TestCase):
    def test_first_pilot_release_has_no_aliases(self):
        self.assertEqual(
            plan_aliases("v0.1.0", []),
            {"tag": "v0.1.0", "aliases": [], "make_latest": False},
        )

    def test_pilot_patch_has_no_aliases_or_github_latest(self):
        self.assertEqual(
            plan_aliases("v0.1.1", [release("v0.1.0")]),
            {"tag": "v0.1.1", "aliases": [], "make_latest": False},
        )

    def test_pilot_release_candidate_has_no_aliases(self):
        self.assertEqual(
            plan_aliases("v0.1.0-rc.1", []),
            {"tag": "v0.1.0-rc.1", "aliases": [], "make_latest": False},
        )

    def test_first_v1_release_can_promote_latest_after_v0(self):
        self.assertEqual(
            plan_aliases("v1.0.0", [release("v0.9.5")]),
            {"tag": "v1.0.0", "aliases": ["v1.0", "v1", "latest"], "make_latest": True},
        )

    def test_first_stable_release(self):
        result = plan_aliases("v1.0.0", [])
        self.assertEqual(result["aliases"], ["v1.0", "v1", "latest"])
        self.assertTrue(result["make_latest"])

    def test_new_major_promotes_all(self):
        result = plan_aliases("v2.0.0", [release("v1.9.4")])
        self.assertEqual(result["aliases"], ["v2.0", "v2", "latest"])

    def test_prerelease_never_moves_aliases(self):
        self.assertEqual(
            plan_aliases("v2.0.0-rc.1", [release("v1.9.4")]),
            {"tag": "v2.0.0-rc.1", "aliases": [], "make_latest": False},
        )

    def test_older_major_backport_cannot_regress_latest(self):
        result = plan_aliases("v1.4.1", [release("v1.4.0"), release("v2.0.0")])
        self.assertEqual(result["aliases"], ["v1.4", "v1"])
        self.assertFalse(result["make_latest"])

    def test_old_minor_backport_cannot_regress_major(self):
        result = plan_aliases(
            "v1.4.3", [release("v1.4.2"), release("v1.5.0"), release("v2.0.0")]
        )
        self.assertEqual(result["aliases"], ["v1.4"])

    def test_out_of_order_patch_never_regresses(self):
        result = plan_aliases("v1.4.3", [release("v1.4.10")])
        self.assertEqual(result["aliases"], [])

    def test_new_patch_over_existing_patch(self):
        result = plan_aliases("v1.4.10", [release("v1.4.9")])
        self.assertEqual(result["aliases"], ["v1.4", "v1", "latest"])

    def test_newer_draft_blocks_latest_after_partial_publish(self):
        # A v2 draft may already have moved GHCR latest before a failure.
        result = plan_aliases(
            "v1.5.0", [release("v2.0.0", draft=True),
                       release("v2.0.0-rc.1", prerelease=True)]
        )
        self.assertEqual(result["aliases"], ["v1.5", "v1"])
        self.assertFalse(result["make_latest"])

    def test_newer_draft_blocks_major_and_minor_downgrades(self):
        result = plan_aliases(
            "v1.4.1", [release("v1.4.2", draft=True)]
        )
        self.assertEqual(result["aliases"], [])
        self.assertFalse(result["make_latest"])

    def test_current_draft_does_not_block_its_own_version_aliases(self):
        result = plan_aliases("v1.0.0", [release("v1.0.0", draft=True)])
        self.assertEqual(result["aliases"], ["v1.0", "v1", "latest"])

    def test_prerelease_does_not_block_new_stable_aliases(self):
        result = plan_aliases("v1.0.0", [release("v2.0.0-rc.1", prerelease=True)])
        self.assertEqual(result["aliases"], ["v1.0", "v1", "latest"])

    def test_ignore_unrelated_tags(self):
        result = plan_aliases("v1.5.0", [release("legacy-2026"), release("v1.5.0+meta")])
        self.assertEqual(result["aliases"], ["v1.5", "v1", "latest"])

    def test_gh_paginated_slurp_output(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "releases.json"
            path.write_text(
                json.dumps([[release("v1.4.0")], [release("v2.0.0")]]),
                encoding="utf-8",
            )
            result = plan_aliases("v1.4.1", load_releases(str(path)))
            self.assertEqual(result["aliases"], ["v1.4", "v1"])

    def test_invalid_github_release_json(self):
        with tempfile.TemporaryDirectory() as folder:
            path = Path(folder) / "releases.json"
            path.write_text('{"bad":"shape"}', encoding="utf-8")
            with self.assertRaises(ValueError):
                load_releases(str(path))


if __name__ == "__main__":
    unittest.main()
