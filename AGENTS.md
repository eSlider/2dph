# AGENTS.md — 2dph (deductionphile)

Evidence-first brain over the ops/eSlider stack. Facts need proof or they are
`(not confirmed)`.

Read first: [PLAN](PLAN.md) → [docs](docs/).

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
bin/brain/    search.go serve.go index.go get.go stats.go eval.go watch.go
bin/chats/    sync.go import.go facts.go apply.go; libs in internal/chats
bin/mail/     sync.go import.go (index_mail → brain/index.go)
bin/markdown/ import.go (mistune leafs)
bin/postgres/ query.go (read-only YAML)
bin/git/      import.go (go-git history; Python shim execs it)
bin/web/      search.go (SearXNG; Python shim execs it)
internal/     shared Go (brain/rank is cgo-free; chats parsers; gitlog; websearch)
bin/watch/    corpus watcher (used by bin/brain/watch.go)
bin/tools/    vendored python libs behind bin/* (kblib, yamlout, websearch)
bin/docker-entrypoint  container entrypoint (brain index|search|serve|watch)
compose.yaml  docker composition (root level, not docker/)
Dockerfile    multi-stage: python deps + static Go binaries
var/          kb.lbug, var/mail/*, caches (gitignored)
.venv/        ladybug + model2vec + mistune
```

## Mail pipeline

```bash
bin/mail/sync.go --source onlyoffice,gmail --workers 8 --out var/mail   # raw message.json + attachments
bin/mail/sync.go --source gmail --query 'from:example.com' --out var/mail  # Gmail search (default in:inbox)
bin/mail/import.go --from-raw var/mail                                  # message.json → message.md (convert only)
bin/brain/index.go --rebuild                                            # rebuild brain incl. all mail (fresh DB)
```

- `sync` (Go) downloads messages + attachments; Gmail uses paginated list +
  `body.attachmentId` (not partId) for attachments.
- `import` converts body + attachments to markdown. PDFs use poppler
  `pdftotext -layout` fast path (~15ms); textless/scanned PDFs fall back to
  docling (isolated subprocess — its native onnx can segfault the parent).
  Conversion never touches the brain DB (crash safety).
- `index_mail` is a deprecation shim for `bin/brain/index.go --rebuild`. Ladybug
  corrupts its WAL when brand-new leafs are bulk-inserted while FTS/vector
  indexes exist; a fresh DB with indexes created last is the only safe path.
  Keep conversion + indexing separate so a conversion crash can't leave the
  DB mid-transaction.

## Tools

```bash
bin/facts/audit.go ["self"|"facts"|"info"|"stale"]  # 2-source + staleness gate
bin/facts/crm.go [--dry-run]                       # proof person↔company/company↔project (ooCRM × corpus SoT)
bin/kb/search "query" [--repo X]                  # deprecated wrapper → bin/brain/search.go
bin/brain/search.go "query" [--root facts|info]   # deduction search → YAML
bin/brain/search.go "query" --no-web              # local graph only
bin/brain/get.go <id> [--body] [--json]          # Go read; Python bin/kb/get CI fallback
bin/brain/stats.go [--json]
bin/brain/eval.go [--json]                       # recall@5; questions in internal/brain/rank
bin/brain/serve.go                               # HTTP :8630; GET /openapi.json POST /mcp
bin/markdown/import.go [dir]                      # mistune leaves → YAML
bin/git/import.go [REPO] [--json] [--limit N]     # go-git history → commit leafs
bin/web/search.go "query" [--json]                # SearXNG; throttled ≠ absence
bin/postgres/query.go --profile onlyoffice -c 'SELECT 1'
bin/md/tables                                     # what the graph holds → YAML
bin/brain/deduce "question"                       # thinking wrapper
```

Never start a shell command with `cd` — use the tool working-directory
parameter. Search before reading whole files.

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