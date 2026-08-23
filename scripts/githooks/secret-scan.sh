#!/usr/bin/env bash
#
# secret-scan.sh — shared secret scanner used by git hooks (pre-push, pre-commit)
# and by SE agents before any push.
#
# Scans ONLY the diff of new commits (or staged changes) with gitleaks, never the
# full history. Fails (exit non-zero) on any finding, so a leaking push is blocked.
#
# Uses the gitleaks Docker image (gitleaks/gitleaks) if docker is available,
# otherwise a locally installed `gitleaks` binary. No secret VALUES are ever
# printed: findings are emitted redacted.
#
# Usage:
#   secret-scan.sh <range>            scan a git log range, e.g. origin/release/v1..HEAD
#   secret-scan.sh --staged           scan staged (index) changes
#   secret-scan.sh --all              scan full history (warning: not for normal use)
#
# Exit codes: 0 = clean, 1 = leaks found (caller should abort), 2 = scan failed.

set -euo pipefail

GITLEAKS_IMAGE="zricethezav/gitleaks:latest"

run_gitleaks() {
  # $@ = gitleaks args; runs in current dir (a git repo).
  if command -v gitleaks >/dev/null 2>&1; then
    gitleaks "$@"
  elif command -v docker >/dev/null 2>&1 && docker info >/dev/null 2>&1; then
    docker run --rm -v "$PWD:/repo" -w /repo "$GITLEAKS_IMAGE" "$@"
  else
    echo "error: secret-scan: neither 'gitleaks' binary nor docker image available" >&2
    exit 2
  fi
}

mode="${1:---range}"
shift || true

case "$mode" in
  --range)
    range="${1:?usage: secret-scan.sh <range>}"
    run_gitleaks detect --source "$PWD" --no-banner --redact --log-opts="$range" >&2
    ;;
  --staged)
    # Scan only staged (index) content: pipe `git diff --cached` through gitleaks --pipe.
    git diff --cached --binary | run_gitleaks detect --pipe --no-banner --redact >&2
    ;;
  --all)
    run_gitleaks detect --source "$PWD" --no-banner --redact >&2
    ;;
  *)
    echo "error: secret-scan: unknown mode '$mode'" >&2
    exit 2
    ;;
esac
