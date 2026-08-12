# Chat Import Pipeline

<<<<<<< Updated upstream
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

<<<<<<< HEAD
./bin/chat sync telegram --limit 100
./bin/chat import
./bin/chat index        # delegates to bin/kb/index
./bin/chat facts
./bin/chat apply --dry-run
||||||| parent of 313599b (bin/chats: Phase 1 MVP — Telegram sync/import/index/facts/apply)
---

## 1. Находки / Discoveries

### Доступные платформы и данные

| Platform | Status | Credentials | Go Library |
|----------|--------|-------------|------------|
| **Telegram** | ✅ Active MCP server, session string exists | API ID: `30382285`, API Hash: `fd8a8c1e987bb908b457f374b721c062`, Session string in `/mnt/8TB/projects/eslider/mcp-servers/telegram.env` and `/mnt/8TB/projects/eslider/mcp-servers/telegram-mcp/.env`. Phone: `+34643861471` | [`iyear/tdl`](https://github.com/iyear/tdl) (7.5k★, Go, gotd/td, экспорт в JSON) |
| **WhatsApp** | ✅ MCP server exists + Go bridge. **No `messages.db` yet** (empty). Needs QR re-auth + sync first | MCP: `/mnt/8TB/projects/eslider/mcp-servers/whatsapp-mcp/`. Go bridge uses `tulir/whatsmeow` | [`tulir/whatsmeow`](https://github.com/tulir/whatsmeow) (7k★, Go, multi-device API) |
| **LinkedIn** | ✅ Connected via `mcp-server-linkedin`. Full browser profile + cookies | Browser profile: `~/.linkedin-mcp/profile/`. Active cookies: `~/.linkedin-mcp/storage-state.json`. Cookie backup: `~/.linkedin-mcp/cookies.json.bak-agent`. Script cookies: `/mnt/8TB/projects/eslider/cv/bin/creds/linkedin.cookies.json`. JobHunt session: `/mnt/8TB/projects/eslider/JobHunt/sessions/linkedin.json`. Env: `/mnt/8TB/projects/eslider/cv/.env` (`LINKEDIN_AUTH_TOKEN`). | [`swiftlysingh/lnk`](https://github.com/swiftlysingh/lnk) (Go CLI, Voyager API) |
| **Gmail** | ✅ Already working pipeline (`bin/mail/sync.go` + `bin/mail/import`) | OAuth at `~/.gmail-mcp/` | Already in 2dph |
| **Twitter/X.com** | ⚠️ Password only, no MCP, no session. Not needed now. | `/mnt/8TB/projects/eslider/scratches/Accounts/twitter-pass.txt` | [`benoitpetit/xsh`](https://github.com/benoitpetit/xsh) (Go CLI, cookie auth) |
| **Google Calendar** | ❌ Not connected yet. Gmail OAuth could extend | None | Нужен Google Calendar API |

### Telegram session (проверен, активен)

- API credentials in: `/mnt/8TB/projects/eslider/mcp-servers/telegram.env`
  - `TELEGRAM_API_ID=30382285`
  - `TELEGRAM_API_HASH=fd8a8c1e987bb908b457f374b721c062`
  - `TELEGRAM_PHONE=+34643861471`
  - `TELEGRAM_SESSION_STRING` — длинная строка, active session
- MCP server launcher: `/mnt/8TB/projects/eslider/mcp-servers/run-telegram-mcp.sh`
- Session generator: `/mnt/8TB/projects/eslider/mcp-servers/telegram-mcp/session_string_generator.py`
- Regenerator: `/mnt/8TB/projects/eslider/mcp-servers/regen-telegram-session.py`

### LinkedIn authentication (работает)

- Запускается через Cursor MCP: `uvx mcp-server-linkedin@latest --user-data-dir /home/ano/.linkedin-mcp/profile --no-auto-import`
- Cookie файлы:
  - `~/.linkedin-mcp/storage-state.json` — Playwright storage state (активные cookies)
  - `~/.linkedin-mcp/cookies.json.bak-agent` — резерв
  - `~/.linkedin-mcp/cookies.json.bak-webtop-20260728T143535Z` — резерв
  - `/mnt/8TB/projects/eslider/cv/bin/creds/linkedin.cookies.json` — для скриптов
  - `/mnt/8TB/projects/eslider/JobHunt/sessions/linkedin.json` — JobHunt
- `li_at` token: `AQEDASsyC9gF8hS2AAABnC2A_vIAAAGcdZZfTk4Ahfp9wURXwTpCAAtRIw_htVldds5UN9JDr6L_hr3lN5CcPxnKDvy-CxQo0apf7K4pmftQzQYHdqpKNNzlNakcvEcOOalpqRxPhlTfIt8jxqevkZcr`
- Для Go подхода: можно читать cookie из `storage-state.json` и использовать LinkedIn Voyager API напрямую

### WhatsApp bridge (не синхронизирован)

- Go bridge at: `/mnt/8TB/projects/eslider/mcp-servers/whatsapp-mcp/whatsapp-bridge/`
- Использует `tulir/whatsmeow` Go library (уже в `go.mod`)
- SQLite store: `whatsapp-bridge/store/messages.db` — **не существует** (0 байт), нужна первая авторизация по QR
- Launcher: `/mnt/8TB/projects/eslider/mcp-servers/run-whatsapp-bridge-lharries.sh`
- QR images готовились: `/mnt/8TB/projects/produktor/work/data/export/whatsapp-qr.png`
- Пароль: `Oomtr78k39` (из `.env` OnlyOffice, он же для многих сервисов)

### OnlyOffice доступ

- `.env` в корне 2dph: `ONLYOFFICE_URL=https://office.produktor.io`, `ONLYOFFICE_USER=eslider@gmail.com`, `ONLYOFFICE_PASS=Oomtr78k39`
- `oo` CLI установлен: `/home/ano/go/bin/oo` (go-onlyoffice)
- go-onlyoffice repo: `/mnt/8TB/projects/eslider/go-onlyoffice/` (main, public)
- oo-workspace (каталог, private): `/mnt/8TB/projects/eslider/oo-workspace/`

### Gitea

- Instance: `https://git.produktor.io` (через `gitea-api` wrapper)
- Credentials: `~/.config/gitea/produktor.env`
- User: `eSlider`, admin
- Repos: 30+ приватных репозиториев
- **`eSlider/2dph` не существует на Gitea** — проект только на GitHub
- Для задачи: создать issue в существующем репо (например `eSlider/JobHunt`) или создать новый репо

### Rambox

Не используется. IndexedDB база не существует.
||||||| Stash base
Created: 2026-08-12
Author: Andriy Oblivantsev
Purpose: One-shot handoff for a new agent session to implement `bin/chats`
=======
Plan and implementation details: https://git.produktor.io/eSlider/brain-chats-import/issues/1
>>>>>>> Stashed changes

Progress tracked in that issue. This file kept as a local reference for agent sessions.

<<<<<<< Updated upstream
## 2. Design Decisions

### One binary: `bin/chats`

```go
// Source interface — каждая платформа реализует
type Source interface {
    Name() string                // "telegram", "whatsapp", "linkedin"
    Sync(ctx context.Context, out string) error
}

// CLI subcommands
chats sync telegram    [--limit N] [--since DATE]
chats sync whatsapp    [--qr] [--limit N]
chats sync linkedin    [--limit N]
chats import           // JSONL → markdown (all sources)
chats index            // rebuild var/kb.lbug
chats facts            // extract + cross-check
chats apply            // push to OO CRM [--dry-run]
```

### Directory layout (полные имена платформ)

```
var/chats/
  telegram/<chat_id>/messages.jsonl
  whatsapp/<jid>/messages.jsonl
  linkedin/<conversation_id>/messages.jsonl
  md/<source>/<chat_name>/message.md
```

### JSONL format (одна строка = одно сообщение)

```json
{"id":"123","ts":"2026-01-15T10:30:00Z","from":"me","text":"Hello!","media":null,"platform":"telegram"}
```

### MD format (YAML frontmatter)

```markdown
---
id: tg_12345
platform: telegram
chat_id: "-100123456"
chat_name: "John Doe"
participants: ["me", "John Doe"]
message_count: 1500
type: personal
---

# Чат с John Doe

**2026-01-15 10:30** — me: Hello!
```

### Фильтр "личный чат"

- ≤3 участников
- Боты не считаются (Telegram: `user.is_bot == false`)
- Не канал, не публичная группа
- Telegram: `dialog.Type == "user"` или супергруппа с `participants_count <= 3` (исключая ботов)
- WhatsApp: `@s.whatsapp.net` (не `@g.us`)
- LinkedIn: 1:1 conversation

### Очередность реализации

1. **Telegram** — самый простой (session string + API ID/Hash активны)
2. **Import** — конвертер JSONL → MD
3. **Index** — интеграция с brain
4. **WhatsApp** — после первого QR
5. **Facts** — извлечение контактных данных
6. **LinkedIn** — после проверки cookie
7. **Apply** — запись в OO CRM

### Тестирование (TDD, system tests only)

- Никаких unit-тестов, никаких моков
- Тесты используют реальные данные (слепки)
- Workflow use-case как тест:
  1. Написать тест с реальным сценарием (sync → import → index → search)
  2. Проверить что файлы созданы
  3. Проверить что поиск возвращает ожидаемые результаты
- A/B тест: сравнить два последовательных sync на идентичность
- Integration: тест через `oo` CLI с `--dry-run`

### Cross-check (detective method)

Перед записью в OO CRM — минимум 2 независимых источника:
- S1: Чат (текст сообщения, автор, дата)
- S2: OO CRM (существующий контакт/компания/deal) через `oo persons list` / `oo persons get`
- S3 (опционально): Corpus SoT (`knowledge-mesh-seed.yaml`) или web-search
- Факты с ≥2 источниками → `root=facts` в brain + `approve=true` для CRM
- Факты с 1 источником → предложение на review (catalog-паттерн)

### Apply в OO CRM

Через `oo` CLI:
- `oo persons update <id> --about ... --job-title ...`
- `oo contacts info-add <id> --type Phone|LinkedIn --value ...`
- `oo opportunities create --contact-id ... --title ... --stage ...`
- `oo persons update <id> --company-id ...` (ассоциация)
- `oo projects contacts <project-id>` (связь с проектом)

---

## 3. Implementation Plan

### Phase 1: Telegram sync + import (MVP)

1. Create `bin/chats/` directory structure (nested Go module like `bin/kbsearch/`)
2. Implement `Source` interface
3. Implement `TelegramSource` using `gotd/td` (import from `iyear/tdl` or direct)
4. Write JSONL output to `var/chats/telegram/<id>/messages.jsonl`
5. Implement `chats import` — reads all JSONL, writes MD
6. Write system test: real sync → import → verify output

### Phase 2: Brain indexing

1. Implement `chats index` — rebuild `var/kb.lbug` with corpus + chats
2. Write system test: sync → import → index → `bin/kb/search "query"` → verify recall

### Phase 3: WhatsApp

1. Implement `WhatsAppSource` using `tulir/whatsmeow`
2. First run requires QR auth (save session for later reuse)
3. Write sync output to `var/chats/whatsapp/<jid>/messages.jsonl`

### Phase 4: Facts extraction + cross-check

1. Implement `chats facts`:
   - Phone regex: `\+?\d{7,15}`
   - LinkedIn URL regex: `linkedin\.com/in/[\w-]+`
   - Cross-check: match extracted values against OO CRM API
2. Write facts to brain as `root=facts`

### Phase 5: LinkedIn + Apply

1. Implement `LinkedInSource` using Voyager API + cookies from `storage-state.json`
2. Implement `chats apply`:
   - Build YAML catalog (as `oo-workspace` does)
   - Run `oo catalog match` → review → `oo catalog apply`

---

## 4. Existing code references

| Code | Path | Purpose |
|------|------|---------|
| Mail sync | `bin/mail/sync.go` + `bin/mail/sync/` | Pattern for chat sync (Go subprocess pattern) |
| Mail import | `bin/mail/import` | Pattern for JSONL → MD conversion |
| Mail index | `bin/mail/index_mail` | Pattern for brain rebuild with new corpus |
| kbsearch | `bin/kbsearch/` | Go module pattern (nested, `system_ladybug` tag) |
| kb/index | `bin/kb/index` | Python brain index (upsert_leaf, init_schema) |
| kblib | `bin/tools/kblib.py` | Python brain core (connect, upsert_leaf, hybrid_search) |
| facts/crm | `bin/facts/crm` | Cross-check CRM facts pattern |
| facts/extract | `bin/facts/extract` | 2-source fact extraction pattern |
| oo-workspace catalog | `/mnt/8TB/projects/eslider/oo-workspace/catalog/` | Catalog scan/match/apply pattern (VCF, Thunderbird, projects) |
| WhatsApp bridge | `/mnt/8TB/projects/eslider/mcp-servers/whatsapp-mcp/whatsapp-bridge/` | Existing `whatsmeow` Go bridge |
| go-onlyoffice | `/mnt/8TB/projects/eslider/go-onlyoffice/` | OO CRM API library |
| oo CLI | `/home/ano/go/bin/oo` | Built go-onlyoffice CLI |

---

## 5. Critical files

| File | Content |
|------|---------|
| `~/.config/gitea/produktor.env` | Gitea URL + token |
| `/mnt/8TB/projects/eslider/mcp-servers/telegram.env` | Telegram API ID/Hash + phone + session |
| `/mnt/8TB/projects/eslider/mcp-servers/telegram-mcp/.env` | Telegram session string |
| `/mnt/8TB/projects/ai/2dph/.env` | ONLYOFFICE URL/USER/PASS |
| `/mnt/8TB/projects/eslider/cv/.env` | LinkedIn auth token |
| `~/.linkedin-mcp/storage-state.json` | LinkedIn active cookies |
| `/mnt/8TB/projects/eslider/go-onlyoffice/.env` | OO creds for oo CLI |
| `/mnt/8TB/projects/eslider/go-onlyoffice/go.mod` | Deps for go-onlyoffice |
| `/mnt/8TB/projects/eslider/oo-workspace/go.mod` | Deps for oo-workspace |

---

## 6. Agent prompt (для запуска новой сессии)

Скопируйте этот промпт при старте новой сессии:

```
Ты — ассистент для разработки chat import pipeline в проекте 2dph (eSlider/2dph).

Прочитай docs/chat-import-plan.md полностью. Это план, находки и дизайн.

Задача: реализовать bin/chats — единый Go бинарник для sync/import/index/facts/apply
личных чатов из Telegram, WhatsApp, LinkedIn с последующей записью фактов в OnlyOffice CRM.

Правила:
1. TDD first — workflow use-case как тест. Реальные данные, без моков.
2. Go, всё в один бинарник bin/chats.
3. Source interface — каждая платформа плагином.
4. var/chats/telegram/ — полные имена платформ.
5. Начни с Telegram (session string активна).
6. После реализации — создай/обнови тесты.
7. Сохрани прогресс в docs/chat-import-plan.md.
8. Запроси разрешение перед apply в OO CRM.

Ключевые пути:
- /mnt/8TB/projects/ai/2dph/ — корень проекта
- /mnt/8TB/projects/eslider/mcp-servers/telegram-mcp/ — Telegram MCP
- /mnt/8TB/projects/eslider/mcp-servers/telegram.env — API ID/Hash + session
- /mnt/8TB/projects/eslider/go-onlyoffice/ — go-onlyoffice библиотека
- /home/ano/go/bin/oo — OO CLI

Готов начать.
```
=======
---

## 1. Находки / Discoveries

### Доступные платформы и данные

| Platform | Status | Credentials (path, not values) | Go Library |
|----------|--------|-------------------------------|------------|
| **Telegram** | ✅ Active MCP server, session string exists | API creds in `telegram.env` (see §5). Session string in `telegram-mcp/.env`. | MCP JSON-RPC (через telegram-mcp server) |
| **WhatsApp** | ✅ MCP server exists + Go bridge. **No `messages.db` yet** (empty). Needs QR re-auth + sync first | MCP: `/mnt/8TB/projects/eslider/mcp-servers/whatsapp-mcp/`. Go bridge uses `tulir/whatsmeow` | [`tulir/whatsmeow`](https://github.com/tulir/whatsmeow) (7k★, Go, multi-device API) |
| **LinkedIn** | ✅ Connected via `mcp-server-linkedin`. Full browser profile + cookies | Browser profile: `~/.linkedin-mcp/profile/`. Active cookies: `~/.linkedin-mcp/storage-state.json`. Cookie backup: `~/.linkedin-mcp/cookies.json.bak-agent`. Script cookies: `/mnt/8TB/projects/eslider/cv/bin/creds/linkedin.cookies.json`. JobHunt session: `/mnt/8TB/projects/eslider/JobHunt/sessions/linkedin.json`. Env: `/mnt/8TB/projects/eslider/cv/.env` (`LINKEDIN_AUTH_TOKEN`). | [`swiftlysingh/lnk`](https://github.com/swiftlysingh/lnk) (Go CLI, Voyager API) |
| **Gmail** | ✅ Already working pipeline (`bin/mail/sync.go` + `bin/mail/import`) | OAuth at `~/.gmail-mcp/` | Already in 2dph |
| **Twitter/X.com** | ⚠️ Password only, no MCP, no session. Not needed now. | `/mnt/8TB/projects/eslider/scratches/Accounts/twitter-pass.txt` | [`benoitpetit/xsh`](https://github.com/benoitpetit/xsh) (Go CLI, cookie auth) |
| **Google Calendar** | ❌ Not connected yet. Gmail OAuth could extend | None | Нужен Google Calendar API |

### Telegram session (проверен, активен)

- API credentials in: `/mnt/8TB/projects/eslider/mcp-servers/telegram.env`
  - `TELEGRAM_API_ID`, `TELEGRAM_API_HASH`, `TELEGRAM_PHONE` — загружаются из env
  - `TELEGRAM_SESSION_STRING` — активная Telethon session (в env)
- MCP server launcher: `/mnt/8TB/projects/eslider/mcp-servers/run-telegram-mcp.sh`
- Session generator: `/mnt/8TB/projects/eslider/mcp-servers/telegram-mcp/session_string_generator.py`
- Regenerator: `/mnt/8TB/projects/eslider/mcp-servers/regen-telegram-session.py`

### LinkedIn authentication (работает)

- Запускается через Cursor MCP: `uvx mcp-server-linkedin@latest --user-data-dir ~/.linkedin-mcp/profile --no-auto-import`
- Cookie файлы:
  - `~/.linkedin-mcp/storage-state.json` — Playwright storage state (активные cookies)
  - `~/.linkedin-mcp/cookies.json.bak-agent` — резерв
  - `~/.linkedin-mcp/cookies.json.bak-webtop-20260728T143535Z` — резерв
  - `/mnt/8TB/projects/eslider/cv/bin/creds/linkedin.cookies.json` — для скриптов
  - `/mnt/8TB/projects/eslider/JobHunt/sessions/linkedin.json` — JobHunt
- `li_at` token: в `storage-state.json` (см. §5)
- Для Go подхода: можно читать cookie из `storage-state.json` и использовать LinkedIn Voyager API напрямую

### WhatsApp bridge (не синхронизирован)

- Go bridge at: `/mnt/8TB/projects/eslider/mcp-servers/whatsapp-mcp/whatsapp-bridge/`
- Использует `tulir/whatsmeow` Go library (уже в `go.mod`)
- SQLite store: `whatsapp-bridge/store/messages.db` — **не существует** (0 байт), нужна первая авторизация по QR
- Launcher: `/mnt/8TB/projects/eslider/mcp-servers/run-whatsapp-bridge-lharries.sh`
- QR images готовились: `/mnt/8TB/projects/produktor/work/data/export/whatsapp-qr.png`
- Пароль: в `.env` OnlyOffice

### OnlyOffice доступ

- `.env` в корне 2dph: `ONLYOFFICE_URL`, `ONLYOFFICE_USER`, `ONLYOFFICE_PASS`
- `oo` CLI установлен: `/home/ano/go/bin/oo` (go-onlyoffice)
- go-onlyoffice repo: `/mnt/8TB/projects/eslider/go-onlyoffice/` (main, public)
- oo-workspace (каталог, private): `/mnt/8TB/projects/eslider/oo-workspace/`

### Gitea

- Instance: `https://git.produktor.io` (через `gitea-api` wrapper)
- Credentials: `~/.config/gitea/produktor.env`
- User: `eSlider`, admin
- Repos: 30+ приватных репозиториев
- **`eSlider/2dph` не существует на Gitea** — проект только на GitHub
- Для задачи: создать issue в существующем репо (например `eSlider/JobHunt`) или создать новый репо

### Rambox

Не используется. IndexedDB база не существует.

---

## 2. Design Decisions

### One binary: `bin/chats`

```go
// Source interface — каждая платформа реализует
type Source interface {
    Name() string                // "telegram", "whatsapp", "linkedin"
    Sync(ctx context.Context, out string) error
}

// CLI subcommands
chats sync telegram    [--limit N] [--since DATE]
chats sync whatsapp    [--qr] [--limit N]
chats sync linkedin    [--limit N]
chats import           // JSONL → markdown (all sources)
chats index            // rebuild var/kb.lbug
chats facts            // extract + cross-check
chats apply            // push to OO CRM [--dry-run]
```

### Directory layout (полные имена платформ)
||||||| Stash base
## What this document is for

This file captures the full context, research findings, design decisions, and
execution plan for implementing a single Go binary `bin/chats` that syncs
personal 1:1/group chats from Telegram, WhatsApp, and LinkedIn, converts them
to markdown, indexes them into the brain (`var/kb.lbug`), extracts contact
facts (phones, social links, projects, deals, associations), cross-checks
against ≥2 independent sources (detective method), and applies verified facts
to OnlyOffice CRM (update persons, contacts, opportunities, projects).

Save this file in context for the next agent session.

---

## 1. Находки / Discoveries

### Доступные платформы и данные

| Platform | Status | Credentials (path, not values) | Go Library |
|----------|--------|-------------------------------|------------|
| **Telegram** | ✅ Active MCP server, session string exists | API creds in `telegram.env` (see §5). Session string in `telegram-mcp/.env`. | MCP JSON-RPC (через telegram-mcp server) |
| **WhatsApp** | ✅ MCP server exists + Go bridge. **No `messages.db` yet** (empty). Needs QR re-auth + sync first | MCP: `/mnt/8TB/projects/eslider/mcp-servers/whatsapp-mcp/`. Go bridge uses `tulir/whatsmeow` | [`tulir/whatsmeow`](https://github.com/tulir/whatsmeow) (7k★, Go, multi-device API) |
| **LinkedIn** | ✅ Connected via `mcp-server-linkedin`. Full browser profile + cookies | Browser profile: `~/.linkedin-mcp/profile/`. Active cookies: `~/.linkedin-mcp/storage-state.json`. Cookie backup: `~/.linkedin-mcp/cookies.json.bak-agent`. Script cookies: `/mnt/8TB/projects/eslider/cv/bin/creds/linkedin.cookies.json`. JobHunt session: `/mnt/8TB/projects/eslider/JobHunt/sessions/linkedin.json`. Env: `/mnt/8TB/projects/eslider/cv/.env` (`LINKEDIN_AUTH_TOKEN`). | [`swiftlysingh/lnk`](https://github.com/swiftlysingh/lnk) (Go CLI, Voyager API) |
| **Gmail** | ✅ Already working pipeline (`bin/mail/sync.go` + `bin/mail/import`) | OAuth at `~/.gmail-mcp/` | Already in 2dph |
| **Twitter/X.com** | ⚠️ Password only, no MCP, no session. Not needed now. | `/mnt/8TB/projects/eslider/scratches/Accounts/twitter-pass.txt` | [`benoitpetit/xsh`](https://github.com/benoitpetit/xsh) (Go CLI, cookie auth) |
| **Google Calendar** | ❌ Not connected yet. Gmail OAuth could extend | None | Нужен Google Calendar API |

### Telegram session (проверен, активен)

- API credentials in: `/mnt/8TB/projects/eslider/mcp-servers/telegram.env`
  - `TELEGRAM_API_ID`, `TELEGRAM_API_HASH`, `TELEGRAM_PHONE` — загружаются из env
  - `TELEGRAM_SESSION_STRING` — активная Telethon session (в env)
- MCP server launcher: `/mnt/8TB/projects/eslider/mcp-servers/run-telegram-mcp.sh`
- Session generator: `/mnt/8TB/projects/eslider/mcp-servers/telegram-mcp/session_string_generator.py`
- Regenerator: `/mnt/8TB/projects/eslider/mcp-servers/regen-telegram-session.py`

### LinkedIn authentication (работает)

- Запускается через Cursor MCP: `uvx mcp-server-linkedin@latest --user-data-dir ~/.linkedin-mcp/profile --no-auto-import`
- Cookie файлы:
  - `~/.linkedin-mcp/storage-state.json` — Playwright storage state (активные cookies)
  - `~/.linkedin-mcp/cookies.json.bak-agent` — резерв
  - `~/.linkedin-mcp/cookies.json.bak-webtop-20260728T143535Z` — резерв
  - `/mnt/8TB/projects/eslider/cv/bin/creds/linkedin.cookies.json` — для скриптов
  - `/mnt/8TB/projects/eslider/JobHunt/sessions/linkedin.json` — JobHunt
- `li_at` token: в `storage-state.json` (см. §5)
- Для Go подхода: можно читать cookie из `storage-state.json` и использовать LinkedIn Voyager API напрямую

### WhatsApp bridge (не синхронизирован)

- Go bridge at: `/mnt/8TB/projects/eslider/mcp-servers/whatsapp-mcp/whatsapp-bridge/`
- Использует `tulir/whatsmeow` Go library (уже в `go.mod`)
- SQLite store: `whatsapp-bridge/store/messages.db` — **не существует** (0 байт), нужна первая авторизация по QR
- Launcher: `/mnt/8TB/projects/eslider/mcp-servers/run-whatsapp-bridge-lharries.sh`
- QR images готовились: `/mnt/8TB/projects/produktor/work/data/export/whatsapp-qr.png`
- Пароль: в `.env` OnlyOffice

### OnlyOffice доступ

- `.env` в корне 2dph: `ONLYOFFICE_URL`, `ONLYOFFICE_USER`, `ONLYOFFICE_PASS`
- `oo` CLI установлен: `/home/ano/go/bin/oo` (go-onlyoffice)
- go-onlyoffice repo: `/mnt/8TB/projects/eslider/go-onlyoffice/` (main, public)
- oo-workspace (каталог, private): `/mnt/8TB/projects/eslider/oo-workspace/`

### Gitea

- Instance: `https://git.produktor.io` (через `gitea-api` wrapper)
- Credentials: `~/.config/gitea/produktor.env`
- User: `eSlider`, admin
- Repos: 30+ приватных репозиториев
- **`eSlider/2dph` не существует на Gitea** — проект только на GitHub
- Для задачи: создать issue в существующем репо (например `eSlider/JobHunt`) или создать новый репо

### Rambox

Не используется. IndexedDB база не существует.

---

## 2. Design Decisions

### One binary: `bin/chats`

```go
// Source interface — каждая платформа реализует
type Source interface {
    Name() string                // "telegram", "whatsapp", "linkedin"
    Sync(ctx context.Context, out string) error
}

// CLI subcommands
chats sync telegram    [--limit N] [--since DATE]
chats sync whatsapp    [--qr] [--limit N]
chats sync linkedin    [--limit N]
chats import           // JSONL → markdown (all sources)
chats index            // rebuild var/kb.lbug
chats facts            // extract + cross-check
chats apply            // push to OO CRM [--dry-run]
```

### Directory layout (полные имена платформ)
=======
## Key env vars (set in shell, never committed)
>>>>>>> Stashed changes

```
TELEGRAM_MCP_DIR           telegram-mcp directory
TELEGRAM_API_ID            from telegram.env
TELEGRAM_API_HASH          from telegram.env
TELEGRAM_PHONE             from telegram.env
TELEGRAM_SESSION_STRING    from telegram-mcp/.env
ONLYOFFICE_URL             from .env
ONLYOFFICE_USER            from .env
ONLYOFFICE_PASS            from .env
KNOWLEDGE_MESH_SEED        path to knowledge-mesh-seed.yaml (for facts/crm)
OO_CLI                     path to oo binary (default: $HOME/go/bin/oo)
```

## Quick reference

```bash
./bin/chat sync telegram --limit 100
./bin/chat import
./bin/chat index        # delegates to bin/kb/index
./bin/chat facts
./bin/chat apply --dry-run
```
<<<<<<< Updated upstream

### MD format (YAML frontmatter)

```markdown
---
id: tg_12345
platform: telegram
chat_id: "-100123456"
chat_name: "John Doe"
participants: ["me", "John Doe"]
message_count: 1500
type: personal
---

# Чат с John Doe

**2026-01-15 10:30** — me: Hello!
```

### Фильтр "личный чат"

- ≤3 участников
- Боты не считаются (Telegram: `user.is_bot == false`)
- Не канал, не публичная группа
- Telegram: `dialog.Type == "user"` или супергруппа с `participants_count <= 3` (исключая ботов)
- WhatsApp: `@s.whatsapp.net` (не `@g.us`)
- LinkedIn: 1:1 conversation

### Очередность реализации

1. **Telegram** — самый простой (session string + API ID/Hash активны)
2. **Import** — конвертер JSONL → MD
3. **Index** — интеграция с brain
4. **WhatsApp** — после первого QR
5. **Facts** — извлечение контактных данных
6. **LinkedIn** — после проверки cookie
7. **Apply** — запись в OO CRM

### Тестирование (TDD, system tests only)

- Никаких unit-тестов, никаких моков
- Тесты используют синтетические данные (слепки)
- Workflow use-case как тест:
  1. Написать тест с реальным сценарием (sync → import → index → search)
  2. Проверить что файлы созданы
  3. Проверить что поиск возвращает ожидаемые результаты
- A/B тест: сравнить два последовательных sync на идентичность
- Integration: тест через `oo` CLI с `--dry-run`

### Cross-check (detective method)

Перед записью в OO CRM — минимум 2 независимых источника:
- S1: Чат (текст сообщения, автор, дата)
- S2: OO CRM (существующий контакт/компания/deal) через `oo persons list` / `oo persons get`
- S3 (опционально): Corpus SoT (`knowledge-mesh-seed.yaml`) или web-search
- Факты с ≥2 источниками → `root=facts` в brain + `approve=true` для CRM
- Факты с 1 источником → предложение на review (catalog-паттерн)

### Apply в OO CRM

Через `oo` CLI:
- `oo persons update <id> --about ... --job-title ...`
- `oo contacts info-add <id> --type Phone|LinkedIn --value ...`
- `oo opportunities create --contact-id ... --title ... --stage ...`
- `oo persons update <id> --company-id ...` (ассоциация)
- `oo projects contacts <project-id>` (связь с проектом)

---

## 3. Implementation Plan

### Phase 1: Telegram sync + import (MVP)

1. Create `bin/chats/` directory structure (nested Go module like `bin/kbsearch/`)
2. Implement `Source` interface
3. Implement `TelegramSource` — MCP JSON-RPC клиент к telegram-mcp
4. Write JSONL output to `var/chats/telegram/<id>/messages.jsonl`
5. Implement `chats import` — reads all JSONL, writes MD
6. Write system test: sync → import → verify output

### Phase 2: Brain indexing

1. Implement `chats index` — rebuild `var/kb.lbug` with corpus + chats
2. Write system test: sync → import → index → `bin/kb/search "query"` → verify recall

### Phase 3: WhatsApp

1. Implement `WhatsAppSource` using `tulir/whatsmeow`
2. First run requires QR auth (save session for later reuse)
3. Write sync output to `var/chats/whatsapp/<jid>/messages.jsonl`

### Phase 4: Facts extraction + cross-check

1. Implement `chats facts`:
   - Phone regex: `\+?\d{7,15}` + validation (exclude dates/amounts/cards)
   - LinkedIn URL regex: `linkedin\.com/in/[\w-]+`
   - Cross-check: match extracted values against OO CRM API
2. Write facts to brain as `root=facts`

### Phase 5: LinkedIn + Apply

1. Implement `LinkedInSource` using Voyager API + cookies from `storage-state.json`
2. Implement `chats apply`:
   - Build YAML catalog (as `oo-workspace` does)
   - Run `oo catalog match` → review → `oo catalog apply`

---

## 4. Existing code references

| Code | Path | Purpose |
|------|------|---------|
| Mail sync | `bin/mail/sync.go` + `bin/mail/sync/` | Pattern for chat sync (Go subprocess pattern) |
| Mail import | `bin/mail/import` | Pattern for JSONL → MD conversion |
| Mail index | `bin/mail/index_mail` | Pattern for brain rebuild with new corpus |
| kbsearch | `bin/kbsearch/` | Go module pattern (nested, `system_ladybug` tag) |
| kb/index | `bin/kb/index` | Python brain index (upsert_leaf, init_schema) |
| kblib | `bin/tools/kblib.py` | Python brain core (connect, upsert_leaf, hybrid_search) |
| facts/crm | `bin/facts/crm` | Cross-check CRM facts pattern |
| facts/extract | `bin/facts/extract` | 2-source fact extraction pattern |
| oo-workspace catalog | `/mnt/8TB/projects/eslider/oo-workspace/catalog/` | Catalog scan/match/apply pattern (VCF, Thunderbird, projects) |
| WhatsApp bridge | `/mnt/8TB/projects/eslider/mcp-servers/whatsapp-mcp/whatsapp-bridge/` | Existing `whatsmeow` Go bridge |
| go-onlyoffice | `/mnt/8TB/projects/eslider/go-onlyoffice/` | OO CRM API library |
| oo CLI | `/home/ano/go/bin/oo` | Built go-onlyoffice CLI |

---

## 5. Critical files (paths only, no secrets)

| File | Content |
|------|---------|
| `~/.config/gitea/produktor.env` | Gitea URL + token |
| `/mnt/8TB/projects/eslider/mcp-servers/telegram.env` | Telegram API ID/Hash + phone + session |
| `/mnt/8TB/projects/eslider/mcp-servers/telegram-mcp/.env` | Telegram session string |
| `/mnt/8TB/projects/ai/2dph/.env` | ONLYOFFICE URL/USER/PASS |
| `/mnt/8TB/projects/eslider/cv/.env` | LinkedIn auth token |
| `~/.linkedin-mcp/storage-state.json` | LinkedIn active cookies |
| `/mnt/8TB/projects/eslider/go-onlyoffice/.env` | OO creds for oo CLI |
| `/mnt/8TB/projects/eslider/go-onlyoffice/go.mod` | Deps for go-onlyoffice |
| `/mnt/8TB/projects/eslider/oo-workspace/go.mod` | Deps for oo-workspace |

---

## 6. Agent prompt (для запуска новой сессии)

Скопируйте этот промпт при старте новой сессии:

```
Ты — ассистент для разработки chat import pipeline в проекте 2dph (eSlider/2dph).

Прочитай docs/chat-import-plan.md полностью. Это план, находки и дизайн.

Задача: реализовать bin/chats — единый Go бинарник для sync/import/index/facts/apply
личных чатов из Telegram, WhatsApp, LinkedIn с последующей записью фактов в OnlyOffice CRM.

Правила:
1. TDD first — workflow use-case как тест. Синтетические данные (Alice, Bob).
2. Go, всё в один бинарник bin/chats.
3. Source interface — каждая платформа плагином.
4. var/chats/telegram/ — полные имена платформ.
5. Начни с Telegram (session string активна, через MCP server).
6. После реализации — создай/обнови тесты.
7. Сохрани прогресс в docs/chat-import-plan.md.
8. Запроси разрешение перед apply в OO CRM.
9. НИКАКИХ реальных имён, телефонов, email в коммитах — только ссылки на env файлы.

Ключевые пути:
- /mnt/8TB/projects/ai/2dph/ — корень проекта
- /mnt/8TB/projects/eslider/mcp-servers/telegram-mcp/ — Telegram MCP
- /mnt/8TB/projects/eslider/mcp-servers/telegram.env — API ID/Hash + session (не коммитить!)
- /mnt/8TB/projects/eslider/go-onlyoffice/ — go-onlyoffice библиотека
- /home/ano/go/bin/oo — OO CLI

Готов начать.
```

---

## 7. Implementation Progress

### Session: 2026-08-12 — Phase 1 complete, sync проверен с реальными данными

**Результаты тестового синка (--limit=100, 31 личный чат):**
- Подключение к Telegram MCP server: 1.5с
- Получен 31 личный чат (отфильтровано боты + Telegram service)
- Синк всех чатов: 7.7с (1 flood wait на 3с, обработан MCP сервером)
- Записано 922 сообщения в JSONL
- Import: 30 чатов в MD (1 пустой — Saved Messages)
- Факты: 14 phone + 2 email после фильтрации

**Ключевое изменение: `gotd/td` → Telegram MCP Server**
- Вместо прямого gotd/td подключения (FLOOD_WAIT из-за 5 уже запущенных MCP серверов)
- Используется MCP JSON-RPC (JSONL, `\n`-delimited, **не** Content-Length headers)
- Переиспользует существующий Telethon session string (без phone auth)
- Стартует транзиентный MCP сервер на время синка, убивает после
- Инструменты: `list_chats` + `get_history` через MCP protocol v1.8+

**Файлы:**

| Файл | Роль |
|------|------|
| `bin/chats/mcpclient.go` | MCP JSON-RPC client + `TelegramMCPSource` |
| `bin/chats/sync_cmd.go` | CLI: `chats sync telegram --limit N` |
| `bin/chats/import_cmd.go` | JSONL → MD с YAML frontmatter |
| `bin/chats/index_cmd.go` | Делегирует `bin/kb/index --corpus` |
| `bin/chats/facts_cmd.go` | Regex extraction (phone/email/linkedin) с валидацией |
| `bin/chats/apply_cmd.go` | OO CRM cross-check + apply через `oo` CLI + --dry-run |
| `bin/chat` | build+exec wrapper (как `bin/kb/search`) |

**Phone regex улучшен:**
- Фильтр: исключает даты (`2026-06-14`), суммы (`2 500 000`), номера карт (16 digits), инвойсы (`25-002332001`)
- Валидация: >=7 digits, <=15 digits, без `000` pattern, не дата
- Результат: 37 → 16 фактов (14 phone + 2 email)

**Cross-check + Apply в OO CRM (без персональных данных в файлах):**
- Найден существующий контакт через first name: добавлен phone + email
- Создан новый контакт: phone добавлен
- Apply через `oo contacts info-add <id> --type Phone|Email --value ...`

**Brain:** 132 leafs (120 info + 12 facts), чаты заиндексены

**Безопасность:**
- `var/` в `.gitignore` — сырые чат-данные не коммитятся
- credentials в env файлах, не в коде
- Тесты используют синтетические данные (Alice, Bob, Charlie, Diana)
- Этот документ не содержит реальных значений API ключей, токенов, паролей или персональных данных третьих лиц

**Что дальше (следующая сессия):**
1. WhatsApp source (`tulir/whatsmeow`) — после первого QR
2. LinkedIn source (Voyager API через cookies)
3. Full history sync (без --limit) для всех чатов
4. Telegram session reuse через MCP server (уже работает)
5. Apply через `bin/chats apply` (уже работает, улучшен поиск контактов)
>>>>>>> 313599b (bin/chats: Phase 1 MVP — Telegram sync/import/index/facts/apply)
||||||| Stash base

### MD format (YAML frontmatter)

```markdown
---
id: tg_12345
platform: telegram
chat_id: "-100123456"
chat_name: "John Doe"
participants: ["me", "John Doe"]
message_count: 1500
type: personal
---

# Чат с John Doe

**2026-01-15 10:30** — me: Hello!
```

### Фильтр "личный чат"

- ≤3 участников
- Боты не считаются (Telegram: `user.is_bot == false`)
- Не канал, не публичная группа
- Telegram: `dialog.Type == "user"` или супергруппа с `participants_count <= 3` (исключая ботов)
- WhatsApp: `@s.whatsapp.net` (не `@g.us`)
- LinkedIn: 1:1 conversation

### Очередность реализации

1. **Telegram** — самый простой (session string + API ID/Hash активны)
2. **Import** — конвертер JSONL → MD
3. **Index** — интеграция с brain
4. **WhatsApp** — после первого QR
5. **Facts** — извлечение контактных данных
6. **LinkedIn** — после проверки cookie
7. **Apply** — запись в OO CRM

### Тестирование (TDD, system tests only)

- Никаких unit-тестов, никаких моков
- Тесты используют синтетические данные (слепки)
- Workflow use-case как тест:
  1. Написать тест с реальным сценарием (sync → import → index → search)
  2. Проверить что файлы созданы
  3. Проверить что поиск возвращает ожидаемые результаты
- A/B тест: сравнить два последовательных sync на идентичность
- Integration: тест через `oo` CLI с `--dry-run`

### Cross-check (detective method)

Перед записью в OO CRM — минимум 2 независимых источника:
- S1: Чат (текст сообщения, автор, дата)
- S2: OO CRM (существующий контакт/компания/deal) через `oo persons list` / `oo persons get`
- S3 (опционально): Corpus SoT (`knowledge-mesh-seed.yaml`) или web-search
- Факты с ≥2 источниками → `root=facts` в brain + `approve=true` для CRM
- Факты с 1 источником → предложение на review (catalog-паттерн)

### Apply в OO CRM

Через `oo` CLI:
- `oo persons update <id> --about ... --job-title ...`
- `oo contacts info-add <id> --type Phone|LinkedIn --value ...`
- `oo opportunities create --contact-id ... --title ... --stage ...`
- `oo persons update <id> --company-id ...` (ассоциация)
- `oo projects contacts <project-id>` (связь с проектом)

---

## 3. Implementation Plan

### Phase 1: Telegram sync + import (MVP)

1. Create `bin/chats/` directory structure (nested Go module like `bin/kbsearch/`)
2. Implement `Source` interface
3. Implement `TelegramSource` — MCP JSON-RPC клиент к telegram-mcp
4. Write JSONL output to `var/chats/telegram/<id>/messages.jsonl`
5. Implement `chats import` — reads all JSONL, writes MD
6. Write system test: sync → import → verify output

### Phase 2: Brain indexing

1. Implement `chats index` — rebuild `var/kb.lbug` with corpus + chats
2. Write system test: sync → import → index → `bin/kb/search "query"` → verify recall

### Phase 3: WhatsApp

1. Implement `WhatsAppSource` using `tulir/whatsmeow`
2. First run requires QR auth (save session for later reuse)
3. Write sync output to `var/chats/whatsapp/<jid>/messages.jsonl`

### Phase 4: Facts extraction + cross-check

1. Implement `chats facts`:
   - Phone regex: `\+?\d{7,15}` + validation (exclude dates/amounts/cards)
   - LinkedIn URL regex: `linkedin\.com/in/[\w-]+`
   - Cross-check: match extracted values against OO CRM API
2. Write facts to brain as `root=facts`

### Phase 5: LinkedIn + Apply

1. Implement `LinkedInSource` using Voyager API + cookies from `storage-state.json`
2. Implement `chats apply`:
   - Build YAML catalog (as `oo-workspace` does)
   - Run `oo catalog match` → review → `oo catalog apply`

---

## 4. Existing code references

| Code | Path | Purpose |
|------|------|---------|
| Mail sync | `bin/mail/sync.go` + `bin/mail/sync/` | Pattern for chat sync (Go subprocess pattern) |
| Mail import | `bin/mail/import` | Pattern for JSONL → MD conversion |
| Mail index | `bin/mail/index_mail` | Pattern for brain rebuild with new corpus |
| kbsearch | `bin/kbsearch/` | Go module pattern (nested, `system_ladybug` tag) |
| kb/index | `bin/kb/index` | Python brain index (upsert_leaf, init_schema) |
| kblib | `bin/tools/kblib.py` | Python brain core (connect, upsert_leaf, hybrid_search) |
| facts/crm | `bin/facts/crm` | Cross-check CRM facts pattern |
| facts/extract | `bin/facts/extract` | 2-source fact extraction pattern |
| oo-workspace catalog | `/mnt/8TB/projects/eslider/oo-workspace/catalog/` | Catalog scan/match/apply pattern (VCF, Thunderbird, projects) |
| WhatsApp bridge | `/mnt/8TB/projects/eslider/mcp-servers/whatsapp-mcp/whatsapp-bridge/` | Existing `whatsmeow` Go bridge |
| go-onlyoffice | `/mnt/8TB/projects/eslider/go-onlyoffice/` | OO CRM API library |
| oo CLI | `/home/ano/go/bin/oo` | Built go-onlyoffice CLI |

---

## 5. Critical files (paths only, no secrets)

| File | Content |
|------|---------|
| `~/.config/gitea/produktor.env` | Gitea URL + token |
| `/mnt/8TB/projects/eslider/mcp-servers/telegram.env` | Telegram API ID/Hash + phone + session |
| `/mnt/8TB/projects/eslider/mcp-servers/telegram-mcp/.env` | Telegram session string |
| `/mnt/8TB/projects/ai/2dph/.env` | ONLYOFFICE URL/USER/PASS |
| `/mnt/8TB/projects/eslider/cv/.env` | LinkedIn auth token |
| `~/.linkedin-mcp/storage-state.json` | LinkedIn active cookies |
| `/mnt/8TB/projects/eslider/go-onlyoffice/.env` | OO creds for oo CLI |
| `/mnt/8TB/projects/eslider/go-onlyoffice/go.mod` | Deps for go-onlyoffice |
| `/mnt/8TB/projects/eslider/oo-workspace/go.mod` | Deps for oo-workspace |

---

## 6. Agent prompt (для запуска новой сессии)

Скопируйте этот промпт при старте новой сессии:

```
Ты — ассистент для разработки chat import pipeline в проекте 2dph (eSlider/2dph).

Прочитай docs/chat-import-plan.md полностью. Это план, находки и дизайн.

Задача: реализовать bin/chats — единый Go бинарник для sync/import/index/facts/apply
личных чатов из Telegram, WhatsApp, LinkedIn с последующей записью фактов в OnlyOffice CRM.

Правила:
1. TDD first — workflow use-case как тест. Синтетические данные (Alice, Bob).
2. Go, всё в один бинарник bin/chats.
3. Source interface — каждая платформа плагином.
4. var/chats/telegram/ — полные имена платформ.
5. Начни с Telegram (session string активна, через MCP server).
6. После реализации — создай/обнови тесты.
7. Сохрани прогресс в docs/chat-import-plan.md.
8. Запроси разрешение перед apply в OO CRM.
9. НИКАКИХ реальных имён, телефонов, email в коммитах — только ссылки на env файлы.

Ключевые пути:
- /mnt/8TB/projects/ai/2dph/ — корень проекта
- /mnt/8TB/projects/eslider/mcp-servers/telegram-mcp/ — Telegram MCP
- /mnt/8TB/projects/eslider/mcp-servers/telegram.env — API ID/Hash + session (не коммитить!)
- /mnt/8TB/projects/eslider/go-onlyoffice/ — go-onlyoffice библиотека
- /home/ano/go/bin/oo — OO CLI

Готов начать.
```

---

## 7. Implementation Progress

### Session: 2026-08-12 — Phase 1 complete, sync проверен с реальными данными

**Результаты тестового синка (--limit=100, 31 личный чат):**
- Подключение к Telegram MCP server: 1.5с
- Получен 31 личный чат (отфильтровано боты + Telegram service)
- Синк всех чатов: 7.7с (1 flood wait на 3с, обработан MCP сервером)
- Записано 922 сообщения в JSONL
- Import: 30 чатов в MD (1 пустой — Saved Messages)
- Факты: 14 phone + 2 email после фильтрации

**Ключевое изменение: `gotd/td` → Telegram MCP Server**
- Вместо прямого gotd/td подключения (FLOOD_WAIT из-за 5 уже запущенных MCP серверов)
- Используется MCP JSON-RPC (JSONL, `\n`-delimited, **не** Content-Length headers)
- Переиспользует существующий Telethon session string (без phone auth)
- Стартует транзиентный MCP сервер на время синка, убивает после
- Инструменты: `list_chats` + `get_history` через MCP protocol v1.8+

**Файлы:**

| Файл | Роль |
|------|------|
| `bin/chats/mcpclient.go` | MCP JSON-RPC client + `TelegramMCPSource` |
| `bin/chats/sync_cmd.go` | CLI: `chats sync telegram --limit N` |
| `bin/chats/import_cmd.go` | JSONL → MD с YAML frontmatter |
| `bin/chats/index_cmd.go` | Делегирует `bin/kb/index --corpus` |
| `bin/chats/facts_cmd.go` | Regex extraction (phone/email/linkedin) с валидацией |
| `bin/chats/apply_cmd.go` | OO CRM cross-check + apply через `oo` CLI + --dry-run |
| `bin/chat` | build+exec wrapper (как `bin/kb/search`) |

**Phone regex улучшен:**
- Фильтр: исключает даты (`2026-06-14`), суммы (`2 500 000`), номера карт (16 digits), инвойсы (`25-002332001`)
- Валидация: >=7 digits, <=15 digits, без `000` pattern, не дата
- Результат: 37 → 16 фактов (14 phone + 2 email)

**Cross-check + Apply в OO CRM (без персональных данных в файлах):**
- Найден существующий контакт через first name: добавлен phone + email
- Создан новый контакт: phone добавлен
- Apply через `oo contacts info-add <id> --type Phone|Email --value ...`

**Brain:** 132 leafs (120 info + 12 facts), чаты заиндексены

**Безопасность:**
- `var/` в `.gitignore` — сырые чат-данные не коммитятся
- credentials в env файлах, не в коде
- Тесты используют синтетические данные (Alice, Bob, Charlie, Diana)
- Этот документ не содержит реальных значений API ключей, токенов, паролей или персональных данных третьих лиц

**Что дальше (следующая сессия):**
1. WhatsApp source (`tulir/whatsmeow`) — после первого QR
2. LinkedIn source (Voyager API через cookies)
3. Full history sync (без --limit) для всех чатов
4. Telegram session reuse через MCP server (уже работает)
5. Apply через `bin/chats apply` (уже работает, улучшен поиск контактов)
=======
>>>>>>> Stashed changes
