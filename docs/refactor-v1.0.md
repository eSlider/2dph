# Refactor v1.0 — plan (Musk method)

Goal: cut clutter so each command, dir, service and MCP tool earns its place.
Evidence-first (Sherlock) applied to the tool surface itself.

Status: **in progress 2026-08-21** — P1–P4 done (cleanup, compose verify, MCP 5→3).
Open: **P5 tests taxonomy** (`qa/`→`test/{integration,system}`), **P6 FHS `etc/`**
(`deploy/`→`etc/{subject}/`), **P7 documentation** (project description + usage),
P8 (CI + PR). Epic [v1.0 #65](https://git.produktor.io/eSlider/2dph/issues/65),
milestone [v1.0](https://git.produktor.io/eSlider/2dph/milestone/16).
Parallel to cash-sprint [epic #62](https://git.produktor.io/eSlider/2dph/issues/62) — not closed.

## Measure first

| Parameter | Before (2026-08-21) | After | Target |
|-----------|--------------------:|------:|-------:|
| bin/ tool files (`.go` + executables) | 72 | ~67 | ~34 |
| bin/ dirs | 19 | 18 | ~15 |
| internal/ dirs | 13 | 13 | ~11 |
| compose services | 9 (3 default + 6 profile) | 9 | keep (already lean) |
| MCP tools | 5 | **3** | 3 |
| root config dirs (`deploy/`, `qa/`) | 2 | 0 (folded into `etc/`, `test/`) | 0 |

## A/B (delete / keep)

### Delete (verified 2026-08-21, each is deprecated/dup)
- `bin/kb/{add,index,search,watch}` — deprecated wrappers → real Go in `bin/brain/*` (PLAN.md D6). Delete dir.
- `bin/tools/web-search` — only test fixtures; move to testdata, drop dir. `bin/tools/__pycache__` — junk.
- `bin/serve.go`, `bin/fulfill-assoc.go`, `bin/doc.go` root shims — fold into `bin/brain/` or `bin/facts/`, delete root copies.

### Keep (verified not dup)
- `bin/chat` — launcher bash that builds+execs `bin/chats/` (not a dup of `chats/`). Keep.
- `bin/markdown/split-leaf.go` — real tool (H2→leaf), wired into `bin/shell/complete.go`. Keep.
- `bin/postgres/query.go` — active Go read-only wrapper over `bin/db/psql-yq`, documented. Keep.
- `bin/ci/semver.go` — **active**: CI Release job computes semver. Keep.
- `bin/fulfill-assoc.go` — active (recent commit c6d27c9, #52/#55). Keep.

### Keep (one clear owner each)
- `bin/brain/*` (11) — core read/write/search/serve.
- `bin/cgo/*` (zig/zcc/zc++) — toolchain.
- `bin/chats/*` — conversations.
- `bin/onlyoffice/import-contact.go`, `bin/brain/import-contact.go`, `bin/contact/list.go` — contacts (subject = target).
- `bin/db/*` — psql-yq, ssh-tunnel.
- `bin/facts/*` — audit/extract/crm.
- `bin/brain/import-git.go` — history.
- `bin/mail/*` — sync/import/ocr.
- `bin/jsonl/stats.go` — duckdb.
- `bin/reasoner/bench.go` — CPU bake-off.
- `bin/stack/*` — compose dispatcher.
- `bin/shell/complete.go` — flaggy completion.
- `bin/web/search.go` — SearXNG.

## compose (verified 2026-08-21)

9 services: **3 default** (`brain`, `brain-watch`, `mail-sync`) + **6 profile**
(`index`, `searxng`, `reasoner`, `picoclaw`, `ocr-paddle`, plus `brain-mcp` under
picoclaw). `reasoner-ollama` / `picoclaw-home` are named **volumes** (needed),
not stub services. Already lean — no merge needed. Verify P2 only that
default set stays minimal.

## MCP 5 → 3

`search`, `get`, `audit` are the detective lever (#15). Fold `stats` into `search`
(scores/quantiles block) and `ingest` into `get`? No — `ingest` is a write. Decision:
expose only `search` / `get` / `audit`; move `stats` behind `search --stats`,
`ingest` behind `POST /ingest` (OpenAPI, not MCP). Tools/list shows 3.

## P5 — tests taxonomy: `qa/` → `test/{integration,system}`

Current `qa/` is a grab-bag: `system_perf*.go`, `stress/`, `load_test_summary.md`.
Rework into a clear taxonomy (D-test):

- `test/system/` — offline-gated system tests (no live brain in CI): recall eval,
  perf gates, source-gate checks. Corresponds to current `qa/system_perf*`.
- `test/integration/` — tests that need a live dependency (OnlyOffice CRM, SearXNG,
  Ladybug DB, mail) — opt-in via build tag / env, not run by default `go test ./...`.
- `test/stress/` — load/stress scenarios (from `qa/stress/`).
- `test/README.md` — how to run each tier (`go test ./test/system/...`,
  `go test -tags=integration ./test/integration/...`).

CI keeps running only the offline `system` tier by default.

## P6 — FHS config: `deploy/` → `etc/{subject}/`

Move operator-edited config out of `deploy/` into `etc/{subject}/`, one per tool:

- `deploy/searxng/{settings.yml,limiter.toml}` → `etc/searxng/{settings.yml,limiter.toml}`
- `deploy/picoclaw/{config.json,mcp.json.example}` → `etc/picoclaw/…`
- `compose.yaml` stays at root (compose convention) but env templates →
  `etc/{brain,mail-sync}/.env.example`; examples of `db-profiles.yml` / `search.env`
  → `etc/{brain,postgres}/…example`.
- runtime output stays in `var/` (gitignored) — already done.

Compose paths and any `deploy/` references updated. `docker compose config -q` stays clean.

## mapstructure/v2 (D34) — verified 2026-08-21, not needed

Config loaders already use **`yaml.v3` struct tags** (`bin/facts/extract.go` →
`Services map[string]any`, `bin/fulfill-assoc.go` → typed `Org`/`Orgs`,
`bin/postgres/query.go` → thin bash wrapper over `bin/db/psql-yq`). There is no
hand-rolled map→struct to replace. Applying mapstructure would be a regression.
Decision: **keep yaml.v3 struct tags**; do not add mapstructure.

## P7 — documentation rework

Project must read as "what is this, how to use it" from a cold start:

- **README.md** — rewrite: what 2dph is, one-line pitch, quick start (build, serve,
  search), CLI tour, links to docs. Not an exhaustive tool list.
- **docs/runbook.md** — operational: build, config (etc/ + `~/.config/brain`),
  serve/search/watch/index, compose profiles, common tasks, troubleshooting.
- **docs/design.md** — architecture + decisions (already good; link from README).
- **docs/refactor-v1.0.md** — this plan.
- Verify: a fresh reader can go README → runbook → serve → search in ~15 min.

## Phases

| Phase | Work | Exit |
|-------|------|------|
| P1 | Snapshot bin/ + compose; verify delete list; delete `bin/kb/`, fold root shims; relocate web-search fixtures → testdata | no `bin/kb`, no root shims, fixtures in testdata |
| P2 | (verified) compose 9 services, 3 default + 6 profile — already lean | `docker compose config -q` clean |
| P3 | (cancelled after verify) `markdown`/`postgres`/`ci` are active & documented — keep | bin/ dirs = 19, all live |
| P4 | MCP 5→3 (stats/ingest out), OpenAPI paths stay, testdata reloc | tools/list = 3 |
| P5 | tests taxonomy `qa/` → `test/{system,integration,stress}` + test/README | `go test ./test/system/...` green; no root `qa/` |
| P6 | FHS config `deploy/` → `etc/{subject}/`; env templates; compose refs updated | no root `deploy/`; `docker compose config -q` clean |
| P7 | docs rework: README (what/how), runbook, design link, test/README | cold-start → serve → search in ~15 min |
| P8 | CI green (system tier), PLAN updated, PR | CI green |

## Non-goals

- No Python reintroduction.
- No behavior change to graph/search semantics.
- No rework of PLUGABLE reasoner / CGO toolchain.
- `bin/jsonl/stats.go` (DuckDB) stays — it is a tool, not the test taxonomy `qa/`.
- Not blocking cash-sprint #62.
