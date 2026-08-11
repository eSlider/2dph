import sys
import unittest
from pathlib import Path

sys.path.insert(0, str(Path(__file__).resolve().parent.parent))

from semver import bump_type, bump_version  # noqa: E402


class BumpTypeTest(unittest.TestCase):
    def test_empty_is_none(self):
        self.assertEqual(bump_type([]), "none")

    def test_feat_is_minor(self):
        self.assertEqual(bump_type(["feat: add search"]), "minor")

    def test_fix_is_patch(self):
        self.assertEqual(bump_type(["fix: typo"]), "patch")

    def test_chore_and_docs_still_release(self):
        self.assertEqual(bump_type(["docs: readme"]), "patch")
        self.assertEqual(bump_type(["ci: green"]), "patch")

    def test_breaking_marker_is_major(self):
        self.assertEqual(bump_type(["feat!: break api"]), "major")
        self.assertEqual(bump_type(["fix: x\n\nBREAKING CHANGE: y"]), "major")

    def test_mixed_commits_choose_highest(self):
        self.assertEqual(bump_type(["fix: a", "feat: b"]), "minor")


class BumpVersionTest(unittest.TestCase):
    def test_patch(self):
        self.assertEqual(bump_version("v0.1.0", "patch"), "v0.1.1")

    def test_minor(self):
        self.assertEqual(bump_version("v0.1.0", "minor"), "v0.2.0")

    def test_major(self):
        self.assertEqual(bump_version("v0.1.0", "major"), "v1.0.0")

    def test_initial_when_no_tag(self):
        self.assertEqual(bump_version(None, "patch"), "v0.0.1")

    def test_none_returns_none(self):
        self.assertIsNone(bump_version("v0.1.0", "none"))


if __name__ == "__main__":
    unittest.main()