# ADR P-9.6: 2dph memory — a single model of facts, audit and storage

> Status: **Accepted** (2026-09-03). The document introduces no new mechanisms: it
> consolidates the already accepted and working decisions (P-9.2/#235, P-9.3/#13,
> P-9.4/#240, L-9/#229–234) into one model for clients. A change of the model —
> through a new issue; epic P-9 (#239).

## 1. Purpose and place of the document

2dph ("memory", the prefrontal cortex per ADR-0012) holds **facts and conclusions**:
leafs (statements) + links + audit. Scattered documents described parts of the
model; this ADR — a single entry point for clients (agents/MCP, gator, cv,
scripts), so that everyone relies on one model of facts/audit/storage.

What it does and what it does not do:

- **consolidates** what already exists: the write contract (`docs/brain/contract.md`,
  P-9.2/P-9.3), the read contract (`docs/brain/read-contract.md`, P-9.4),
  the audit empirics (`docs/facts/audit-recipes.md`, #52/#56) and the formal
  logic of L-9 (issues #229–234), the facts layer (`bin/facts/extract.go`,
  `docs/facts/ingest.md`);
- **does not duplicate code and does not replace** the contracts: here — the model and references,
  the normative part — in the listed files (the overlap with L-9 is minimal at the file level:
  the ADR is a separate file, L-9 writes to `docs/facts/audit-recipes.md` and
  `var/audit/source-of-truth.yml`);
- **does not describe** the collection/CRM/gator tools — their zones are fixed by ADR-0012
  (zone responsibility, "one canon per data type").

Zone map (ADR-0012), where 2dph lives:

| Zone | System | Canon |
|------|---------|-------|
| sensors/raw | gator (`var/` raw/parquet) | **data** (raw, versions, append-only) |
| cortex/memory | **2dph** (Ladybug `var/kb.lbug`) | **facts/conclusions** (leafs) + links |
| skeleton | OnlyOffice CRM | business structure (deals/contacts/tasks) |
| superego | skills/inventar | rules, contracts, ADR |

2dph references the data and CRM canons, does not copy them; it keeps its own canon (facts/
conclusions) itself. Details — ADR-0012 (external fabric document, inventar
`docs/adr/ADR-0012-responsibility-zones.md`).

## 2. Fact model: terms

Unified terms (matching the write and read contracts):

| Term | Definition |
|--------|-------------|
| **observation** (corpus leaf) | a single piece of evidence from one source: a corpus sheet (`root=info`, kind `mail`/`commit`/`chat`/`reference`/`seed`/…). Written by a corpus adapter from one file/commit/message — by construction **single-source**, on its own it asserts nothing. The moment "when the content was seen" is recorded by `observed_at` (RFC3339). |
| **info** | the root of narrative/descriptive corpus sheets (mail/git/chats/docs). The search layer, `never asserted`. One sheet = one observation. |
| **fact** | a claim under `root=facts`, confirmed by **≥2 independent sources**; `confidence: confirmed`. The only root whose answer can be given as confirmed knowledge. |
| **source independence** | sources from different witness classes: runtime × declared (e.g. `docker ps` × compose file; `~/.ssh/config` × README/PLAN/AGENTS). In the sheet the pair is written to `source` via `" x "` (fact-layer notation, `facts/extract`). |
| **version** | one version = one id. Record id = `ContentHash(source\|external_id\|kind\|text)[:32]` (dedup key == id). A change of content/identity → new hash → new version; the old one stays in the graph. Rewriting the same version — MERGE (upsert). |
| **validity** | `valid_from` / `valid_to` (`YYYY-MM-DD`, D24) — the interval of truthfulness **of the fact by days**, orthogonal to the version (the version is about the record, validity about the meaning). An empty interval = "always" (legacy); an empty `valid_to` = an open end; the ends are inclusive. Filter — `--as-of YYYY-MM-DD`. |
| **confirmed** | `confidence: confirmed`; an empty confidence value (legacy leafs) is treated as `confirmed`. |
| **(not confirmed)** | any answer that **did not come from the facts root**: an info hit, hypothesis/partial, a single source, a 2v2 contradiction with no rule triggered. The agent must mark such an answer `(not confirmed)` and not present it as a fact. |
| **stale** | `valid_to` in the past, or the fact is absent in today's `--as-of` view (D24). Not to be confused with D16 `temporal_freshness` (source freshness against HEAD). |

One sheet in the DB — contract fields against the Ladybug runtime schema:

| Contract (`internal/contract.Leaf`) | Column/meaning in the DB |
|-------------------------------------|--------------------|
| `source` | name of the corpus source: `mail`/`git`/`chats`/`docs`; for facts — a pair of sources `"S1 x S2"` |
| `external_id` | stable ref inside the corpus (message id / commit sha / chat id / rel-path; for mail — content-address `sha256(text)[:16]`) |
| `kind` | sheet type (in the DB the column is `type`): `fact`/`mail`/`chat`/`commit`/`reference`/`seed`/… |
| `text` | content (Heading + body, uniform across all corpora) |
| `root` | `facts` (claim, ≥2 sources) or `info` (narrative); default `info` |
| `confidence` | open domain: `confirmed`/`estimated`/`inferred`/`hypothesis`/`partial`/… |
| `valid_from`/`valid_to` | D24 validity day (see above) |
| `observed_at` | when the content was seen (RFC3339); **outside** the dedup key |

The write norm — [`docs/brain/contract.md`](contract.md) (Leaf.Validate(),
`NormalizeText`, ContentHash, `observed_at` outside the hash, corpus adapters).

## 3. Audit model

### 3.1 How a fact is confirmed

1. **Observations** come from corpora (adapters `internal/corpus`) and are written
   always as `root=info` (`internal/brain/corpus.go`, `WriteCorpus`) — one
   sheet originates from one file/commit and on its own cannot be a fact.
2. The **facts layer** — a separate evidence gate (`bin/facts/extract.go`), not a corpus:
   it matches two independent classes of sources —
   S1 runtime (`docker ps` / `~/.ssh/config`) × S2 declared
   (compose file / README+PLAN+AGENTS) — and writes the match as
   `root=facts`, `confidence: confirmed`, `source: "S1 x S2"`.
3. **Promote** (`bin/facts/promote.go`): already confirmed `info` sheets
   whose `source` contains ≥2 distinct sources are re-rooted into `facts`
   (idempotently).
4. On **rebuild** the facts layer is enabled only by the `--with-facts` flag
   (`bin/brain/index.go`); without it the facts root is empty (#181) — see
   `docs/facts/ingest.md`.

Two-source verification against the live DB — `bin/facts/audit.go db` (every
`root=facts` sheet); the root of the "≥2 sources" rule — evidence-first (AGENTS.md),
the empirics — `docs/facts/audit-recipes.md` (#52/#56).

### 3.2 When an answer is (not confirmed)

- the answer came from the `info` root (narrative, even if `confidence: confirmed`);
- the claim has one source (hypothesis/partial);
- **2v2 contradiction**: `a x b` against `c x d`, both sides with ≥2 sources,
  but no rule triggered (`temporal_freshness` / `authority_pairing`,
  D16) — it stays a hypothesis until investigated (`bin/facts/audit.go
  contradict`);
- stale (valid_to in the past or absent in today's `--as-of`).

The deductive search order: **facts → info → web** (`web` — SearXNG, the second
independent source, kept in a separate block; `throttled`/`refused` — not
proof of absence).

### 3.3 Formal logic L-9 (Vinogradov)

The empirics of the recipes (#52/#56) is complemented by formal laws at the level of
judgments/URLs (L-9, issues #229–234; the core `internal/facts`, cgo-free):

| Law | Rule | Verdict |
|-------|---------|---------|
| identity (sameness) | one canonical URL in different spellings → merge/flag | `weaken` |
| contradiction | confirmed `P` and `¬P` about one predicate of one URL with overlapping D24 intervals | `reject` |
| excluded middle | hypothesis/partial or a claim without a predicate — not FACT, a candidate for `open_questions[]` | `weaken` |
| sufficient reason | verdict `accept` only with FACT-/OPEN- premises | `reject`/`weaken` |

A judgment = a fact about a URL resource: `{id, url, claim, attr, neg, conf, from, to}`
(`attr` — the predicate, `neg` — the P/¬P sign). Machine checks —
`bin/facts/audit.go formal` (stdin JSON). URL canonicalization —
`facts.CanonicalURL` (gator convention G-8.1). Full recipes —
`docs/facts/audit-recipes.md` (§Formal URL checks, L-9.3).

### 3.4 Audit cards and source-of-truth

Every verdict is recorded by a **Vinogradov card** in the logical SoT
(`var/audit/source-of-truth.yml`, `audits[]`, L-9.1 #230 / L-9.2 #231):
a recommendation without a card = a violation. The card must rely on
FACT-/OPEN- premises:

```bash
bin/facts/audit-card.go --claim "…" --premises FACT-9002,FACT-9001 \
  --inference deduction --gaps OPEN-0001 --counter none --verdict weaken
```

- The CLI validates the card (verdict `accept|reject|weaken`, inference
  `deduction|induction|analogy|other`, mandatory claim/premises/counter),
  assigns `AUD-NNNN`, appends to `audits[]` (yaml tree, does not touch the other
  sections).
- The SoT stores only indexed facts `{id, claim, source}` — slices from
  `root=facts` Ladybug; the raw material stays in `var/`; no secrets (ASR-0006);
  the file is gitignored, lives locally as a circuit artifact.
- In `root=facts` there are **no** vacancy facts (truth-gate #52) — see OPEN-0001 in
  the SoT: not invented.

### 3.5 Read audit (response formats)

- `/audit` (HTTP/MCP) and `bin/facts/audit.go db` — a `root ×
  confidence` histogram (format — the read contract, field `by_confidence`);
- `bin/brain/audit-contract.go` — a read-only audit of **conformance to the write
  contract** (missing required fields, distribution by corpus);
- `bin/brain/read-contract.go` / `read-contract-db.go` — format gates of the
  read contract (live service and live DB, read-only).

## 4. Storage model

### 4.1 Ladybug, one file, two roots

All memory — one embedded Ladybug graph: `var/kb.lbug`. `facts` and `info` —
the `root` column on each sheet, a **semantic, not physical** separation:
both roots live in one file and are written in one transaction (D12, ACID — the audit
sees a consistent snapshot).

The schema (`internal/brain` `InitSchema`, idempotent): nodes
`Leaf`/`File`/`Host`/`Commit`/`Person`/`Message` + edges
`FROM_FILE`, `RUNS_ON`, `AUTHORED`, `SENT`, `TO`, `CC`, `BCC`, `REPLY_TO`,
`SYNAPTIC` (user leaf↔leaf links, #82). A sheet carries an embedding
`FLOAT[256]`; the ANN index — `var/state/vector.ann` (dim=256, nlist=2000,
nprobe=2000, #206). The Ladybug schema is internal, changes without notice;
**clients never parse `var/kb.lbug` directly**, only the read contract.

### 4.2 Write: contract + corpus adapters + facts layer

| Write path | Tool | What it writes |
|-------------|------------|-----------|
| corpora (docs/mail/chats/git) | adapters `internal/corpus` (`contract.Source`: `Name()` + `Stream(ctx, emit(Leaf))`) → driver `bin/brain/index.go` → `brain.WriteCorpus` | `root=info`, one observation per file/commit/message |
| facts | `bin/facts/extract.go` → `UpsertLeaf` (directly, without `FROM_FILE`) | `root=facts`, `kind=fact`, a pair of sources in `source` |
| manual addition | `bin/brain/add.go` (`--root facts --source "a.md x b.md"`) | a sheet per the contract |
| rebuild | `bin/brain/index.go --rebuild --with-mail --with-facts [--with-chats] [--git-root DIR]` | full id rehash = ContentHash, deterministically |

Facts are written **without `external_id`** (21 sheets on the live DB) — the direct
`UpsertLeaf` path is recorded by the contract as allowed (P-9.3, the discrepancy is
described in `docs/brain/contract.md`). `observed_at` is stamped on write
if empty.

### 4.3 Read: the read contract (one format on all surfaces)

Read contract (`internal/contract/read.go`, version **`1.0`**, MAJOR.MINOR):
every response carries `contract_version`; additive edits — MINOR, breaking —
MAJOR. Clients must accept any response with the same MAJOR, ignore
unknown fields, and pass the absence of `contract_version` (a service predating the contract)
with `--relax-version`.

| Surface | Command / path | Format |
|-------------|----------------|--------|
| CLI | `bin/brain/search.go "q" --json`, `get.go <id> --body --json`, `stats.go --json` | read contract |
| HTTP (:8630) | `GET /search?q=`, `GET /get?id=`, `GET /stats`, `GET /audit`, `GET /openapi.json` | read contract (+ raw graph: `/leafs`, `/edges`, `/path`) |
| MCP | `POST /mcp`: tools `search` / `get` / `audit` (minimal surface, D20; `stats`/`ingest` — HTTP-only) | read contract |
| gates | `bin/brain/read-contract.go`, `read-contract-db.go` (read-only) | schema validation |

The normative formats (search/get/stats/audit, absence → absence,
`results` always an array) — [`docs/brain/read-contract.md`](read-contract.md).

### 4.4 Current DB state (after a full rebuild, 2026-09-03)

Measurements on the live DB (`/stats`, `/audit` of the :8630 service and the read-only
`bin/brain/audit-contract.go`):

| Metric | Value |
|---------|----------|
| total leafs | **105,220** |
| info | **105,199** = mail 105,027 + docs 172 |
| facts | **21** = 19 docker/compose + 2 ssh-host (all `confidence: confirmed`, two-source) |
| root × confidence | facts 21 confirmed; info 105,199 confirmed |
| ANN | 105,220 (loaded, `var/state/vector.ann`, dim=256) |
| contract conformance | observed_at/how/loc/kind: 0 gaps; `external_id` is absent from exactly 21 facts sheets (the legal facts write path) |

Context for the numbers: the full rebuild on 2026-09-02 (P-9.3 content-address dedup) collapsed
307,431 streamed mail sheets → 105,027 unique (live `var/corpus/mail` +
legacy `var/mail` collapsed by ContentHash, #248). The previously
documented reference points are **withdrawn**: "info ≥ ~200k" and "~313k leafs"
(see §7). Between the read-contract gate on 2026-09-02 (105,210) and the measurement
on 2026-09-03 the DB grew by 10 docs sheets; the current numbers above are normative.

## 5. Boundaries: what the brain does NOT do

- **Inspector/auditor, not executor** (#52): 2dph does not write the state of the
  job pipeline/cv/CRM, does not send messages, does not change sources. It
  filters, searches, audits; its only write is its own canon
  (leafs/facts/conclusions) in `var/kb.lbug` (+ graph layers Commit/Person/Message).
- **One canon per data type** (ADR-0012): data → gator, business structure
  → OnlyOffice CRM, rules → inventar/skills. 2dph **references**, does not
  copy; import into the graph (mail/git) — premises/links, not a canon duplicate.
- **Deduction does not go beyond the premises** (boundaries of the L-9.4/L-9.5 recipes):
  "what they did" by message/files — a hypothesis, not a fact about intent; 1 commit ≠
  "the whole week on X"; email aliases are not merged (joining strictly by
  email); transitive chains "friends of friends" — outside the pilot (depth=1);
  the mail↔repo binding is not invented.
- **facts without inventions**: in `root=facts` only two-source claims;
  there are no vacancy facts (SoT OPEN-0001) until ≥2 sources confirm them.
- **Responsibility for confirmation**: the agent does not present info/a single
  source as a fact — the `(not confirmed)` marker is mandatory; the `web` block is not
  mixed with graph hits.

## 6. Consumer map

| Consumer | Surface | Contract / tool |
|-------------|-------------|----------------------|
| agents (PicoClaw gateway, `cv/ai-bot`) | MCP `search → get → audit` (`POST /mcp`, :8630) | read contract (P-9.4); skills `picoclaw`, `brain` |
| gator and future service clients | HTTP/CLI `--json` | read contract; P-9.5 (#241) clients on top of it; gator's write — its own contract `contract.Record` (G-8.0, #73) |
| cv / job pipeline | `search → get → audit`, `/stats` | read contract + audit recipes (#52) |
| stack scripts | CLI `bin/brain/*.go --json` | read contract (version `1.0`); gates — `read-contract*.go` |
| operator/auditor | `bin/brain/*`, `bin/facts/*`, `audit-card.go` | write contract (writing), read contract, SoT, L-9 |

All clients must: accept MAJOR-compatible responses, ignore
unknown fields, mark non-facts answers `(not confirmed)` and not parse
`var/kb.lbug` directly.

## 7. What changed against what was documented earlier

- **The "info ≥ ~200k" rule is withdrawn** (it was in `skills/brain/SKILL.md`): after
  the full rebuild on 2026-09-02 the current reference point is — **total 105,220** =
  info 105,199 + facts 21 (see §4.4). Less → the corpus is under-written.
- **The "~313k leafs" reference point is withdrawn**: the P-9.3 content-address dedup collapsed
  mail duplicates (307k → 105k). The historical 313k remain only as
  context in the runbook (ANN parameter measurements #192/#206).
- Old snapshots in `docs/brain/contract.md` ("as of P-9.3", 313,817
  leafs, "not rebuilt yet") describe the state **before** the rebuild; the current
  numbers are §4.4 of this ADR.

## 8. Cross-ref

- Write contract: [`docs/brain/contract.md`](contract.md) (P-9.2/#235,
  P-9.3/#13) + `internal/contract` (Leaf, ContentHash, adapters).
- Read contract: [`docs/brain/read-contract.md`](read-contract.md)
  (P-9.4/#240) + `internal/contract/read.go` (`contract_version 1.0`).
- Audit: [`docs/facts/audit-recipes.md`](../facts/audit-recipes.md) (#52/#56),
  L-9 (#229–234), SoT `var/audit/source-of-truth.yml` (L-9.1 #230),
  `bin/facts/audit-card.go` (#231), formal checks (#232).
- Facts layer: `bin/facts/extract.go`, `docs/facts/ingest.md`, rebuild —
  `docs/brain/rebuild.md`.
- Design/deduction: `docs/design.md` (D12/D16/D20/D24), skill `brain`
  (`skills/brain/SKILL.md`), `docs/picoclaw.md` (agent profile).
- Zones/canons: ADR-0012 (zone responsibility), gator G-8.0 (#73) —
  data write contract; P-9.5 (#241) — service clients.
