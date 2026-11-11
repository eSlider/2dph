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

## Read through the contract (client, P-9.5)

`bin/brain/*.go` above read kb.lbug directly (needs a local DB + cgo).
For agents/scripts/gator/cv on any machine with the brain service (HTTP :8630,
`bin/brain/serve.go`) there is a service client for the read contract — **the same JSON,
no kb.lbug and no cgo**:

```bash
bin/brain/client.go search "onlyoffice postgres" --root facts --json  # facts: facts gate
bin/brain/client.go search "onlyoffice postgres"                      # facts → info, info marked (not confirmed)
bin/brain/client.go get <id> --body                                   # full text + source (evidence)
bin/brain/client.go stats --json
bin/brain/client.go audit                                             # facts-root hygiene (exit 1 on hypothesis/partial)
bin/brain/client.go search "q" --root facts --as-of 2025-01-01        # D24 validity
```

- SDK: `pkg/brainclient` (typed `Search`/`Get`/`Stats`/`Audit`/`Facts`,
  response validation by the contract). CLI: `pkg/brainclient/cli` + shebang
  `bin/brain/client.go`. Formats and the gate — `docs/brain/read-contract.md`
  (section "Client layer (P-9.5)").
- **Facts gate**: `--root facts` / `Client.Facts` return only
  `confidence=confirmed` (root=facts); everything else goes to `not_confirmed[]` with
  the mark `(not confirmed)`. 2v2 contradictions (hypothesis, D16) are never
  served as confirmed. `audit` with `not_confirmed_facts > 0` — exit code 1:
  analyse via `bin/facts/audit.go contradict` / `audit-card` (L-9).
- Do not open kb.lbug from client code: the read contract (formats above) is
  the only read path for consumers.

## Corpus — what lives in the brain (#198/#199)

`info` holds the WHOLE corpus, never just one root. Current composition
(after the full rebuild 2026-09-02, P-9.3 content-address dedup:
307,431 streamed mail items collapsed into 105,027 unique, #248;
105,220 leafs in total in the DB — see docs/brain/memory-adr.md §4.4):

- **mail** — BOTH corpora index into the brain: live `var/corpus/mail`
  (inbox 50 + PST) AND legacy `var/mail` (215k message.md: tb-andriy-profile,
  tb-backup-128g, tb-2010-zip, contacts_eml, defacto, gmail_*, inbox).
  `bin/brain/index.go --with-mail` enables the mail adapter
  (`corpus.Mail`, `corpus.MailRoots`); dedup between corpora — by ContentHash
  (external_id = content address of the text, P-9.3). Mail is only in the brain if
  the index ran with `--with-mail`.
- **git history** — `bin/brain/import-git.go --root <dir>` (git adapter, go-git).
- **chats** — telegram/linkedin/whatsapp messages.md (`--with-chats`,
  `var/corpus/chats/md`).
- **docs** — README/PLAN/AGENTS/docs/skills (default) + `--corpus` paths.

If a search misses mail that exists on disk: the brain was rebuilt WITHOUT
`--with-mail`, or `var/mail` was never indexed. Fix:
`bin/stack/sync.go --with-mail` (wave step `mail-index`) or
`bin/brain/index.go --skip --with-mail` (resume/append, de-duped, idempotent).
`bin/brain/stats.go` on the ops host must show the post-rebuild baseline:
**total 105,220 = info 105,199 (mail 105,027 + docs 172) + facts 21**
(ANN 105,220; after the full rebuild 2026-09-02). Anything much less means a
corpus is missing. The old rule "info ≥ ~200k" (and the ~313k reference point) is retired —
P-9.3 dedup collapsed the mail duplicates, the current model is docs/brain/memory-adr.md.

## Corpus sources (P-9.3) — every corpus is an adapter

`info` holds the whole corpus via four adapters (`internal/corpus`, cgo-free),
each implementing `contract.Source` (`Name()` + `Stream(ctx, emit(Leaf))`):

| Corpus | Adapter | What it indexes | source / external_id |
|--------|---------|-----------------|----------------------|
| docs | `corpus.Docs` | README/PLAN/AGENTS/docs/skills + `--corpus` paths (md + yaml) | `docs` / rel path |
| mail | `corpus.Mail` | live `var/corpus/mail` + legacy `var/mail` (`<id>/message.md`) | `mail` / content address `sha256(text)[:16]` |
| chats | `corpus.Chats` | `var/corpus/chats/md/<platform>/<chat>/messages.md` | `chats` / frontmatter `id` |
| git | `corpus.Git` | commit history (go-git) | `git` / full commit sha |

`bin/brain/index.go` — the driver: `corpus.StreamAll` → `brain.WriteCorpus`
(single writer, id = `contract.ContentHash()`[:32]). Flags enable adapters:
`--with-mail`, `--with-chats [DIR]`, `--corpus DIR` (repeatable), `--git-root DIR`.
`bin/brain/import-git.go --root DIR` — a thin wrapper over the git adapter
(wave `stack/sync.go`, step git-brain).

**How to add a new corpus source** (for example `calendar`):
1. A new adapter file in `internal/corpus/<name>.go`: a struct with `Name() string`
   (the corpus name — it is also the `source` in the leafs) and
   `Stream(ctx, emit func(contract.Leaf) error) error`; every emit must
   pass `Leaf.Validate()` (source/external_id/kind/text), the text is
   `Heading + "\n\n" + body`.
2. A unit test next to it (`<name>_test.go`, fixtures in t.TempDir, no lbug): leaf shape,
   external_id, dedup by ContentHash.
3. Register the adapter in `sources()` of the driver `bin/brain/index.go`
   (+ a flag switch if needed).
4. Rebuild: `bin/brain/index.go --rebuild <flags>` — deterministic ids
   (ContentHash) → adding/removing a corpus without manual magic.
5. `skill-sync push` + a PR on the updated SKILL.md (see skill-sync).

The git duplicate is eliminated: git history is written only by the git adapter;
`var/corpus/git/*.md` and `2dph__corpus__git__*` are excluded by the docs adapter.

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