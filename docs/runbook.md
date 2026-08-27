---
type: howto
status: current
related:
  - docs/README.md
  - PLAN.md
---

# Run 2dph (portable)

No laptop-absolute paths. Config lives in env files under `$HOME/.config/brain/`
(mode 0600), not in git.

## Toolchain

- Go (see `go.mod`)
- Optional: Docker, Zig CGO via `bin/cgo/zig` (not gcc)
- Optional: poppler (`pdftotext`/`pdftoppm`) + tesseract `eng+deu` for mail OCR
- Optional: ghostscript `gs` — normalizes export-locked / oversized PDFs before
  extraction (strips export-protection, shrinks; original preserved, artifact
  in `var/tmp`). Clean PDFs skip it, so the pdftotext fast path stays fast.

```bash
eval "$(bin/cgo/zig env)"   # optional; bin/brain/{search,get,stats,eval,serve}.go auto-wrap zig
go test ./...
```

## Config

| File / env | Purpose |
|------------|---------|
| `$BRAIN_SEARCH_ENV` (default `$HOME/.config/brain/search.env`) | `BRAIN_SEARCH_URL` (SearXNG). Optional Basic Auth. |
| `$HOME/.config/brain/db-profiles.yml` | read-only Postgres profiles (OnlyOffice via tunnel) |

If the host already runs SearXNG, point `BRAIN_SEARCH_URL` at it. Do not start
a second copy (D3). Optional Compose instance:

```bash
SEARXNG_SECRET=$(openssl rand -hex 32) docker compose --profile searxng up -d
```

That binds `127.0.0.1:8888`. JSON format must stay enabled.

## Index then search

Write path is `bin/brain/add.go` for a leaf (or `POST /ingest`). Bulk
corpus rebuild remains `bin/brain/index.go --rebuild` (Compose profile
`index`). Do not DROP INDEX on Ladybug 0.19.

```bash
bin/brain/add.go --text "arc-1 runs Matrix" --root facts --source "compose.yml x docker ps"
bin/brain/index.go --rebuild --with-facts --with-chats
bin/brain/search.go "LadybugDB vector index"     # facts → info → web (D17)
bin/brain/search.go "upstream flag" --no-web
bin/brain/search.go "who works where" --as-of 2025-01-01  # D24 intervals
source <(./bin/shell/complete.go bash)            # D23 flaggy complete
bin/brain/get.go <id> --body
bin/brain/stats.go
```

`--hop N` walks File → Commit → Person from each hit. `--as-of YYYY-MM-DD`
keeps leafs whose `valid_from`/`valid_to` cover that day (empty interval =
legacy always-on). Empty web results are `throttled`, not absence.
Gap to v1: [roadmap](roadmap.md) / [epic #16](https://git.produktor.io/eSlider/2dph/issues/16).

Ladybug 0.19: never `DROP INDEX` FTS/VECTOR (ghost catalog). Fresh indexes =
delete `var/kb.lbug` then `--rebuild`.

## HTTP / MCP

```bash
scripts/stack/start                 # brain :8630, wait until MCP search/get/audit
scripts/stack/status                # YAML: brain / reasoner / picoclaw / mail_sync
scripts/stack/start-mail-sync       # compose ETL: OO+Gmail sync→import (300s; no auto-rebuild)
scripts/stack/start-assistant       # + qwen3.5:9b + PicoClaw agent (ask the brain)
scripts/stack/start-assistant --no-attach
scripts/stack/stop                  # compose stop; volumes kept
```

Same Compose services by hand:

```bash
docker compose up -d brain                       # :8630 Zig CGO serve
docker compose up -d mail-sync                   # ETL loop into kb-var
docker compose --profile index run --rm index    # rebuild
docker compose --profile picoclaw up brain-mcp   # MCP 127.0.0.1:8630
```

`GET /openapi.json`, `POST /mcp`. Agent tool order: `search` → `get` → `audit`.
See [picoclaw.md](picoclaw.md).

### Brain health-check & restart (#180)

The brain wedged once (2026-08-24): a `/ingest` write blocked forever inside
the Ladybug C layer, holding the read/write lock, so every search/get/audit
hung while `/health` still answered `ok` (it never touches the DB). The static
healthcheck let the container sit "healthy" but unresponsive until a manual
restart.

**Health-check** — probe the DB read path, not `/health`:

```bash
# fast (healthy): a real MATCH over kb.lbug
curl -sS --max-time 5 http://127.0.0.1:8630/stats   # -> {"total":9204,"by_root":{...}}
# live check of every agent tool
curl -sS --max-time 10 "http://127.0.0.1:8630/search?q=test&n=1&noweb=1"
```

`/stats` returns quickly only when the read handle is healthy; a wedged brain
times out. The compose `brain` healthcheck uses `/stats` for exactly this
reason (see `compose.yaml`), so docker auto-restarts a deadlocked container
(`restart: unless-stopped`) instead of leaving it "healthy".

**Restart**:

```bash
docker compose up -d brain          # or: docker restart 2dph-brain-1
# verify: process not a zombie, leaf count unchanged
ps -eo pid,ppid,stat,args | grep -E "brain-(serve|search)"   # no <defunct>
curl -sS --max-time 5 http://127.0.0.1:8630/stats            # total unchanged
```

Data lives in `var/kb.lbug` (host path, gitignored) and survives a container
restart unchanged — verify with `sha256sum var/kb.lbug` before/after. A stuck
write never commits, so the on-disk DB is the last successful state. A full
rebuild is only needed after a deliberate `--rebuild` or a corrupt DB, not
after a restart.

## Бенчмарки поиска (#202)

A/B-харнесс `bin/brain/bench.go` гоняет фиксированный golden-set запросов
(`internal/brain/testdata/golden-set.json`, ~50, рус+англ, темы
facts/mail/docs/git/ssh) через поиск и печатает latency p50/p95/mean,
recall@5/@10 и CPU/RSS. **Никакой кандидат не принимается без замера**
(эпик #201): baseline — текущий линейный скан.

```bash
# baseline против живого brain (HTTP MCP :8630, read-only; контейнер можно не трогать)
./bin/brain/bench.go

# machine-отчёт (для issue/CI)
./bin/brain/bench.go --json

# кандидат: исполняемый бинарь с CLI bin/brain/search.go --json -n N --no-web "q"
./bin/brain/bench.go --candidate /path/to/candidate

# кандидат: другой serve (:8631) — A/B recall vs baseline + latency ratio
./bin/brain/bench.go --candidate http://127.0.0.1:8631

# локально, без сервера: открыть var/kb.lbug read-only в процессе
# (сначала quiesce: docker compose stop brain — single-writer!)
./bin/brain/bench.go --inproc

# локально против отдельной копии БД (напр. свежий --rebuild --db /tmp/kb.lbug)
./bin/brain/bench.go --inproc --db /tmp/kb.lbug

# ресурсы сервера: семплировать PID brain-контейнера вместо собственного
./bin/brain/bench.go --pid $(docker inspect -f '{{.State.Pid}}' 2dph-brain-1)
```

Метрики и гейты:

| Метрика | Смысл | Гейт |
|---------|-------|------|
| latency p50/p95/mean, ms | распределение длительности поиска на запрос | кандидат p50 ≤ baseline p50 × 1.5 |
| recall@5/@10 (fragment) | доля запросов, у которых ожидаемый фрагмент (из корпуса) в top-k | baseline recall@5 ≥ 0.95 |
| recall@5/@10 (vs baseline) | кандидат не теряет известные baseline-хиты (top-k IDs) | кандидат ≥ 0.95 |
| CPU/RSS | user+sys jiffies, RSS до/после, пик (VmHWM) из /proc | для сравнения при «тех же ресурсах» |

Exit codes: `0` — гейты прошли, `2` — recall/ratio FAIL, `1` — ошибка
(нет golden-set, не поднялся searcher, и т.п.).

Чтение: p50/p95 по per-query latency при `--workers 1` (по умолчанию) —
чистый одиночный запрос; `--workers N` меряет сервер под нагрузкой.
Повторяемость: два прогона подряд, разброс p50 < 10 % (линейный скан
детерминирован; разброс даёт только фоновая нагрузка). Baseline-эталон:
p50 ≈ 32s на 313k leafs (линейный скан, #192); после внедрения кандидата
цель эпика p50 < 500ms, p95 < 2s.

В CI (GitHub, оффлайн): `--rebuild` корпуса → `bench --inproc --json`
(та же схема, что и recall-gate `bin/brain/eval.go`).

### ANN-индекс в проде (#204/#206)

Векторный поиск brain идёт через инкрементальный индекс **вне liblbug** (его
собственный HNSW крашится на росте графа, #192; fallback — линейный скан).
Алгоритм: **IVF** (k-means cells + точный косинус внутри probed-кластеров,
чистый Go, без внешних зависимостей). `coder/hnsw` (HNSW) отброшен на
эмпирике: на 313k реальных эмбеддингов из entry достижимы лишь 34% узлов
(изоляция при вытеснении соседей) → recall@5 падает до 0.16; IVF держит
recall@30 ≥ 0.93 при NList=2000/NProbe=128 (замеры probe #204). Индекс
живёт в `var/state/vector.ann` (+ append-only WAL `vector.ann.wal`).

**Включён по умолчанию** (#206): `vector.ann.enabled: true` — serve и CLI
ищут через ANN; при отсутствии/повреждении индекса поиск автоматически
падает на линейный скан (никогда не ломается). `serve` грузит индекс при
старте (warm start, без rebuild); если индекс пуст — serve отвечает через
скан, а волна (шаг `ann-build`) строит его.

```bash
# полный build из БД (~30s extract + ~4min k-means + assign на 313k, NList=2000)
KB_BUFFER_POOL=4294967296 ./bin/brain/ann.go build

# волна/ручной режим: build если индекса нет или он устарел (>10% отставания
# по счётчику leafs), иначе инкрементальный WAL-upsert новых leafs (<1s, НЕ rebuild)
./bin/brain/ann.go ensure

# инкрементальный upsert новых leafs (append WAL, НЕ rebuild) — для ручных прогонов
./bin/brain/ann.go upsert

# статистика индекса
./bin/brain/ann.go stats

# CLI-поиск через ANN (fallback на скан при отсутствии индекса)
./bin/brain/ann.go --json -n 10 "query"

# HTTP-сервер поиска через ANN (для bench --candidate http://127.0.0.1:8631)
./bin/brain/ann.go api --port 8631
```

**Инкрементальность** (критично: волна идёт каждые N минут, полный rebuild
на каждую волну НЕДОПУСТИМ): `ensure` сравнивает счётчик leafs в БД с
мощностью индекса; steady-state рост (+100 писем на 313k) всегда уходит в
WAL-upsert (<1s), полный build — только когда индекса нет или он отстал
>10% (битый/частичный/после bulk-import). Доказано тестом
`TestAnnEnsureIncrementalUpsertNoRebuild` (+100 leafs → rebuild=false,
upsert <1s) и замером на проде.

A/B (изолирует векторный слой: baseline = скан, кандидат = ANN, один
процесс, одна БД):

```bash
# quiesce сначала (single-writer): docker compose stop brain
KB_BUFFER_POOL=4294967296 ./bin/brain/bench.go --inproc --candidate inproc-ann
# эталон: ./bin/brain/bench.go --inproc                 (baseline, ~26 min на 50 запросов)
# через отдельный serve: ./bin/brain/bench.go --candidate http://127.0.0.1:8631
```

**nprobe-дефолт = 2000** (решение #206, замерено на живом корпусе 313689
векторов, 11 запросов golden-set): полный probe даёт порядок кандидатов,
совпадающий с точным сканом (recall@5 vs baseline = 1.0) при p50 ~470ms
(гейт <500ms); снижение до 256 ускоряет поиск (p50 ~260ms), но recall@5
падает до ~0.82 — теряются точные top-5. Выбор: корректность сначала,
тюнинг (256/512) — отдельной задачей, если понадобится p50 < 300ms.
Конфиг (`etc/brain/config.yml`, секция `vector.ann`): `enabled`, `index`,
`dim` (256), `nlist` (2000 — k-means cells, ~150 векторов на ячейку при
313k), `nprobe` (2000). В compose весь `etc/brain` монтируется в
`/data/etc/brain:ro` (repo root в контейнере — `/data`, #208); без mount
serve работает на код-default (nprobe=128). Идемпотентность: повторный
`upsert` не дублирует (idCell — map), повторный `build` из той же БД даёт
ту же мощность.
A/B-отчёты: #204 (кандидат), #206 (прод, nprobe 256 vs 2000).

## Disk mail/contacts import (#79)

Sources on `/mnt/8TB` (TB mbox, .eml, VCF/MAB) → corpus pipeline; see
[mail-sources.md](mail-sources.md) for the authoritative source table + status.
PST stays blocked until `libpst-utils` (`readpst`) is installed.

```bash
./bin/mail/convert-mbox.go --in <tb-root> --out var/corpus/mail --source tb-profile --dry-run
./bin/mail/convert-mbox.go --in <tb-root> --out var/corpus/mail --source tb-profile
./bin/mail/import.go --from-eml var/corpus/mail
```

## Sync wave + OO CRM (AGENTS.md, #81)

Deterministic ingest/reconcile wave; every step is a reconcile/upsert, so the
wave is idempotent. `--dry-run` prints the fixed order without executing.
Bulk rebuild is NOT part of the wave (use `bin/brain/index.go --rebuild`).

```bash
go run ./bin/stack/sync.go --dry-run                     # print the wave
go run ./bin/stack/sync.go                               # default wave
go run ./bin/stack/sync.go --only mail,mail-import       # subset, order kept
go run ./bin/stack/sync.go --with-mail --only mail,mail-import,mail-index  # mail → brain index (#199)
go run ./bin/stack/sync.go --with-chats --contacts <vcf> # + chats, contacts
go run ./bin/stack/sync.go --with-chats --git-root <dir> # + chats, git history
```

`--only` takes actual step names (fixed order): `mail`, `mail-import`,
`mail-index`, `chats`, `contact-brain`, `git-brain`, `contact-crm`.
`--with-mail` включает шаг `mail-index` (mail leafs обоих корпусов → brain,
#199); без флага шаг SKIP, не FAIL.

### chats step (--with-chats, #195)

The `chats` step is one logical step name that runs two sub-commands
sequentially: `bin/chat/sync.go telegram`, then `bin/chat/sync.go linkedin`.
Both download messages to `var/corpus/chats/<platform>/`.

Credentials come from the environment:

| Env var | Purpose |
|---------|---------|
| `TELEGRAM_API_ID` / `TELEGRAM_API_HASH` / `TELEGRAM_PHONE` | Telegram app credentials |
| `TELEGRAM_MCP_DIR` | path to the telegram-mcp checkout (session read from its `.env`) |
| `TELEGRAM_SESSION_STRING` | optional; otherwise read from `TELEGRAM_MCP_DIR/.env` |
| `LINKEDIN_USER_DATA_DIR` | LinkedIn profile dir with session files (default `~/.linkedin-mcp/profile`) |

**SKIP semantics**: a platform whose credentials/session are missing prints
`chats: SKIP …` and exits with code 3 (`pkg/cli.ExitSkip`) — the wave shows
`SKIP`, never `FAIL`. The same convention applies to `mail` sources without
creds (onlyoffice/m365/imap). Tools with credentials present but broken
(config paths, invalid API id) still FAIL loudly.

### var/ permissions (uid 1001 vs 1000)

The brain/mail-sync containers write `var/**` as uid 1001 (Dockerfile
`useradd --uid 1001`) while host tools run as uid 1000. A container write
leaves `var/kb.lbug.wal` and `var/corpus/**` unopenable by the wave
(`OpenDatabase … status 1`). Since compose.yaml now runs the containers as
the host uid (`user: "${KB_UID:-1000}:${KB_GID:-1000}"`), new writes align
automatically; a one-time repair for already-misowned files goes through
docker busybox (no sudo on this host):

```bash
scripts/stack/fix-var-perms            # chown -R var/ to the caller uid:gid
scripts/stack/fix-var-perms --check    # report files NOT owned by the caller
```

Run it after any container write or when a wave step fails on `var/**`.
The wave also quiesces the compose brain container around write steps
(`docker compose stop brain` → writes → `start brain`), so `git-brain` does
not collide with the running serve process holding `kb.lbug`.

### OnlyOffice CRM tools

OnlyOffice CRM tools need `ONLYOFFICE_URL`/`ONLYOFFICE_USER`/`ONLYOFFICE_PASS`
env. Both are read-only (report) by default; `--write` applies changes.

```bash
# reconcile report — human mail senders vs OO CRM persons (match by email)
go run -tags=onlyoffice_reconcile_contact ./bin/onlyoffice/reconcile-contact.go
go run -tags=onlyoffice_reconcile_contact ./bin/onlyoffice/reconcile-contact.go --write --limit 200

# interaction write — mail → history note on the person's opportunity
go run -tags=onlyoffice_import_interaction ./bin/onlyoffice/import-interaction.go
go run -tags=onlyoffice_import_interaction ./bin/onlyoffice/import-interaction.go --write --limit 50
```

Both scan `var/corpus/mail` by default (`--sources` to override) and build an
email → person index in one pass over the whole CRM. Machine senders
(newsletter/bounce/platform) never become contacts.

## Browser sync (periodic, #163)

Pushes the browser-extracted corpus (`var/corpus/{gmail,linkedin,djinni}`) into
the brain as leafs every 6 h. Thorium down is tolerated: extraction is skipped
and the last-known corpus is still ingested.

```bash
./bin/cron/browser-sync.go                  # one cycle, default paths
./bin/cron/browser-sync.go --skip-extract   # no Thorium probe
./bin/cron/browser-sync.go --interval 6h    # run continuously
sudo scripts/browser-sync-install.sh        # install the 6-hourly systemd timer
```

Log: `var/log/browser-sync.log`. The systemd units
(`scripts/browser-sync.{service,timer}`) use an `@REPO_ROOT@` placeholder; the
install script substitutes the real root at install time.

## Reasoner (optional, D18)

CPU sidecar on `127.0.0.1:11435`. Weights are not in the 2dph image.

```bash
docker compose --profile reasoner up -d reasoner
REASONER_BASE_URL=http://127.0.0.1:11435/v1 ./bin/reasoner/bench.go --json
```

See [reasoner.md](reasoner.md).

## GitHub mirror sync (#194)

Gitea is the canonical home; GitHub is a publish-only **showcase** (`#190`,
`#213`). `main` is the single working line and the public face (Gitea `#213`).
PRs merge into `main` directly; no `release/v1` branch is kept. After each
merge into `main`, sync **only `main`** (plus tags) to GitHub:

```bash
git fetch origin --tags --force
git push github main:main --force-with-lease
git push github --tags --force
```

Verify zero drift on `main`:

```bash
git rev-list --count origin/main..github/main   # 0
git rev-list --count github/main..origin/main   # 0
```

Branch policy (`#213`): both Gitea and GitHub keep `main` only. Old/merged
feature branches are backed up locally under `refs/backup/*` before deletion.

Content guard (`#190`): before pushing to GitHub, confirm the diff is project
content only — no internal paths/hosts/ports, no secrets. Precedent #142/#194:
leaks are purged from GitHub history with `git filter-repo`; Gitea stays the
canonical home and is never rewritten for the mirror.
