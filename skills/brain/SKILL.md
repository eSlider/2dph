---
name: brain
description: >-
  Deduction search over the 2dph brain (Ladybug graph: ops corpus, portfolio,
  ssh hosts) with bin/brain/search.go instead of reading files or grepping
  repos. Use whenever a question starts with "where is", "what runs on",
  "which file describes", "who is", "how is X done", before opening any
  documentation.
---

# brain — deduction over facts and info

One embedded Ladybug graph (`var/kb.lbug`, read-only when queried) holding two
roots:

- **facts** — assertions backed by ≥2 independent sources (docker ps × compose
  × ssh-config × docs), `confidence: confirmed`.
- **info** — descriptive/narrative leafs, searchable, never asserted.

Search = deduction: facts root first, info root second, `web-search` as the
second independent source when local roots cannot confirm. An answer is
`confirmed` only if it comes off the facts root; anything else is
`(not confirmed)`.

```bash
bin/brain/search.go "Matrix federation"                # pointers + snippets, YAML
bin/brain/search.go "onlyoffice postgres" --root facts # restrict to confirmed
bin/brain/search.go "where is cs-lexicon" --json | yq '.[].ref'
bin/brain/search.go "who works where" --as-of 2025-01-01  # D24 intervals
bin/brain/add.go --text T --root facts --source "a.md x b.md"
bin/brain/get.go <id> --body                           # full chunk only when needed
bin/brain/stats.go                                     # index health
bin/brain/eval.go                                      # recall@5 >= 0.95 gate (Go, via Zig CGO)
```

`--as-of YYYY-MM-DD` keeps leafs whose `valid_from`/`valid_to` cover that day
(empty interval = always; not D16 source staleness).

Schema of a written leaf (source/external_id/observed_at/kind, dedup by
ContentHash, versioning): see `docs/brain/contract.md` (P-9.2/P-9.3); audit
compliance with `bin/brain/audit-contract.go`.

## Read через контракт (клиент, P-9.5)

`bin/brain/*.go` выше читают kb.lbug напрямую (нужна локальная БД + cgo).
Для агентов/скриптов/gator/cv на любой машине с brain-сервисом (HTTP :8630,
`bin/brain/serve.go`) есть сервисный клиент read-контракта — **тот же JSON,
без kb.lbug и без cgo**:

```bash
bin/brain/client.go search "onlyoffice postgres" --root facts --json  # факты: гейт facts
bin/brain/client.go search "onlyoffice postgres"                      # facts → info, info помечены (not confirmed)
bin/brain/client.go get <id> --body                                   # полный текст + source (доказательства)
bin/brain/client.go stats --json
bin/brain/client.go audit                                             # гигиена facts-корня (exit 1 при hypothesis/partial)
bin/brain/client.go search "q" --root facts --as-of 2025-01-01        # D24 валидность
```

- SDK: `pkg/brainclient` (typed `Search`/`Get`/`Stats`/`Audit`/`Facts`,
  валидация ответов контрактом). CLI: `pkg/brainclient/cli` + shebang
  `bin/brain/client.go`. Форматы и гейт — `docs/brain/read-contract.md`
  (раздел «Клиентский слой (P-9.5)»).
- **Гейт facts**: `--root facts` / `Client.Facts` возвращают только
  `confidence=confirmed` (root=facts); всё остальное — `not_confirmed[]` с
  пометкой `(not confirmed)`. 2v2-противоречия (hypothesis, D16) никогда не
  выдаются как подтверждённые. `audit` с `not_confirmed_facts > 0` — код 1:
  разбор через `bin/facts/audit.go contradict` / `audit-card` (L-9).
- Не открывай kb.lbug из клиентского кода: read-контракт (форматы выше) —
  единственный путь чтения для потребителей.

## Corpus — what lives in the brain (#198/#199)

`info` holds the WHOLE corpus, never just one root. Current composition
(после полного rebuild 2026-09-02, P-9.3 content-address dedup:
307,431 стримнутых mail-листов схлопнулись в 105,027 уникальных, #248;
всего в БД 105,220 leafs — см. docs/brain/memory-adr.md §4.4):

- **mail** — BOTH corpora index into the brain: live `var/corpus/mail`
  (inbox 50 + PST) AND legacy `var/mail` (215k message.md: tb-andriy-profile,
  tb-backup-128g, tb-2010-zip, contacts_eml, defacto, gmail_*, inbox).
  `bin/brain/index.go --with-mail` включает mail-адаптер
  (`corpus.Mail`, `corpus.MailRoots`); dedup между корпусами — по ContentHash
  (external_id = content-address текста, P-9.3). Mail is only in the brain if
  the index ran with `--with-mail`.
- **git history** — `bin/brain/import-git.go --root <dir>` (git-адаптер, go-git).
- **chats** — telegram/linkedin/whatsapp messages.md (`--with-chats`,
  `var/corpus/chats/md`).
- **docs** — README/PLAN/AGENTS/docs/skills (default) + `--corpus` пути.

If a search misses mail that exists on disk: the brain was rebuilt WITHOUT
`--with-mail`, or `var/mail` was never indexed. Fix:
`bin/stack/sync.go --with-mail` (wave step `mail-index`) or
`bin/brain/index.go --skip --with-mail` (resume/append, de-duped, idempotent).
`bin/brain/stats.go` on the ops host must show the post-rebuild baseline:
**total 105,220 = info 105,199 (mail 105,027 + docs 172) + facts 21**
(ANN 105,220; после полного rebuild 2026-09-02). Anything much less means a
corpus is missing. The old rule «info ≥ ~200k» (и ориентир ~313k) сняты —
P-9.3 dedup схлопнул дубликаты mail, актуальная модель — docs/brain/memory-adr.md.

## Corpus sources (P-9.3) — каждый корпус = адаптер

`info` holds the whole corpus via four adapters (`internal/corpus`, cgo-free),
each implementing `contract.Source` (`Name()` + `Stream(ctx, emit(Leaf))`):

| Корпус | Адаптер | Что индексирует | source / external_id |
|--------|---------|-----------------|----------------------|
| docs | `corpus.Docs` | README/PLAN/AGENTS/docs/skills + `--corpus` пути (md + yaml) | `docs` / rel-путь |
| mail | `corpus.Mail` | live `var/corpus/mail` + legacy `var/mail` (`<id>/message.md`) | `mail` / content-address `sha256(text)[:16]` |
| chats | `corpus.Chats` | `var/corpus/chats/md/<platform>/<chat>/messages.md` | `chats` / frontmatter `id` |
| git | `corpus.Git` | история коммитов (go-git) | `git` / полный commit sha |

`bin/brain/index.go` — драйвер: `corpus.StreamAll` → `brain.WriteCorpus`
(единый writer, id = `contract.ContentHash()`[:32]). Флаги включают адаптеры:
`--with-mail`, `--with-chats [DIR]`, `--corpus DIR` (повторяемый), `--git-root DIR`.
`bin/brain/import-git.go --root DIR` — тонкая обёртка над git-адаптером
(волна `stack/sync.go`, шаг git-brain).

**Как добавить новый источник корпуса** (например `calendar`):
1. Новый файл адаптера в `internal/corpus/<name>.go`: struct с `Name() string`
   (имя корпуса — оно же `source` в лифах) и
   `Stream(ctx, emit func(contract.Leaf) error) error`; каждый emit должен
   проходить `Leaf.Validate()` (source/external_id/kind/text), текст —
   `Heading + "\n\n" + body`.
2. Юнит-тест рядом (`<name>_test.go`, фикстуры в t.TempDir, без lbug): форма
   лифа, external_id, дедуп по ContentHash.
3. Зарегистрировать адаптер в `sources()` драйвера `bin/brain/index.go`
   (+ флаг-переключатель при необходимости).
4. Пересборка: `bin/brain/index.go --rebuild <флаги>` — детерминированные id
   (ContentHash) → добавление/удаление корпуса без ручной магии.
5. `skill-sync push` + PR на обновлённый SKILL.md (см. skill-sync).

Дубль git устранён: git-история пишется только git-адаптером;
`var/corpus/git/*.md` и `2dph__corpus__git__*` docs-адаптером исключаются.

## Rules

- Search before you read. Never grep a repo for a concept the graph covers.
- `--root facts` returns only confirmed evidence-linked answers. Default shows
  facts first, then info leafs clearly marked `(not confirmed)`.
- If there is no facts hit, `bin/brain/search.go` consults SearXNG and adds a
  `web` block (kept apart from graph hits). `throttled` / `skipped` / `refused`
  are not evidence of absence. `--root facts|info` and `--no-web` skip the web.
- If recall looks wrong, run `bin/brain/eval.go`; it gates control questions and
  should stay at or above 95% recall@5.
- Contradictions (≥2 yes vs ≥2 no) stay `(not confirmed)` until
  `bin/facts/audit.go contradict` fires `temporal_freshness` or `authority_pairing`.
- Agents: `GET /openapi.json` and `POST /mcp` on `bin/brain/serve.go` (same
  handlers; tool names match paths `search`/`get`/`stats`/`audit`). Generated
  list: [tools.md](tools.md).
- Never report an unconfirmed single-source local answer as fact.