"""semver logic shared by bin/ci/semver and its tests. No git IO here."""

from __future__ import annotations

BREAKING_MARKERS = ("BREAKING CHANGE", "breaking-change")
BUMP_PATCH_TYPES = ("fix", "perf", "refactor", "build", "ci", "docs", "chore", "test", "style", "revert")
BUMP_MINOR_TYPE = "feat"


def bump_type(subjects: list[str]) -> str:
    if not subjects:
        return "none"
    for subject in subjects:
        text = subject.lower()
        if any(m.lower() in text for m in BREAKING_MARKERS):
            return "major"
        if "!" in subject.split(":")[0]:
            return "major"
    for subject in subjects:
        if subject.startswith(f"{BUMP_MINOR_TYPE}:"):
            return "minor"
    return "patch"


def bump_version(current: str | None, bump: str) -> str | None:
    if bump == "none":
        return None
    major, minor, patch = [int(n) for n in (current or "0.0.0").lstrip("v").split(".")]
    if bump == "major":
        return f"v{major + 1}.0.0"
    if bump == "minor":
        return f"v{major}.{minor + 1}.0"
    return f"v{major}.{minor}.{patch + 1}"