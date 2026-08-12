# Chat Import Pipeline

Plan: https://git.produktor.io/eSlider/brain-chats-import/issues/1

## Env vars (set in shell, never committed)

```
TELEGRAM_MCP_DIR
TELEGRAM_API_ID / TELEGRAM_API_HASH / TELEGRAM_PHONE
TELEGRAM_SESSION_STRING
ONLYOFFICE_URL / ONLYOFFICE_USER / ONLYOFFICE_PASS
OO_CLI                     (default: $HOME/go/bin/oo)
```

## Quick reference

```
./bin/chat sync telegram --limit 100
./bin/chat import
./bin/chat index
./bin/chat facts
./bin/chat apply --dry-run
```
