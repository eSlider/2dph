#!/usr/bin/env bash
#
# install.sh — symlinks the shared githooks (pre-push, pre-commit) into .git/hooks
# for this repository. Safe to run repeatedly.
#
# Usage:
#   ./scripts/githooks/install.sh

set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
SRC="$ROOT/scripts/githooks"
HOOKS="$ROOT/.git/hooks"

mkdir -p "$HOOKS"
chmod +x "$SRC"/secret-scan.sh "$SRC"/pre-push "$SRC"/pre-commit

for h in pre-push pre-commit; do
  if [[ -e "$HOOKS/$h" ]] && [[ ! -L "$HOOKS/$h" ]]; then
    echo "error: $HOOKS/$h already exists and is not a symlink; remove it first" >&2
    exit 1
  fi
  ln -sfn "$SRC/$h" "$HOOKS/$h"
  echo "installed $h -> $SRC/$h"
done
echo "githooks installed for $(basename "$ROOT")"
