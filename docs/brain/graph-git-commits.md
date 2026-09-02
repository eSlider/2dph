# Graph: git-commits → 2dph Commit/Person/AUTHORED (L-9.4 #233)

Дизайн и реализация граф-заполнителя Commit/Person/AUTHORED из git-истории
репозиториев git.produktor.io (eSlider/*, produktor/*). Цель эпика L-9 (#229) —
дедукция «кто над чем работал, когда, что сделал»: источник фактов для аудита
и ось «проект/время» сети связей (L-9.5 #234, ADR-0012).

## 1. Место в графе: два слоя

| Слой | Что | Путь | Узел |
|------|-----|------|------|
| premises | git-история как Leaf-текст (kind=commit, source=git) | `internal/corpus/git.go` → `gitlog.Log`/`ToLeaf` → `bin/brain/import-git.go` | `Leaf` |
| граф | дедуктивный слой: Commit/Person/AUTHORED | `internal/gitgraph` → `bin/git/graph.go` → `brain.UpsertCommits` | `Commit`, `Person`, `AUTHORED` |

Leaf и Commit-узел — разные граф-слои (ADR-0012): git — premises для
дедукции; Commit-узел — результат. Дубль-корпус (`var/corpus/git/*.md` +
`2dph__corpus__git__*`) устранён (#10/P-9.3), тексты старых тел #233 про него
устарели.

## 2. Источник git-фактов

- **Репозитории**: локальные клоны git.produktor.io. Скан `--root DIR` —
  одноуровневый по каталогам с `.git` (алгоритм `corpus.Git.resolveRepos`),
  либо явный `--repo path[,path]`. Имя репозитория = `gitlog.RepoName`
  (base имени origin-remote; без origin — имя каталога): стабильно при
  переезде клона.
- **Чтение истории**: `gitlog.Log` (go-git, без git-бинара) — тот же канон,
  что premises-корпус: **merge-коммиты пропускаются**, `Subject` = первая
  строка message, автор = `Author` (не committer), дата = `Author.When`
  RFC3339. Дублирующего чтения нет — коннектор реиспользует gitlog.
- **Канон git-коммита**: уникальность по паре `(repo, sha)` — полный sha
  уникален в репозитории; одинаковый sha в разных репо (форк/копия) —
  разные узлы. `Commit.id = <repo>:<sha>` (формат FileID `repo:path`).
  Неизменяемая история остаётся в git-репах; gator коммиты не выносит (P-9).

## 3. Маппинг в схему

`gitlog.Commit` → `brain.CommitInput` (`internal/gitgraph.ToInputs`):

| gitlog.Commit | CommitInput / узел | заметка |
|---|---|---|
| sha | `Commit.id = repo:sha` | канон |
| repo (RepoName) | `Commit.repo` | как leaf.Repo git-корпуса |
| subject (first line) | `Commit.subject` | «что сделал» — гипотеза из message |
| author name | `Commit.author` | display name |
| author email | `Commit.email`, `Person.id` | **lowercase** — Person.id канон mail (D-1 #257): тот же email в mail и git = один Person |
| author date | `Commit.date` | RFC3339 |

Person без display name в mail-графе получает name из git-автора (name-мердж
«последний sync побеждает», SET в MERGE). Коммит без email автора остаётся
Commit-узлом без AUTHORED (сопряжение по email невозможно).

Рёбра: `(:Commit)-[:AUTHORED]->(:Person)`. Рёбер Commit→File/Leaf нет
(YAGNI, тело #233): область проекта на этом этапе = `Commit.repo`; Project-
таблица не заводится (выводится из repo/файлов — решает дизайн #234).
Сортировка записи — по дате ASC (родители раньше; flat commits — рёбер между
коммитами нет), tie-break по id.

## 4. Write-путь

- `internal/brain/commitplan.go` (cgo-free): `CommitInput`, SQL-формы
  `commitUpsertQuery` (MERGE Commit по id + SET свойств),
  `authoredEdgeQuery` (MATCH Commit+Person + MERGE AUTHORED),
  `commitAuthor` (Person.id = email lowercase, name = author).
- `internal/brain/gitcommit.go` (cgo): `UpsertCommit`/`UpsertCommits` — пачка
  одной транзакцией (образец `UpsertMessages` D-1.2 #259). Идемпотентность —
  MERGE по id (repo:sha / email): повторный прогон 0 дублей. `InitSchema`
  (write.go:130-133) уже создаёт Commit/Person/AUTHORED — схема не меняется.
- CLI `bin/git/graph.go` (образец `bin/mail/graph.go` D-1.3 #260): dry-run по
  умолчанию (`--commit` пишет), `--repo`/`--root`, `--since`, `--limit`,
  `--db`, `--skip-existing` (инкремент по имеющимся Commit.id), live-holder
  guard (`--force`), `--json`, батчи 500. Отчёт: dry-run — commits per repo /
  unique emails / weak subjects; commit — Commit/Person/AUTHORED before→after.

## 5. Дедукция (инструмент git-facts)

`bin/git/facts.go` (read-only, RO-open параллельно живому сервису): читает
`(:Commit)-[:AUTHORED]->(:Person)` тройки и выдаёт факт-карточки
«кто работал над `<repo>` в `<period>`» (внутренняя логика
`internal/gitgraph.GroupFacts`/`BuildFacts`).

Факт-карточка (Vinogradov, audit card):

```
repo=2dph person=Ada Lovelace <ada@example.com>
  claim: «Ada Lovelace» работал(а) над 2dph в 2026-08-10..2026-08-12 (3 коммитов)
  gap: OPEN: 1 из 3 коммитов со служебным message — их «что именно» не выводится
  inference: deduction; verdict: accept
```

Границы дедукции (иначе fallacy, docs/audit-recipes.md):
- «что сделал» — гипотеза из subject коммита, НЕ факт о намерении;
- слабый/пустой subject («Update», WIP) → только «трогал файлы», карточка
  weaken + gap OPEN (весь subject-набор слабый → verdict weaken);
- авторство по Author; merge-коммиты не импортируются (gitlog.Log),
  co-author в теле сообщения невидим — ограничение (OPEN);
- 1 коммит ≠ «вся неделя на X» — поспешная индукция не строится (период —
  от фактических дат коммитов).

## 6. Сопряжение с mail-графом

Person mail-графа (D-1 #257, id=email lowercase, ~3 064 узла) и git-авторы
с тем же email сливаются в один Person-узел (MERGE по id): git-коммит автора
получает AUTHORED к уже существующему Person из mail. Незнакомый email —
новый Person (name из git-автора). Итог: один Person-узел на email, рёбра
SENT/TO/CC/BCC/REPLY_TO (mail) + AUTHORED (git) сосуществуют.

## 7. Тесты

- юнит cgo-free (`internal/brain/commitplan_test.go`): формы MERGE-запросов,
  деривация автора (email lowercase), no-email;
- юнит cgo-free (`internal/gitgraph`): канон repo:sha, маппинг
  gitlog.Commit → CommitInput, dedup/сортировка (идемпотентность повтора),
  Resolve (root-скан/явные), ReadAll (go-git fixture), WeakSubject, Stats,
  GroupFacts/BuildFacts (фильтры, вердикты accept/weaken+OPEN, premises);
- cgo на живой Ladybug (`internal/brain/gitcommit_test.go`, temp DB): запись
  Commit/Person/AUTHORED и перечитывание, повторный прогон = 0 дублей,
  сопряжение mail↔git Person (SENT + AUTHORED на одном узле), no-email,
  сосуществование с Leaf/Message + EnsureIndexes.

## 8. Сеть связей (L-9.5 #234) — сделано

Сетевой слой поверх графа (mail D-1 #257 + git L-9.4 #233): инструмент
`bin/network/network.go` «кто с кем и через кого» — Person↔Person по общим
письмам/тредам (SENT/TO/CC/BCC/REPLY_TO) и общим проектам (AUTHORED
Commit.repo — ось «проект/время»), premises (Message.id/Commit.id), verdict
accept/weaken, экспорт accept в CRM (ADR-0012 п.4). Дизайн и границы:
[docs/brain/graph-network.md](graph-network.md). Осталось после #234 (не
скоуп): группировка файлов → проектная область (сейчас область = repo),
алиасы email, транзитивные цепочки depth>1.

## Ссылки

- Epic L-9 #229, тело L-9.4 #233 (дедуктивный паттерн, границы).
- D-1 #257 (mail-граф, Person.id=email), D-1.2 #259 (write-путь/InitSchema),
  D-1.3 #260 (CLI-импортёр, образец).
- ADR-0012 (§сеть связей), ADR-0013 (база графа); docs/facts/audit-recipes.md
  (шаблон audit card).
