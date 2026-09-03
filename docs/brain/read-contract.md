# Контракт чтения brain (P-9.4)

Нормативный формат ответов `search` / `get` / `stats` / `audit` — как клиенты
и агенты читают факты, info и аудит из Ladybug. Зеркало write-контракта
[`docs/brain/contract.md`](contract.md) (P-9.2/P-9.3): там — как писать leaf
(source/external_id/kind/text, ContentHash, observed_at), здесь — как читать.

Реализация схемы: `internal/contract/read.go` (cgo-free, версия
`contract.ReadContractVersion`); продьюсеры ответов — `internal/brain`
(search/get/stats/audit), HTTP-поверхность — `pkg/httpapi`
(`bin/brain/serve.go`, :8630) + MCP (`POST /mcp`), CLI — `bin/brain/*.go --json`;
сервисный клиент для потребителей — `pkg/brainclient` (P-9.5, раздел
[«Как клиенту читать brain»](#клиентский-слой-p-95)).

## Поверхности чтения (один формат)

| Поверхность | Команда / путь |
|-------------|----------------|
| CLI | `bin/brain/search.go "q" --json`, `bin/brain/get.go <id> --json`, `bin/brain/stats.go --json` |
| HTTP | `GET /search?q=`, `GET /get?id=`, `GET /stats`, `GET /audit` (:8630) |
| MCP | tools `search` / `get` / `stats` / `audit` (`POST /mcp`) |
| Клиент | SDK `pkg/brainclient`, CLI `bin/brain/client.go search|get|stats|audit` (P-9.5, раздел «Клиентский слой») |

Все поверхности отдают один и тот же JSON (схемы ниже); HTTP/MCP — тонкий
транспорт над теми же структурами. **Клиенты читают brain только через эти
форматы — никогда не парсят `var/kb.lbug` напрямую** (схема Ladybug —
внутренняя, меняется без предупреждения).

## Версионность контракта

Каждый ответ несёт поле **`contract_version`** (semver `MAJOR.MINOR`,
сейчас `1.0`). Правки контракта:

- **additive** (minor bump): новое опциональное поле, уточнение доков, новый
  домен значения. Обратно совместимо — старые клиенты работают без изменений.
- **breaking** (major bump): удаление/переименование обязательного поля, смена
  типа или семантики. Требует миграции всех клиентов.

Правила клиентов:

- Принимать **любой** ответ с совместимым MAJOR; не требовать точного MINOR.
- Игнорировать неизвестные поля (additive evolution) — не валиться на них.
- Отсутствие `contract_version` = ответ сформирован **до** контракта
  (старый сервис); формат полей при этом тот же — гейт допускает такой ответ
  через `--relax-version` (warning, не failure).

Потребители контракта: агенты (PicoClaw gateway — `search → get → audit`),
`gator`/`cv`-клиенты, `bin/brain`-инструменты, скрипты стека.

## Форматы ответов (JSON-схемы)

Общие правила: **отсутствие → отсутствие** — опциональное поле, которого нет,
просто отсутствует в JSON (`omitempty`), не `null` и не пустая строка.
Обязательные поля всегда присутствуют. `results`/`by_confidence` — всегда
массив (может быть пустым), не `null`.

### search — `GET /search?q=…` / CLI `--json`

| Поле | Тип | Обязательно | Смысл |
|------|-----|-------------|-------|
| `contract_version` | string | да | версия контракта, `MAJOR.MINOR` |
| `query` | string | да | переданный запрос |
| `root_filter` | string | да | `facts` / `info`; `""` = все корни |
| `as_of` | string | нет | `YYYY-MM-DD` (D24), фильтр валидности |
| `count` | int | да | число результатов (`>= 0`) |
| `results` | array | да | хиты (пусто, если нет совпадений) |
| `web` | object | нет | второй независимый источник (SearXNG) |

Хит (`results[]`):

| Поле | Тип | Обязательно | Смысл |
|------|-----|-------------|-------|
| `id` | string | да | id листа |
| `text` | string | да | полный текст листа |
| `root` | string | да | `facts` / `info` |
| `confidence` | string | нет | `confirmed` / `estimated` / `inferred` / … |
| `score` | float | да | оценка релевантности (RRF/косинус) |
| `snippet` | string | нет | до 280 рун текста |
| `valid_from` / `valid_to` | string | нет | `YYYY-MM-DD` (D24) |
| `hops` | array | нет | обход `--hop N` (`id`/`label`/`name`/`depth`) |

`web` блок: `status` (обязательно: `ok`/`throttled`/`skipped`/`refused`/…),
`note`, `cached`, `results[]` (`rank`/`title`/`url`/`snippet`/`engine`).

### get — `GET /get?id=…` / CLI `--json`

| Поле | Тип | Обязательно | Смысл |
|------|-----|-------------|-------|
| `contract_version` | string | да | версия контракта |
| `id` | string | да | id листа |
| `root` | string | да | `facts` / `info` |
| `confidence` | string | да | `confirmed` / `estimated` / `inferred` / … |
| `source` | string | да | корпус: `mail` / `git` / `chats` / `docs` / `facts` |
| `type` | string | да | kind листа (`fact` / `mail` / `chat` / `commit` / …) |
| `valid_from` / `valid_to` | string | нет | `YYYY-MM-DD` (D24) |
| `snippet` | string | нет | без `--body` (до 280 рун) |
| `text` | string | нет | только с `--body` (полный текст) |

`text` и `snippet` взаимоисключающие: `--body` → `text`, иначе `snippet`.

### stats — `GET /stats` / CLI `--json`

| Поле | Тип | Обязательно | Смысл |
|------|-----|-------------|-------|
| `contract_version` | string | да | версия контракта |
| `total` | int | да | всего leafs (`>= 0`) |
| `by_root` | object | да | `{"facts": N, "info": N}` — фактические корни |
| `db` | string | да | путь к kb.lbug |
| `model` | string | нет | CLI-расширение: id embedding-модели |
| `ann` | object | нет | HTTP-расширение: статус ANN-индекса |

Required-ядро едино для CLI и HTTP; `model` и `ann` — аддитивные расширения
поверхностей (клиент не должен на них полагаться).

### audit — `GET /audit` / MCP `audit`

| Поле | Тип | Обязательно | Смысл |
|------|-----|-------------|-------|
| `contract_version` | string | да | версия контракта |
| `status` | string | да | `"ok"` |
| `by_confidence` | array | да | гистограмма root × confidence |

Строка `by_confidence[]`: `root` (`facts`/`info`), `confidence`, `count`
(`>= 0`). Это **формат ответа** чтения аудита; сами аудит-карточки
Vinogradov (L-9, issues #229–234) — отдельный домен (`docs/facts/`,
`bin/facts/audit.go`), в этот контракт не входят. Аудит соответствия
write-контракту — `bin/brain/audit-contract.go` (своя схема отчёта, P-9.2).

## Семантика: root, confidence, (not confirmed)

Единые термины с write-контрактом (contract.md):

- **`root=facts`** — утверждение, подтверждённое **≥2 независимыми
  источниками**, `confidence: confirmed`. Это единственный корень, с которого
  ответ считается фактом.
- **`root=info`** — описательный/нарративный leaf корпуса (mail/git/chats/docs),
  поисковый, никогда не утверждается.
- **`confidence`** — домен открыт: `confirmed` / `estimated` / `inferred` /
  `hypothesis` / `partial` / …. Пустое значение (legacy leafs) трактуется
  как `confirmed`.
- **`(not confirmed)`** — ответ не сходит с facts-корня: info-хит,
  hypothesis/partial, либо только один независимый источник. Агент обязан
  помечать такой ответ `(not confirmed)`, а не выдавать за факт.
- Дедuction-порядок поиска: facts → info → web. `--root facts` возвращает
  только подтверждённые; без фильтра info-хиты помечаются `(not confirmed)`.
- `web`-блок держится отдельно от графовых hits: «наши» vs «не наши».
  `throttled`/`skipped`/`refused` — не доказательство отсутствия.

## Время: `--as-of` (valid_from/valid_to) и observed_at

- **`valid_from` / `valid_to`** — валидность факта **по дням** (D24,
  `YYYY-MM-DD`), **ортогональна версии** (версия = id/ContentHash).
  `--as-of YYYY-MM-DD` оставляет hits, чей интервал покрывает день: пустой
  интервал = «всегда» (legacy), пустой `valid_to` = открытый конец, оба конца
  включительно. Это фильтр по смыслу факта, не по дате записи.
- **`observed_at`** — когда контент **увиден** (RFC3339), часть write-
  контракта, **не входит в dedup-ключ** (ContentHash): тот же контент,
  записанный позже, дедуплицируется к той же версии. В read-ответах поле
  сейчас **не экспонируется** (аддитивное будущее); читается через аудит
  write-контракта (`bin/brain/audit-contract.go`).

## Гейт соответствия (read-only)

Контракт проверяется скриптом, а не только текстом. Оба гейта read-only,
работают параллельно с живым сервисом (Ladybug: второй RW-open невозможен,
RO-open разрешён).

```bash
# Формат ответов живого сервиса (HTTP :8630):
./bin/brain/read-contract.go                       # строго (требует contract_version)
./bin/brain/read-contract.go --relax-version       # сервис до контракта: формат проверяется, версия — warning
./bin/brain/read-contract.go --base URL --token T  # другой сервис
./bin/brain/read-contract.go --json                # машиночитаемый отчёт

# Инварианты данных на живой kb.lbug (не нужен поднятый сервис):
bin/cgo/zig go run -tags=system_ladybug,brain_readcontract_db bin/brain/read-contract-db.go
bin/cgo/zig go run -tags=system_ladybug,brain_readcontract_db bin/brain/read-contract-db.go --json
```

Что проверяет HTTP-гейт: search (общий + `root=facts` + `as_of`), get (с телом
и без), stats, audit — обязательные поля, типы, домены (`root` ∈ facts|info,
даты `YYYY-MM-DD`), отсутствие → отсутствие. DB-гейт воспроизводит формы
ответов из живых данных и прогоняет те же валидаторы (выборка 200 leafs).

Прогон 2026-09-02 на живой kb.lbug (105210 leafs, сервис :8630):

```
$ bin/cgo/zig go run -tags=system_ladybug,brain_readcontract_db bin/brain/read-contract-db.go
read contract gate: db (contract_version=1.0)
  PASS   stats    total=105210 roots=2
  PASS   audit    rows=2
  PASS   sample   search/get fields ok (sample=200)
gate: PASS

$ ./bin/brain/read-contract.go --relax-version   # live :8630 до редеплоя
read contract gate: http:http://127.0.0.1:8630 (contract_version=1.0)
  PASS*  search   ok (warnings)     # warning: service predates read-contract
  …
gate: PASS
```

## Клиентский слой (P-9.5)

Единый клиентский слой поверх read-контракта, чтобы gator/cv/агенты/скрипты
не звали HTTP/MCP/CLI каждый по-своему и не открывали `var/kb.lbug`:

| Слой | Где | Что |
|------|-----|-----|
| SDK | `pkg/brainclient` (cgo-free) | typed-клиент: `Search`/`Get`/`Stats`/`Audit` → `internal/contract`-ответы; ответы валидируются контрактными валидаторами (формат + `contract_version`, несовместимый сервис — ошибка); «гейт facts» (`Gate`, `Facts`, `GateAudit`) |
| CLI | `bin/brain/client.go` (shebang, `brain_client`) | подкоманды `search`/`get`/`stats`/`audit`, `--json`, `--root facts|info`, `--as-of`, `-n`, `--no-web`, `--base URL`, `--token T`; base по умолчанию из `internal/config` (host/port), иначе `127.0.0.1:8630` |

Клиент ходит только через контракт (HTTP :8630, `bin/brain/serve.go`) — в
коде клиента нет ни одного открытия kb.lbug, парсинга Ladybug-файла или
запросов к внутренней схеме БД. Пакет cgo-free: тесты на httptest-фикстурах
входят в обычный `go test ./...`.

```bash
# CLI-примеры
./bin/brain/client.go search "onlyoffice postgres" --root facts --json
./bin/brain/client.go get <id> --body
./bin/brain/client.go stats
./bin/brain/client.go audit
```

SDK-пример (агент/скрипт/сервис на Go):

```go
import "github.com/eSlider/2dph/pkg/brainclient"

cl := brainclient.New(brainclient.Config{Base: "http://127.0.0.1:8630"})
facts, err := cl.Facts(ctx, "where is the lexicon", brainclient.SearchOptions{})
for _, f := range facts.Confirmed { /* безопасно цитировать как факт */ }
for _, nf := range facts.NotConfirmed { /* (not confirmed): только с пометкой */ }
```

### Гейт facts

Сервер отдаёт листья как они лежат в БД; за то, что **не подтверждённое не
выдаётся за факт**, отвечает клиентский гейт (`pkg/brainclient`):

- `Gate(root, confidence)` — вердикт для одного хита: `true` только для
  `root=facts` с `confidence=confirmed` (пустое = legacy, по семантике выше).
  `info`, `hypothesis`/`partial`/`estimated`/`inferred` → `false`.
- `Client.Facts` = `search --root facts` + гейт: ответ разделяется на
  `confirmed[]` (подтверждённые факты) и `not_confirmed[]` (отклонённое, с
  полем `reason` — пометка `(not confirmed)`). 2v2-противоречия (D16) лежат
  как `hypothesis` и в `confirmed` не попадают никогда.
- `GateAudit` — гейт поверх `audit`-гистограммы: `confirmed_facts` vs
  `not_confirmed_facts` на facts-корне. `not_confirmed_facts > 0` значит: на
  facts-корне есть hypothesis/partial — такие листы нельзя подавать как
  факты, пока их не разберёт `bin/facts/audit.go contradict` / audit-card
  (L-9, #229–234). CLI `audit` в этом случае выходит с кодом 1.
- Гейт — оборона на чтении; запись по-прежнему требует ≥2 независимых
  источников (promote, `docs/brain/contract.md`). Клиент не «доверяет»
  серверу: несовместимый формат/версия — ошибка, отклонённое — помечается.

### Пример 1: search-факт через клиент

```bash
./bin/brain/client.go search "onlyoffice postgres" --root facts --json
```

```json
{
  "contract_version": "1.0",
  "query": "onlyoffice postgres",
  "root_filter": "facts",
  "count": 1,
  "confirmed": [
    {
      "id": "af77a292ba48f7f8be5c040c21e56dd5",
      "text": "container 'onlyoffice' is running and declared in docker-compose.yml",
      "root": "facts",
      "confidence": "confirmed",
      "score": 5.48,
      "snippet": "container 'onlyoffice' is running and declared in docker-compose.yml"
    }
  ]
}
```

Если на facts-корне встретится hypothesis/partial (например 2v2-лист до
аудита), он уйдёт в `not_confirmed[]` с `reason`, а не в `confirmed[]` —
агент не сможет процитировать его как факт. `get <id> --body` показывает
двухисточниковый `source` («docker ps x compose:docker-compose.yml») —
доказательство по write-контракту.

### Пример 2: audit-карточка (гигиена facts-корня)

```bash
./bin/brain/client.go audit
facts          confirmed        21
info           confirmed    105199
facts gate: confirmed=21 not_confirmed=0
```

`facts gate: not_confirmed=0` — facts-корень чист: все 21 факта confirmed.
Если гейт покажет `not_confirmed > 0`, это сигнал для L-9-карточки:
`bin/facts/audit.go contradict` (2v2-разбор) или `bin/facts/audit-card.go`
(вердикт в `var/audit/source-of-truth.yml`) — до разбора такие листы остаются
`(not confirmed)` и в ответах клиента фактами не считаются.

### Кросс-репо потребители (gator/cv)

`internal/contract` — внутренний пакет модуля, поэтому сегодня клиент
импортируют 2dph-инструменты и скиллы. Чтобы gator/cv импортировали
клиент как внешний go-модуль, нужен вынос типов read-контракта и клиента в
публичный `go-*` модуль — это решает P-9.6 (общий ADR модели
фактов/аудита/памяти, эпик #239); до него внешние потребители читают brain по
этому документу (форматы) и через HTTP/MCP/CLI без дублирования логики.

## Cross-ref

- Write-контракт: [`docs/brain/contract.md`](contract.md) (P-9.2/P-9.3,
  issues #235/#13) — Leaf, ContentHash, observed_at, адаптеры корпуса.
- Общий паттерн версионности / ADR: P-9.6, issue #16.
- Аудит-карточки Vinogradov: L-9, issues #229–234 — отдельный домен
  (`docs/facts/audit-recipes.md`, `bin/facts/audit.go`).
- Скилл `brain` (skills/brain) — как пользоваться поиском и добавлять корпус.
- HTTP/MCP поверхность: `pkg/httpapi` (OpenAPI `GET /openapi.json`).
