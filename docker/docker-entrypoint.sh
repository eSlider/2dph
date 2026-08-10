#!/usr/bin/env bash
# docker-entrypoint.sh - run 2dph bin tools inside the container.
#
#   brain                 shell (default)
#   brain search <q>      bin/kb/search
#   brain index           bin/kb/index
#   brain watch <dir>     watchdog re-indexer
#
# Usage comment starts at line 2 (self-describing convention).
set -euo pipefail

CMD="${1:-shell}"
shift || true

case "$CMD" in
  shell) exec bash ;;
  search) exec python3 /app/bin/kb/search "$@" ;;
  index) exec python3 /app/bin/kb/index "$@" ;;
  watch) exec python3 /app/.dockerbin/kb-watch "$@" ;;
  serve) exec python3 /app/.dockerbin/serve "$@" ;;
  *) echo "unknown command: $CMD" >&2; exit 2 ;;
esac