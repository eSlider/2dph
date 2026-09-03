# Job pipeline truth-gate — audit recipes (#52)

`2dph` is the inspector/auditor for the job pipeline and the `cv/ai-bot` agent
workflows. It never writes job-pipeline state; it filters, searches, and audits.
Only `root=facts` holds confirmed claims.

## Status vocabulary

| State | Rule |
|-------|------|
| `confirmed` | `source` contains two independent sources joined by ` x ` (e.g. `crm.md x contract.md`). |
| `(not confirmed)` / hypothesis | contradiction `a x b vs c x d` where both sides have ≥2 sources but no rule fires (authority pairing / temporal freshness, D16). Stays hypothesis until audited. |
| stale | `valid_to` passed, or the fact is missing from the view for today's `--as-of` (D24 intervals). |

## Operator flow

Every query is `search → get → audit`:

```bash
bin/brain/search.go "dossier title or claim" --json -n 20          # CLI
bin/brain/search.go "who works where" --as-of 2025-01-01 --json    # D24 intervals
bin/brain/get.go <id> --body                                        # full text + source
bin/facts/audit.go self                                             # repo lexicon (Go, no deps)
bin/facts/audit.go db                                               # every root=facts leaf (two-source rule)
bin/facts/audit.go contradict                                       # adjudicate stdin claim(s)
bin/facts/audit.go formal                                           # L-9.3 URL-формальные проверки (stdin JSON)
bin/facts/audit-card.go --claim "…" --premises FACT-… --verdict …   # audit card → SoT audits[]
```

HTTP (`bin/brain/serve.go`, :8630): `/search?q=&as_of=&n=`, `/get?id=&body=1`,
`/stats`, `/audit`, `/ingest`, `/openapi.json`, `/mcp`.

Read-only Postgres (OnlyOffice, via SSH tunnel when the VM is up):

```bash
scripts/db/ssh-tunnel                       # 127.0.0.1:5433 -> vm:5432
scripts/db/psql-yq --profile onlyoffice -s doc_changes     # columns
scripts/db/psql-yq --profile onlyoffice -c 'SELECT ...'    # read-only query -> YAML
```

Profile lives in `~/.config/brain/db-profiles.yml` (0600, secrets never in the
repo). Passwords come from `~/.config/ops/onlyoffice.env`, bootstrapped from the
VM's `/etc/onlyoffice/documentserver/local.json` (dbUser/dbPass). The profile
uses `network: host` so the docker psql client shares the host loopback and can
reach the SSH tunnel on `127.0.0.1:5433`. (#53)

## Audit cards (L-9.2 #231)

Каждый вердикт любого рецепта ниже фиксируется карточкой Vinogradov в SoT
`audits[]` (`var/audit/source-of-truth.yml`), а не «сам по себе»: рекомендация
без card = нарушение. Карточка обязана опираться на FACT-/OPEN- premises.

```bash
# premises/gaps: флаг повторяемый или значения через запятую.
# inference: deduction | induction | analogy | other; gaps опциональны.
# counter: none или контр-аргумент; verdict: только accept | reject | weaken.
bin/facts/audit-card.go \
  --claim "demo2 подтверждён вторым источником" \
  --premises FACT-9002,FACT-9001 \
  --inference deduction \
  --gaps OPEN-0001 \
  --counter none \
  --verdict weaken
```

CLI валидирует карточку (verdict/inference enum, claim/premises/counter
обязательны), присваивает следующий `AUD-NNNN`, дописывает в `audits[]` и
печатает карточку. Остальные секции SoT и комментарии не трогаются
(правка на yaml-дереве). Формальная логика inference — L-9.3 (#232).

## Формальные проверки URL (L-9.3 #232)

Машинные проверки законов Vinogradov на уровне URL — поверх эмпирики #56.
Ядро — `internal/facts` (cgo-free, `CanonicalURL` + `CheckFormal`):
суждение = факт о ресурсе URL: `{id, url, claim, attr, neg, conf, from, to}`,
где `attr` — предикат, `neg` — знак (P/¬P), `conf` — подтверждение leaf'а.
Правила:

| Закон | Код-правило | Флаг |
|-------|-------------|------|
| identity (тождество) | один канонический URL в разных написаниях → merge/flag | `weaken` |
| contradiction (противоречие) | подтверждённые `P` и `¬P` об одном предикате одного URL с пересекающимися D24-интервалами | `reject` |
| excluded middle (исключённого третьего) | hypothesis/partial или claim без предиката — не FACT, кандидат в `open_questions[]` | `weaken` |
| sufficient reason (достаточного основания) | verdict `accept` только с FACT-/OPEN- premises; иначе — reject/weaken | `reject` |

Пример — два факта на один URL с противоречивыми суждениями («вакансия
активна» vs «вакансия closed»):

```bash
cat <<'EOF' | bin/facts/audit.go formal
{"facts":[
  {"id":"FACT-01","url":"https://jobs.example.com/vacancy/42","claim":"вакансия активна","attr":"вакансия открыта","neg":false,"conf":"confirmed"},
  {"id":"FACT-02","url":"https://jobs.example.com/vacancy/42","claim":"вакансия closed","attr":"вакансия открыта","neg":true,"conf":"confirmed"}
]}
EOF
# → contradiction verdict=reject; эквивалентная карточка пишется audit-card.go
```

Канонизация URL — локальная реализация конвенции gator G-8.1
(`facts.CanonicalURL`: lowercase scheme/host, default-port, utm_*/ref/gh_jid/
token-query, сортировка query, без фрагмента/trailing slash); gator-модуль не
импортируется. Проверка sufficient-reason по карточкам идёт тем же
подкомандой: `echo '{"cards":[{…}]}' | bin/facts/audit.go formal` — accept без
FACT-/OPEN- premises возвращается как нарушение (reject/weaken).

## Recipes

### 0. Git-commits: «кто над чем работал, когда» (L-9.4 #233)

Граф-слой Commit/Person/AUTHORED (пишет `bin/git/graph.go` из git-истории
репо git.produktor.io, канон commit id = `repo:sha`) → дедуктивные факты
`bin/git/facts.go` (read-only). Дедукция Vinogradov: посылка — commit-лист
(`commit <sha> in <repo>`, `Author`, `Date`, subject/files) → вывод «<автор>
работал над <repo> в <date>».

```bash
bin/git/graph.go --repo /x/2dph --dry-run        # сколько коммитов/Person до записи
bin/git/graph.go --repo /x/2dph --commit         # MERGE в kb.lbug (идемпотентно)
bin/git/facts.go --repo 2dph --since 2026-01-01  # факт-карточки
bin/git/facts.go --author a@b.c --commits --json # детально + audit cards
```

**Шаблон audit card (commit-факт):** claim из repo+периода фактических дат
коммитов; premises — commit id (`repo:sha`, первые 5 + счётчик остатка);
inference = `deduction`; verdict:

- `accept` — subject-ы содержательные («feat: mesh node», «fix: typo») → «что
  сделал» как гипотеза из message/files;
- `weaken` — весь subject-набор пустой/служебный («Update», WIP) → выводится
  только «трогал файлы в <repo>», не «что именно»; gap `OPEN`;
- частично слабый набор → `accept` + gap `OPEN` про число слабых коммитов.

**Границы (иначе fallacy):**
- «что сделал» — гипотеза из message/files, НЕ факт о намерении;
- авторство по `Author:` коммита, не обязательно единственный автор: merge-
  коммиты в граф не пишутся (gitlog.Log пропускает), co-author в теле
  сообщения невидим — OPEN;
- 1 коммит ≠ «вся неделя на X»: период берётся от фактических дат коммитов,
  поспешная индукция не строится;
- Leaf-коммиты (premises, source=git) не раздуваются — граф пишется
  отдельным слоем Commit/Person/AUTHORED.

### 0a. Сеть связей «кто с кем и через кого» (L-9.5 #234)

Граф mail (Message/Person + SENT/TO/CC/BCC/REPLY_TO, D-1 #257) + git
(Commit/Person/AUTHORED, L-9.4 #233) → связи Person↔Person с premises и
verdict; read-only инструмент `bin/network/network.go`.

```bash
bin/network/network.go --person eslider@gmail.com                # топ связей
bin/network/network.go --person eslider@gmail.com --json         # полный JSON
bin/network/network.go --person alice@x --project demo --since 2026-01-01
bin/network/network.go --person alice@x --accept-only            # CRM-экспорт (YAML)
```

**Деривации** (только из существующих узлов/рёбер): письмо sender↔recipient
(вес TO 1.0/CC 0.5/BCC 0.25), общий получатель, REPLY_TO-диалог, треды
(thread_id), общий проект (AUTHORED Commit.repo). Premises — Message.id /
Commit.id `repo:sha`.

**Шаблон audit card (link-факт):** claim «Q связан(а) с target: N писем, M
тредов, период D»; inference = `deduction`; verdict:

- `accept` — ≥2 прямых писем, или ≥1 REPLY_TO-диалог, или общий проект
  (обе стороны AUTHORED в repo) → экспорт в CRM (ADR-0012 п.4);
- `weaken` — одиночный контакт (1 письмо без ответа и без проекта) или
  только общий получатель → gap `OPEN`, в CRM-экспорт не идёт (ADR-0012
  п.2 «1 письмо ≠ устойчивая связь»).

**Границы (иначе fallacy):** алиасы email не склеиваются (сопряжение строго
по email); «что обсуждали» по телу письма не выводится (Paragraph не
материализован); транзитивные цепочки «знакомые знакомых» — вне пилота
(depth=1); mail↔repo привязка не выдумывается (в графе нет такого ребра).
Дизайн: docs/brain/graph-network.md.

### 1. Dossier URL dedupe

```bash
bin/brain/search.go "<dossier company/title>" --json -n 100 | yq '.[].ref'
bin/brain/get.go <id> --body
```

Pull the leafs, collect `loc` (artifact path) + `source` (ref list), group by
URL/dossier. Flag: same URL in more than one leaf, or a dossier re-ingested under
different ids (same `source`, different `loc`). Duplicates are `(not confirmed)`
until the operator collapses them into one two-source fact.

### 2. Source conflicts

```bash
bin/brain/search.go --root facts "<subject>" --json -n 50
echo '{"claim":"X works at Y","left":"a x b","right":"c x d"}' | bin/facts/audit.go contradict
```

For one subject, compare `source` lists. If two facts assert the same claim with
disjoint refs, or one contradicts the other, run `audit contradict`. A 2v2 with
no authority/temporal rule stays `(not confirmed)`. Cross-check against the
OnlyOffice CRM graph and the corpus SoT (`bin/facts/prove-crm --mismatches`).
`bin/facts/prove-crm.go` proves person→company and company→project associations
as `root=facts` only when BOTH sources agree (corpus org × ooCRM graph); every
one-sided association is reported as a mismatch, never a fact. The merge rule
lives in `internal/facts.CRMAssocFacts` (single implementation, covered by
offline unit tests).

### 3. Stale status

```bash
bin/brain/search.go "<subject>" --as-of "$(date +%F)" --json -n 50
bin/facts/audit.go db
```

Facts carry `valid_from`/`valid_to` (D24). Anything absent from today's
`--as-of` view or with `valid_to` in the past is stale. `audit db` additionally
flags facts that lost their two-source form.

### 4. Correspondence provenance

Every claim's `source` must resolve to an artifact the brain actually holds:

- mail: `var/corpus/mail/md` (M365 sync → `bin/mail/sync.go` → `brain/index --with-mail`)
- telegram/n chat: `var/corpus/chats` (`bin/chat/sync.go n|linkedin`)
- corpus: `cv/`, `projects/knowledge-mesh-seed.yaml`

```bash
bin/brain/get.go <id> --body     # source + loc
test -f "$KB_ROOT/<loc>"         # artifact exists on disk
```

Claims whose source points at a missing artifact or a chat thread that does not
support the claim are downgraded to `(not confirmed)` pending re-audit.

## Test plan (on one known deal)

Executable, offline proof in `internal/brain/rank/deal_flow_test.go` (issue #58):

1. `search` → `get` → `audit` on one synthetic deal (`acme-2026`, fixture
   Alice/Bob/example.com, no PII): `search` fuses FTS+vector and surfaces the
   confirmed facts + info leafs; `get` resolves a leaf by id and exposes its
   two-source `source`; `audit` (`facts.CheckFactRow` + `facts.Adjudicate`)
   confirms the two-source facts. `TestDealSearchGetAuditConfirmedFlow`.
2. Contradiction path: the fixture's `a x b vs c x d` leaf (2 yes × 2 no, no
   authority/temporal rule fires) stays `(not confirmed)` until audited — never
   reported as a confirmed fact, deduction escalates to the second source.
   `TestDealContradictionNotConfirmedUntilAudited`.
3. Operator flow is the same for the `cv/ai-bot` assistant: the MCP/HTTP tools
   (`/search`, `/get`, `/audit`, `/stats`) expose exactly this
   `search → get → audit` contract; an operator or agent reads `confirmed` only
   from `root=facts` hits, treats `hypothesis`/`partial` as `(not confirmed)`,
   and runs `audit contradict` to resolve a 2v2 claim (D16). Docs live here.

The DB-backed `get` step (ladybug `lookupLeaf`/`HTTP.Get`) is exercised live
against `var/kb.lbug`; the offline test stands a fixture leaf-store in for that
fetch and runs the real cgo-free adjudication/lexicon code.

```bash
go test -race ./internal/brain/rank/ -run 'TestDeal'
```