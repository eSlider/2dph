# Контракт записи в Ladybug (P-9.2 + P-9.3)

Единый контракт записи leaf в Ladybug для всех источников корпуса (mail,
git, chats, docs, facts). Формализация по образцу gator `contract.Record`
(G-8.0, issue #73) — см. также общий ADR по версионности P-9.6 (#16).

Реализация: пакет `internal/contract` (cgo-free) + адаптеры корпуса
`internal/corpus` (cgo-free) + write-путь `internal/brain` (колонки
`observed_at`, `external_id`; id = `ContentHash`).

## Поля leaf

| Поле | Обязательно | Смысл |
|------|-------------|-------|
| `source` | да | корпус-источник: `mail` / `git` / `chats` / `docs` / `facts` (P-9.3: имя корпуса, не evidence pointer) |
| `external_id` | да | устойчивый ref внутри корпуса: message id / commit sha / chat message id / rel-путь файла |
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

## Dedup (ContentHash) — id в БД

Dedup-ключ версии **и id записи в БД** — `ContentHash()` =
`sha256(source|external_id|kind|text)[:32]` (P-9.3: runtime id переведён с
`LeafID(text, source)[:24]` на контрактный `ContentHash` — единый ключ,
dedup-ключ == id). Текст перед хэшем нормализуется `contract.NormalizeText`
(ToValidUTF8 → LF → trim): один контент из разных путей (CRLF vs LF,
хвостовые пробелы) даёт один id. `observed_at` намеренно вне хэша — семантика
gator (G-8.0 #73): тот же контент, записанный позже, дедуплицируется к той же
версии (сохраняется первый `observed_at`); изменился контент → новый ключ.

## Версионирование

Одна версия = один id. Изменение контента (text) или identity (source /
external_id / kind) → новый `ContentHash` → новый id, старая версия остаётся в
графе. Повторная запись той же версии — MERGE (upsert), без дубликата.

`valid_from/valid_to` — валидность факта по дням (D24, `facts.NormalizeDay`),
используется в поиске `--as-of`. Это не версионирование записи: версия
определяется id/ContentHash, валидность — интервалом.

## Формат ref (external_id) — P-9.3

`source` = имя корпуса; `external_id` = устойчивый ref; `loc` = evidence
pointer (путь/репо) для FROM_FILE и аудита.

| Корпус | source | external_id | Пример loc |
|--------|--------|-------------|------------|
| mail | `mail` | content-address `sha256(NormalizeText(text))[:16]` — единая схема для live (OO numeric id) и legacy (sha256:16 dir), чтобы один message из обоих корпусов сливался по ContentHash (#5.2) | `/var/corpus/mail/<folder>/<id>/message.md` |
| git | `git` | commit sha (полный) | путь репозитория |
| chats | `chats` | frontmatter `id` (первый message id); нет id → content-address | `.../var/corpus/chats/md/<platform>/<chat>/messages.md` |
| docs | `docs` | относительный путь в репо (стабилен между машинами) | абсолютный путь файла |

Текст лифа = `Heading + "\n\n" + body` во всех корпусах (git переведён с
одинарного `\n` на двойной, #5.3) — единообразие для ContentHash.

## Адаптеры корпуса (P-9.3)

Каждый корпус — адаптер по контракту gator `Source` (`internal/contract`):

```go
type Source interface {
    Name() string                          // mail | git | chats | docs
    Stream(ctx context.Context, emit func(Leaf) error) error
}
```

Адаптеры в `internal/corpus` (cgo-free): `mail.go`, `git.go`, `chats.go`,
`docs.go`. Общий index-драйвер (`bin/brain/index.go`) собирает leafs через
`corpus.StreamAll` и пишет единым `brain.WriteCorpus([]contract.Leaf)`.
**Добавление/удаление корпуса = адаптер в `sources()` + `--rebuild`**
(детерминированные id = ContentHash → пересборка без ручной магии).
Флаги `--with-mail` / `--with-chats` / `--corpus` / `--git-root` включают
соответствующие адаптеры. facts остаются отдельным путём (не корпус, а
evidence gate) — семантика «facts = ≥2 независимых источника» не трогается.

Дубль git устранён: git-история пишется только git-адаптером
(`var/corpus/git/*.md` и `2dph__corpus__git__*` в docs-корпусе исключаются
docs-адаптером).

## Аудит соответствия

`bin/brain/audit-contract.go` — read-only аудит против живой kb.lbug
(открывает БД с `ReadOnly=true`, не держит файл, агрегаты одним проходом):

    ./bin/brain/audit-contract.go            # summary
    ./bin/brain/audit-contract.go --json     # полные агрегаты

Считает total, пропуски обязательных/рекомендуемых полей, распределение по
корпусам (группировка по полю `source` напрямую — P-9.3; на живой БД до
пересборки source ещё содержит старые evidence-указатели, такие строки
показываются как есть). Прогон на живой БД
(2026-09-01, 313817 leafs): `missing_external_id: 313817` (колонки ещё нет —
миграция применится при старте сервиса), `missing_valid_from: 10099`,
`missing_how/loc/kind: 4`, `observed_at: 0`.

## Известные расхождения (на момент P-9.3)

- Живая `var/kb.lbug` (313k leafs) ещё не пересобрана: старые id
  (`sha256(source\0text)[:24]`), старые source (evidence pointers), git-дубль.
  Пересборка — отдельный шаг (devops, после merge): `index --rebuild
  --with-mail --with-facts [--with-chats] [--git-root DIR]`, полный рехэш id.
- facts-путь (`factsFromJSON`) пишет напрямую `UpsertLeaf` без `FROM_FILE` —
  оставлено как есть, фиксируется контрактом как допустимый путь записи.

## Cross-ref

- G-8.0 контракт записи gator: `internal/contract` (gator), issue #73.
- Общий паттерн версионности / ADR: P-9.6, issue #16.
- Адаптеры корпуса + рехэш id: P-9.3, issue #13.
- Скилл `brain` (skills/brain) — как пользоваться поиском и как добавить источник.
