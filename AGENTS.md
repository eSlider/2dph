# AGENTS.md — 2dph (deductionphile)

Evidence-first brain over the ops/eSlider stack. Facts need proof or they are
`(not confirmed)`.

Read first: [PLAN](PLAN.md) → [docs](docs/) → [roadmap](docs/roadmap.md).
Standards epic: [#88](https://git.produktor.io/eSlider/2dph/issues/88).

## Work — Gitea only (MUST)

- Canonical home: `git.produktor.io/eSlider/2dph`. All `github.com/eSlider/go-*`
  libraries are owned; their working home is Gitea `git.produktor.io/eSlider/<repo>`
  ([#101]). GitHub is a publish mirror only. Issues, PRs, reviews, epics,
  milestones, releases exist **only on Gitea** — never on GitHub.
- Every task starts as a Gitea issue **на русском**. Epics group child issues;
  milestones are sprints. Agents report progress as issue comments like senior
  engineers and close issues via commit refs (`(#id)`).
- Roles: PO owns priorities, acceptance and dispatch; SE sub-agents implement
  TDD-style against one issue each; sync happens through Gitea only.
- Branches `type/slug#<issue>`; Conventional Commits
  `type(scope): summary (#id)` (feat|fix|refactor|chore|docs|test).
  PR → review → CI green → merge into `release/v1`. No direct pushes to
  `main`/`release/*`.
- SemVer tags on Gitea after merge: `fix:`→PATCH, `feat:`→MINOR,
  breaking (`!`)→MAJOR; next version computed by the semver tool.

## Method (detective)

≥2 independent sources or `(not confirmed)`; link the lexicon path behind
each claim. `facts` root = proven assertions, `info` = narrative. Deduction:
facts → info → web-search. PicoClaw: search → get → audit before factual
replies (`skills/picoclaw/SKILL.md`). `throttled` ≠ absence.

## Hard rules

1. **Secrets**: `~/.config/brain/`, `.env`, `.secrets/` never read into
   context, printed, or committed. Tokens live in credential helpers — never
   embedded in remote URLs.
2. **Read-only sources**: Ladybug DB and Postgres queried read-only; rebuilds
   write only under `var/` (gitignored).
3. **PII**: client data never read or quoted; test data synthetic
   (Alice/Bob/example.com).
4. **TDD — нет теста → не работает → задача открыта.** Failing test before
   tool code; tests run offline vs fixtures; network/db calls wrapped.
   A task is NOT done until a test proves it (and is green). For mail
   send/receive specifically: an end-to-end test (send → receipt) is MANDATORY
   before closing a mail task; until it exists and passes, the issue stays
   open.
5. **Docs reflect behaviour**: any change updates `docs/` + `PLAN.md` status.
6. **No hardcoded absolute paths or host URLs anywhere** (code, comments,
   tests, scripts). External values load from config; missing key after the
   full stack = explicit error pointing at `etc/brain/config.yml`.
7. **Gitea-задачи — на русском** (код и команды как есть).

## Code standards (Go) — mandatory for every implementation phase/issue

1. **Go only.** No Python scripting; no Python remnants (`__pycache__`).
2. **Static typing first.** No `map[string]any` / stringly-typed values at
   API boundaries; optional fields are pointers + `omitempty`; enums are named
   int types. Loose data (JSON forms, env, external payloads) merges into
   strict structs via `github.com/go-viper/mapstructure/v2`
   (`WeakDecode` at platform boundaries — see `dialog` reference).
3. **Primitives only via `pkg/utils/*.go`**: ptr/deref, first-non-empty, time
   normalization, numeric parsing, URL/path escaping. Never hand-roll per site.
4. **Clients/connections: read `skills/api-client/SKILL.md` FIRST — mandatory
   before implementing any client.** First-class example:
   `eSlider/go-ollama` client.go. Typed CRUD: go-onlyoffice tasks.go.
5. **Concurrency** (any API/connection/transformation):
   - public APIs stay synchronous — channels never in public signatures;
     wrap sync calls internally when async behaviour is needed;
   - `context.Context` first param on every IO op; transports use
   - `NewRequestWithContext`; blocking ops `select` on `<-ctx.Done()`;
   - bounded worker pools (buffered semaphore / jobs+results); fan-out/fan-in
     for multi-source merges; `errgroup.WithContext` for first-error cancel;
   - every goroutine wg-accounted; sender closes channels; receive errors
     never swallowed; backpressure over unbounded queues; no channels for
     simple CRUD/single-threaded logic;
   - graceful shutdown via `signal.NotifyContext`; CI runs `go test -race`.
6. **Config via `github.com/eslider/go-config` only.** Stack lowest→highest:
   `etc/brain/config.yml` (committed defaults) → `etc/brain/config.local.yml`
   (gitignored) → `.env` files → process env. One load per process into a
   strict struct; boot order: config → storage → clients → runner
   (`NewApp(configPath)` pattern). Merge semantics: nested maps recurse,
   scalar leaves last-write-wins, slices replace (concat opt-in); keys
   normalize lower+alnum so YAML/env/Go fields align. Never `os.Getenv` ad
   hoc. Secrets stay in gitignored `.env`, never in committed YAML.
7. **Zero-allocation hot paths** (ETL streams, indexing, transports, bulk
   transforms): pre-allocate known capacity `make([]T,0,cap)`; reuse buffers
   by resetting length (`buf = buf[:0]`); `sync.Pool` for frequently created
   objects — zero/reset before Put; no `+` string building in loops —
   `strings.Builder` / `bytes.Buffer.Grow()`; keep locals off the heap;
   sets are `map[K]struct{}`; fixed keys → structs over maps; leverage
   zero-value init. Prove with `go test -bench -benchmem`.
8. **MIME/EML: `github.com/emersion/go-message` only.** Envelope via its
   `mail` subpackage; recursive parts via `entity.Walk()` — never hand-split
   multipart, never flatten to flat attachment lists; stream body entities
   straight into converters (no whole-body buffering); charsets via
   `golang.org/x/text`. enmime stays legacy-parity only until cutover [#95].
9. **ETL handlers: any format may nest any other format** ([#96]).
   One Handler per type; `Handle(ctx, Blob) → *Result`; Result carries typed
   Meta + extracted text + lazy `Children []Attachment` (`Open func(ctx)
   (io.ReadCloser, error)` — never materialized buffers; large payloads spool
   to `var/tmp`). Registry dispatches by declared MIME × magic-byte sniffing
   (mismatch wins for safety) and recurses with depth ≤ 10, size/count limits,
   zip-ratio bomb guard, sanitized filenames (`..`, control chars, >255,
   collisions → `-<hash8>` suffix). Per-child errors are collected, never
   fatal to the whole tree. ZIP = `archive/zip`; HTML bytes stay raw unless a
   processor opts into `golang.org/x/net/html`.
10. **Single implementation.** Every transformer/handler/extractor/decoder
    exists exactly once, in `internal/{domain}`; tools and services import it.
    A duplicate is a bug — delete the copy in the same PR.
11. **URL content addressing** ([#100]). Every extracted node gets a stable
    URL `scheme://platform/thread/msg/path-segments[#anchor]`
    (e.g. `mail://gmail/T42/M17/body/p[3]/table[0]#r2,c5`); node ID =
    `sha256(URL)[:16]`; content integrity = separate `sha256(body)`. Parts
    reference each other only via URLs (href/src → `LINKS_TO` edges). Content
    splits granularly BEFORE insertion: html/md/pdf blocks are mime-typed
    Items (paragraph/table/image/link/page). DOM/jQuery-like selector
    mini-language supports point retrieval (`p[3] > table[0] > tr[1] td[2]`).

## Layout

```
.opencode/    NEVER commit — global config, lives at ~/.config/opencode/
PLAN.md       decisions + status          docs/    truth only (plans → Gitea)
skills/       vendored agent skills (api-client, etl-handler, …)
bin/{subj}/   tools ONLY: {verb}-{object}.go, singular, nothing loose at bin/
scripts/      executables & orchestration (docker-entrypoint, stack, db) [#89]
etc/{subject}/ configs; etc/brain/config.yml = central external-sources config
var/tmp       temp files/spool            var/dist   built binaries/toolchains
var/log       logs                        var/state  checkpoints/runtime state
var/cache     caches                      var/hf     model cache
var/kb.lbug   served DB
var/corpus/{git,mail,chats,docs,…}  ETL staging → ingest [#89]
pkg/          reusable Go, no 2dph deps (cli, utils [#92], duckdb, httpapi)
internal/     private Go by domain, mirrors bin/{subject}
```

Tool naming D14 unchanged: `bin/{subject}/{verb}-{object}.go`, singular.
Items marked `[#89]`/`[#92]` are mid-migration — see epic #88.

## Owned repos — shared template

go-config · go-onlyoffice · go-ollama (+ siblings under `eSlider/go-*`):
subpackage-per-domain — `doc.go`, `client.go` (Client + Config/DSN +
transport core), `{domain}/` packages, `cmd/`. Same Gitea workflow, same
standards. Reference implementations: go-ollama `client.go` (first-class),
go-onlyoffice `tasks.go` typed CRUD, `dialog` package (reply chains, upsert
idempotency, weak-decode boundaries), esliderbot `app.go` (bootstrap order,
relative `etc/config.yml`).

## Sync-ETL pipeline (epic #88)

```
Source.Fetch(ctx,cursor) → []Blob → Registry.Decode → Transform → Load(brain)
```

- Sources (one adapter each): onlyoffice · gmail · m365 · disk · chat
  platforms · git history. Atomic checkpoints in `var/state/<source>.json`;
  sha256 seen-set idempotency; upsert-on-conflict writes [#97][#98].
- **Conversation canon** ([#99]): one model for mail AND chats —
  `Message{ID, ThreadID, Platform, From, ReplyTo, To, CC, BCC, SentAt, Body,
  Attachments(lazy)}`. Stored on disk (`var/corpus/{mail,chats}` JSON +
  manifest) as the evidence layer; the brain ingests ONLY this canon:
  `(:Person)-[:SENT|TO|CC|BCC]->(:Message)`, `[:REPLY_TO]` threads,
  `[:PART_OF]` paragraphs. Rebuild without network; dedup by hash.
- PDF handler: original preserved in corpus; Ghostscript normalization
  (strip export protection → shrink → simplify extraction) produces the
  working artifact in `var/tmp`; then pdftotext fast path / tesseract
  fallback [#102].

## Tools

See `bin/*/doc.go` and `docs/runbook.md`. Paths above are post-migration
targets; until [#89] lands, legacy paths remain functional.

## Communication

Same tone as the corpus: plain, lists, no hype. Sign-off `Andriy Oblivantsev`.
German C1 where useful. Caveman only for agent chat, never in committed docs.
