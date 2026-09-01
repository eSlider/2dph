# Контракт записи в Ladybug (P-9.2)

Единый контракт записи leaf в Ladybug для всех источников корпуса (mail,
git, chats, docs, facts). Формализация по образцу gator `contract.Record`
(G-8.0, issue #73) — см. также общий ADR по версионности P-9.6 (#16).

Реализация: пакет `internal/contract` (cgo-free) + write-путь
`internal/brain` (колонки `observed_at`, `external_id`).

## Поля leaf

| Поле | Обязательно | Смысл |
|------|-------------|-------|
| `source` | да | корпус-источник: `mail` / `git` / `chats` / `docs` / `facts` (см. P-9.3: сейчас в runtime ещё смешан с ref) |
| `external_id` | да | устойчивый ref внутри корпуса: message id / commit sha (+ path) / chat message id / путь файла |
| `kind` | да | тип leaf: `fact` / `mail` / `chat` / `commit` / `reference` / `seed` / … |
| `text` | да | содержимое leaf |
| `root` | нет | `facts` (утверждение, ≥2 источника) или `info` (нарратив); default `info` |
| `confidence` | нет | `confirmed` / `estimated` / `inferred` / …; default `confirmed` |
| `source_rev` | нет | ревизия источника: `working-tree` / sha; default `working-tree` |
| `how` | нет | как получено: `kb/index`, `mail/import`, `git-log`, `facts/extract`, `brain/add` |
| `loc` | нет | локальный контекст (путь/репо); default = `source` |
| `valid_from` / `valid_to` | нет | D24-день валидности (YYYY-MM-DD) — **ортогонально версии**: это про факт, а не про запись |
| `observed_at` | нет | когда контент **увиден** (RFC3339); пусто → штамп `now()` при записи; **не входит в dedup-ключ** |

Проверка обязательных полей — `Leaf.Validate()`; невалидный leaf режется на
границе записи (до upsert).

## Dedup (ContentHash)

Dedup-ключ версии — `ContentHash()` = `sha256(source|external_id|kind|text)[:32]`.
`observed_at` намеренно вне хэша — семантика gator (G-8.0 #73): тот же контент,
записанный позже, дедуплицируется к той же версии (сохраняется первый
`observed_at`); изменился контент → новый ключ.

> Связь с runtime: текущий runtime id = `LeafID(text, source)` =
> `sha256(source\0text)[:24]` (`internal/brain/corpus_pool.go`). Это **другой**
> ключ, чем контрактный `ContentHash`. Переход на `sha256(source|external_id|kind|text)`
> — задача P-9.3 (рехэш id + миграция), в P-9.2 runtime id не меняется.

## Версионирование

Одна версия = один id. Изменение контента (text) или identity (source /
external_id / kind) → новый `ContentHash` → новый id, старая версия остаётся в
графе. Повторная запись той же версии — MERGE (upsert), без дубликата.

`valid_from/valid_to` — валидность факта по дням (D24, `facts.NormalizeDay`),
используется в поиске `--as-of`. Это не версионирование записи: версия
определяется id/ContentHash, валидность — интервалом.

## Формат ref (external_id)

| Корпус | external_id | Пример |
|--------|-------------|--------|
| mail | message id (директория письма) | `ooMail:<id>:<file>` в `source`, `<id>` в `external_id` |
| git | commit sha (полный) | `repo@<sha>` в `source`, `<sha>` в `external_id` |
| chats | chat message id | TBD при импорте чатов |
| docs | путь файла | относительный путь в репо |

## Аудит соответствия

`bin/brain/audit-contract.go` — read-only аудит против живой kb.lbug
(открывает БД с `ReadOnly=true`, не держит файл, агрегаты одним проходом):

    ./bin/brain/audit-contract.go            # summary
    ./bin/brain/audit-contract.go --json     # полные агрегаты

Считает total, пропуски обязательных/рекомендуемых полей, распределение по
корпусам (эвристика по сигнатуре source — до P-9.3). Прогон на живой БД
(2026-09-01, 313817 leafs): `missing_external_id: 313817` (колонки ещё нет —
миграция применится при старте сервиса), `missing_valid_from: 10099`,
`missing_how/loc/kind: 4`, `observed_at: 0`.

## Известные расхождения (на момент P-9.2)

- `source` в runtime — evidence pointer (путь / `ooMail:id:file` / `repo@sha`),
  корпус не выделен в отдельное поле → миграция в P-9.3.
- git-лифы частично попали в docs-корпус (двойное индексирование, #10).
- facts-путь (`factsFromJSON`) пишет напрямую `UpsertLeaf` без `FROM_FILE` —
  оставлено как есть, фиксируется контрактом как допустимый путь записи.

## Cross-ref

- G-8.0 контракт записи gator: `internal/contract` (gator), issue #73.
- Общий паттерн версионности / ADR: P-9.6, issue #16.
- Миграция source=корпус + рехэш id: P-9.3.
- Скилл `brain` (skills/brain) — как пользоваться поиском.
