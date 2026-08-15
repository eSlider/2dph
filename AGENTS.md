# AGENTS.md — 2dph (deductionphile)

Evidence-first brain over the ops/eSlider stack. Facts need proof or they are
`(not confirmed)`.

Read first: [PLAN](PLAN.md) → [docs](docs/) → [roadmap](docs/roadmap.md)
(epic [#16](https://git.produktor.io/eSlider/2dph/issues/16)).

## Method (detective, no fork)

> ≥2 independent sources of evidence, or the finding is `(not confirmed)`.
> Link the lexicon yaml path that backs each claim.

- `facts` root = assertions backed by ≥2 independent sources (docker ps ×
  compose × ssh-config × docs).
- `info` root = descriptive/narrative leafs, searchable, never asserted as fact.
- Search is deduction: `facts` → `info` → `web-search` (second independent
  source). An answer is `confirmed` only if it comes off the facts root.
- Fact-check every *claim* (facts → info → live → web), not every edit or
  syntax tweak. PicoClaw: `search` then `get` then `audit` before a factual
  reply (`skills/picoclaw/SKILL.md`). `throttled` is not a negative finding.

## Hard rules

1. **Secrets.** `~/.config/brain/`, `.env`, `.secrets/` never read into context,
   printed, or committed. The OnlyOffice password is obtained via the
   tunnel, never written to this repo.
2. **Read-only data sources.** Ladybug `var/kb.lbug` and Postgres are opened
   read-only for queries. Index rebuilds write to `var/` (gitignored).
3. **PII.** `brain-test`, `cs_brain` client data is never read or quoted.
4. **No main pushes.** Feature branches + GitHub PR (`gh`); CI (Actions) must be green. Work board: [Gitea issues](https://git.produktor.io/eSlider/2dph/issues).
5. **TDD.** Failing test before tool code. Unit tests run offline against
   fixtures; network/db calls are wrapped.
6. **docs reflect behaviour.** Any change updates `docs/` + `PLAN.md` status.

## Layout

```
PLAN.md       decisions + execution + open questions
docs/         published docs
skills/       in-project agent skills (vendored, no external links)
bin/          self-describing tools bin/{subject}/{method}.go (shebang)
bin/brain/    search.go serve.go index.go add.go get.go stats.go eval.go watch.go
bin/chats/    sync.go import.go facts.go apply.go; libs in internal/chats
bin/mail/     sync.go import.go ocr.go (index_mail → brain/index.go)
bin/markdown/ import.go (H2 leaf split; Python bin/md/import fallback)
bin/postgres/ query.go (read-only YAML)
bin/git/      import.go (go-git history; Python shim execs it)
bin/web/      search.go (SearXNG; Python shim execs it)
bin/reasoner/ bakeoff.go (D18 CPU OpenAI tool-call bake-off)
internal/     shared Go (brain/rank is cgo-free; facts D16; cli flaggy D23; chats; gitlog; websearch; reasoner; duckstats)
bin/qa/       stats.go (DuckDB quantiles / JSONL count; gcc CGO, not Zig)
bin/watch/    corpus watcher (used by bin/brain/watch.go)
bin/tools/    vendored python libs behind bin/* (kblib, yamlout, websearch)
bin/cgo/      zig zcc zc++ (CGO via zig cc, not gcc)
bin/stack/    start start-assistant stop status (compose + PicoClaw agent)
bin/docker-entrypoint  container entrypoint (api: serve|search|watch; index: python)
compose.yaml  docker composition (root level, not docker/)
Dockerfile    api (Zig CGO, no Python) + index (Python write)
var/          kb.lbug, var/mail/*, caches (gitignored)
.venv/        ladybug + model2vec + mistune
```

## Mail pipeline

```bash
bin/mail/sync.go --source onlyoffice,gmail --workers 8 --out var/mail   # raw message.json + attachments
bin/mail/sync.go --source gmail --query 'from:example.com' --out var/mail  # Gmail search (default in:inbox)
bin/mail/sync.go --source m365 --env ~/.config/brain/mail.env --out var/mail  # Microsoft Graph (delta)
bin/mail/import.go --from-raw var/mail                                  # message.json → message.md (convert only)
bin/brain/index.go --rebuild --with-facts --with-chats
bin/stack/start-mail-sync                                               # compose ETL: sync→import every 300s
```

- `sync` (Go) downloads messages + attachments; Gmail uses paginated list +
  `body.attachmentId` (not partId) for attachments. Sources: `onlyoffice`,
  `gmail`, `m365` (client-credentials + delta link; commit after success).
- Compose `mail-sync` / `bin/stack/start-mail-sync`: ETL loop (default
  `onlyoffice,gmail`, 300s). On `new>0` runs import; full `--rebuild` only if
  `MAIL_SYNC_INDEX=1`. Secrets: `~/.config/brain/mail.env` + `~/.gmail-mcp`.
  Case wrappers (e.g. family `gmail-sync-la-quinta.sh`) and ai-bot
  `gmail-reauth.sh` reuse this sync/OAuth — do not fork corpus download.
- `import` converts body + attachments to markdown. PDFs use poppler
  `pdftotext -layout` fast path (~15ms); textless/scanned PDFs use
  `pdftoppm` + tesseract `eng+deu` (`bin/mail/ocr.go`). Optional
  `OCR_ENGINE=paddle`. Conversion never touches the brain DB (crash safety).
- `index_mail` is a deprecation shim for `bin/brain/index.go --rebuild`. Bulk
  rebuild still deletes `var/kb.lbug` and creates FTS/HNSW last. Single-leaf
  write is `bin/brain/add.go` (safe while indexes exist; do not DROP INDEX).
  Keep conversion + indexing separate so a conversion crash can't leave the
  DB mid-transaction.

## Tools

```bash
bin/facts/audit.go ["self"|"db"|"contradict"]     # 2-source + D16 adjudication
bin/facts/crm.go [--dry-run]                       # proof person↔company/company↔project (ooCRM × corpus SoT)
bin/kb/search "query" [--repo X]                  # deprecated wrapper → bin/brain/search.go
bin/brain/search.go "query" [--root facts|info]   # deduction search → YAML
bin/brain/search.go "query" --as-of 2025-01-01    # D24 fact intervals
bin/brain/search.go "query" --no-web              # local graph only
source <(./bin/cli/complete.go bash)              # flaggy completions (D23)
eval "$(bin/cgo/zig env)"                         # optional; Ladybug shebangs call bin/cgo/zig
bin/brain/index.go --rebuild [--with-mail] [--with-facts] [--with-chats]
bin/brain/add.go --text T --root facts --source "a.md x b.md"  # incremental write
bin/brain/add.go --json                                      # stdin leaf or {leafs:[...]}
bin/brain/get.go <id> [--body] [--json]          # Go read; Python bin/kb/get CI fallback
bin/brain/stats.go [--json]
bin/brain/eval.go [--json]                       # recall@5; questions in internal/brain/rank
bin/brain/serve.go                               # HTTP :8630; GET /openapi.json POST /mcp
bin/stack/start                                  # brain HTTP/MCP (reuse healthy :8630)
bin/stack/start-assistant                        # + reasoner + PicoClaw agent
bin/stack/status                                 # YAML health
bin/stack/stop                                   # compose stop; volumes kept
bin/markdown/import.go [dir]                      # H2 leafs → YAML; Python bin/md/import fallback
bin/git/import.go [REPO] [--json] [--limit N]     # go-git history → commit leafs
bin/web/search.go "query" [--json]                # SearXNG; throttled ≠ absence
bin/reasoner/bakeoff.go [--model ID] [--json]     # D18 CPU tool-call bake-off
bin/postgres/query.go --profile onlyoffice -c 'SELECT 1'
bin/qa/stats.go                                  # D22 DuckDB quantiles / JSONL (gcc CGO)
bin/mail/ocr.go <image|pdf>                      # tesseract eng+deu (scans)
bin/md/tables                                     # what the graph holds → YAML
bin/brain/deduce "question"                       # thinking wrapper
```

Never start a shell command with `cd` — use the tool working-directory
parameter. Search before reading whole files. For YAML/JSON/XML/CSV/TOML/HCL
prefer mikefarah/yq (`skills/yq/SKILL.md`). For bulk rows and quantiles use
duckdb-go (`internal/duckstats`, `skills/duckdb/SKILL.md`), not Ladybug.

## GitHub safety rules (ABSOLUTE — never violate)

1. **No absolute paths in committed files.** Replace `/mnt/`, `/home/<user>/`,
   `/Users/<user>/` with env vars (`$HOME`, `$PROJECTS_ROOT`, `$DOCS_BASE`).
2. **No PII in commits.** No real names, phones, emails of third parties.
   Test data must be synthetic (Alice, Bob, Charlie, Diana, example.com).
3. **No credentials/secrets in commits.** API keys, tokens, passwords, session
   strings, phone numbers only in gitignored `.env` files, referenced by path.
4. **Curasoft, edelweiss — no files, no mentions.** Remove all traces if found.
5. **Check git history before push.** If any commit contains leaks, rewrite
   history (rebase + force push) AND delete affected GitHub releases/tags.
6. **`docs/chat-import-plan.md`** — reference Gitea issue, never embed secrets.

## Communication

Same tone as the corpus: plain, lists, no hype. Sign-off `Andriy Oblivantsev`.
German C1 where useful. Caveman only for agent chat, never in committed docs.