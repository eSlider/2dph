#!/bin/sh
# liteparse container entrypoint: keep the parse engine warm and the
# container alive as a long-lived service (docker compose start-as-service).
#
# The official image ships LibreOffice + tesseract + pdfium, but pdfium/tess
# libs load lazily per process. The warmup pass forces the libs + OS page cache
# so a later `docker compose exec liteparse lit parse ...` pays ~40ms instead of
# ~0.7s (docker run) / cold ~2.5s (OCR first hit).
#
# Layout (see compose.yaml service `liteparse`):
#   /s   — samples volume (read-only), used by the A/B tool and warmup
#   /v   — host var/ mount (struct-data + research out), the ETL sink
#   /data/work — scratch for per-doc JSON before struct-data write

set -eu

PDFIUM_LIB_PATH="${PDFIUM_LIB_PATH:-/usr/local/lib/pdfium-rs/chromium_7897/pdfium-linux-x64/lib}"
export PDFIUM_LIB_PATH
export LD_LIBRARY_PATH="${PDFIUM_LIB_PATH}"

mkdir -p /data/work

echo "[liteparse] warmup: load pdfium + tesseract into page cache"
lit --version >/dev/null 2>&1 || true
if [ -f /s/invoice-text.pdf ]; then
  lit parse /s/invoice-text.pdf --format markdown --no-ocr -o /data/work/warmup.md >/dev/null 2>&1 || true
fi

echo "[liteparse] ready; holding container open (start-as-service)"
exec tail -f /dev/null