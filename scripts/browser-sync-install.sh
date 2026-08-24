#!/usr/bin/env bash
# scripts/browser-sync-install.sh - install the browser-sync systemd timer.
#
# Substitutes the real repo root into the unit templates (committed units use
# the @REPO_ROOT@ placeholder to avoid machine-specific paths), installs them
# under /etc/systemd/system and enables the 6-hourly timer.
#
#   sudo scripts/browser-sync-install.sh
set -euo pipefail

if [[ "${EUID:-$(id -u)}" -ne 0 ]]; then
  echo "browser-sync-install: run as root (sudo) to write /etc/systemd/system" >&2
  exit 1
fi

ROOT="$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)"

for unit in browser-sync.service browser-sync.timer; do
  sed "s|@REPO_ROOT@|$ROOT|g" "$ROOT/scripts/$unit" > "/etc/systemd/system/$unit"
done

systemctl daemon-reload
systemctl enable --now browser-sync.timer
systemctl status --no-pager browser-sync.timer
