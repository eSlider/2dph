# Chat Import Pipeline

Plan and implementation details: https://git.produktor.io/eSlider/brain-chats-import/issues/1

Progress tracked in that issue. This file kept as a local reference for agent sessions.

## Key env vars (set in shell, never committed)

TELEGRAM_MCP_DIR           telegram-mcp directory
TELEGRAM_API_ID            from telegram.env (not committed)
TELEGRAM_API_HASH          from telegram.env (not committed)
TELEGRAM_PHONE             from telegram.env (not committed)
TELEGRAM_SESSION_STRING    from telegram-mcp/.env (not committed)
ONLYOFFICE_URL             from .env (not committed)
ONLYOFFICE_USER            from .env (not committed)
ONLYOFFICE_PASS            from .env (not committed)
OO_CLI                     path to oo binary (default: $HOME/go/bin/oo)

## Quick reference

./bin/chat sync telegram --limit 100
./bin/chat import
./bin/chat index        # delegates to bin/kb/index
./bin/chat facts
./bin/chat apply --dry-run
