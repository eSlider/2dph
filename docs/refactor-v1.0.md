# Refactor v1.0 — plan (Musk method)

Goal: cut clutter so each command, dir, service and MCP tool earns its place.
Evidence-first (Sherlock) applied to the tool surface itself.

Status: **plan agreed 2026-08-21** (epic [v1.0 #65](https://git.produktor.io/eSlider/2dph/issues/65),
milestone [v1.0](https://git.produktor.io/eSlider/2dph/milestone/16)).
Parallel to cash-sprint [epic #62](https://git.produktor.io/eSlider/2dph/issues/62) — not closed.

## Measure first

| Parameter | Today (2026-08-21) | Target |
|-----------|--------------------:|-------:|
| bin/ tool files (`.go` + executables) | 72 | ~34 |
| bin/ dirs | 19 | ~15 |
| internal/ dirs | 13 | ~11 |
| compose named services | 12 | ~6 |
| MCP tools | 5 | 3 |

## A/B (delete / keep)

### Delete (verified 2026-08-21, each is deprecated/dup)
- `bin/kb/{add,index,search,watch}` — deprecated wrappers → real Go in `bin/brain/*` (PLAN.md D6). Delete dir.
- `bin/tools/web-search` — only test fixtures; move to testdata, drop dir. `bin/tools/__pycache__` — junk.
- `bin/serve.go`, `bin/fulfill-assoc.go`, `bin/doc.go` root shims — fold into `bin/brain/` or `bin/facts/`, delete root copies.

### Keep (verified not dup)
- `bin/chat` — launcher bash that builds+execs `bin/chats/` (not a dup of `chats/`). Keep.
- `bin/markdown/import.go` — real tool (H2→leaf), wired into `bin/cli/complete.go`. Keep.
- `bin/postgres/query.go` — keep unless it duplicates `bin/db/`; verify in P3.
- `bin/ci/semver.go` — keep if CI uses it; verify in P3.

### Keep (one clear owner each)
- `bin/brain/*` (11) — core read/write/search/serve.
- `bin/cgo/*` (zig/zcc/zc++) — toolchain.
- `bin/chats/*` — conversations.
- `bin/contacts/*` — CRM importer.
- `bin/db/*` — psql-yq, ssh-tunnel.
- `bin/facts/*` — audit/extract/crm.
- `bin/git/import.go` — history.
- `bin/mail/*` — sync/import/ocr.
- `bin/qa/stats.go` — duckdb.
- `bin/reasoner/bakeoff.go` — CPU bake-off.
- `bin/stack/*` — compose dispatcher.
- `bin/cli/complete.go` — flaggy completion.
- `bin/web/search.go` — SearXNG.

## compose 12 → ~6 (real runtime, profiles for the rest)

Merge by runtime role, keep optional under profiles:
- `brain` (api) + `brain-mcp` → one `brain` service (MCP is same image).
- `brain-watch` + `index` → one `index` profile (write/rebuild).
- `mail-sync` stays.
- `searxng`, `reasoner`, `picoclaw`, `ocr-paddle` → optional profiles (already not always-on).
- Drop empty stubs `reasoner-ollama`, `picoclaw-home`.

## MCP 5 → 3

`search`, `get`, `audit` are the detective lever (#15). Fold `stats` into `search`
(scores/quantiles block) and `ingest` into `get`? No — `ingest` is a write. Decision:
expose only `search` / `get` / `audit`; move `stats` behind `search --stats`,
`ingest` behind `POST /ingest` (OpenAPI, not MCP). Tools/list shows 3.

## FHS etc/+var/

Move operator-edited config out of repo root into `etc/` (compose, env templates,
db-profiles examples). Keep runtime output in `var/` (gitignored) — already done.

## mapstructure/v2 (D34)

`mapstructure/v2` for map→struct conversions (config load, fact mapping) instead of
hand-rolled field copies. Do not touch the graph path.

## Phases

| Phase | Work | Exit |
|-------|------|------|
| P1 | Snapshot bin/ + compose; verify delete list; delete `bin/kb/`, fold root shims | no `bin/kb`, no `bin/tools`, no root shims |
| P2 | Merge compose 12→6 (profiles), drop empty stubs | `docker compose config -q` clean |
| P3 | Fold `kb`, `markdown`, `postgres`, `ci` into owners; move root shims | bin/ dirs ≤ 15 |
| P4 | MCP 5→3 (stats/ingest out), OpenAPI paths stay | tools/list = 3 |
| P5 | `etc/` layout + mapstructure/v2 in config load | configs load via etc/ |
| P6 | CI + tests green, docs/PLAN updated, PR | CI green |

## Non-goals

- No Python reintroduction.
- No behavior change to graph/search semantics.
- No rework of PLUGABLE reasoner / CGO toolchain.
- Not blocking cash-sprint #62.
