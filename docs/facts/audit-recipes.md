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
bin/facts/audit.go formal                                           # L-9.3 URL-formal checks (stdin JSON)
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

Every verdict of any recipe below is recorded as a Vinogradov card in the SoT
`audits[]` (`var/audit/source-of-truth.yml`), not "on its own": a recommendation
without a card = a violation. A card must rest on FACT-/OPEN- premises.

```bash
# premises/gaps: the flag is repeatable or values are comma-separated.
# inference: deduction | induction | analogy | other; gaps are optional.
# counter: none or a counter-argument; verdict: only accept | reject | weaken.
bin/facts/audit-card.go \
  --claim "demo2 is confirmed by a second source" \
  --premises FACT-9002,FACT-9001 \
  --inference deduction \
  --gaps OPEN-0001 \
  --counter none \
  --verdict weaken
```

The CLI validates the card (verdict/inference enum, claim/premises/counter are
required), assigns the next `AUD-NNNN`, appends it to `audits[]` and
prints the card. The remaining SoT sections and comments are not touched
(editing the yaml tree). Formal inference logic — L-9.3 (#232).

## Formal URL checks (L-9.3 #232)

Machine checks of the Vinogradov laws at the URL level — on top of the empirics of #56.
The core is `internal/facts` (cgo-free, `CanonicalURL` + `CheckFormal`):
a judgement = a fact about a URL resource: `{id, url, claim, attr, neg, conf, from, to}`,
where `attr` is the predicate, `neg` is the sign (P/¬P), `conf` is the leaf's confirmation.
Rules:

| Law | Code rule | Flag |
|-------|-------------|------|
| identity | one canonical URL in different spellings → merge/flag | `weaken` |
| contradiction | confirmed `P` and `¬P` about one predicate of one URL with overlapping D24 intervals | `reject` |
| excluded middle | hypothesis/partial or a claim without a predicate — not a FACT, a candidate for `open_questions[]` | `weaken` |
| sufficient reason | verdict `accept` only with FACT-/OPEN- premises; otherwise reject/weaken | `reject` |

Example — two facts on one URL with contradictory judgements ("the vacancy
is active" vs "the vacancy is closed"):

```bash
cat <<'EOF' | bin/facts/audit.go formal
{"facts":[
  {"id":"FACT-01","url":"https://jobs.example.com/vacancy/42","claim":"vacancy is active","attr":"vacancy is open","neg":false,"conf":"confirmed"},
  {"id":"FACT-02","url":"https://jobs.example.com/vacancy/42","claim":"vacancy is closed","attr":"vacancy is open","neg":true,"conf":"confirmed"}
]}
EOF
# → contradiction verdict=reject; the equivalent card is written by audit-card.go
```

URL canonicalization — a local implementation of the gator G-8.1 convention
(`facts.CanonicalURL`: lowercase scheme/host, default port, utm_*/ref/gh_jid/
token query params, query sorting, no fragment/trailing slash); the gator module is not
imported. The sufficient-reason check over cards runs through the same
subcommand: `echo '{"cards":[{…}]}' | bin/facts/audit.go formal` — accept without
FACT-/OPEN- premises is returned as a violation (reject/weaken).

## Recipes

### 0. Git commits: "who worked on what, when" (L-9.4 #233)

The graph layer Commit/Person/AUTHORED (written by `bin/git/graph.go` from the git history
of the git.produktor.io repos, commit id canon = `repo:sha`) → deductive facts
`bin/git/facts.go` (read-only). Vinogradov deduction: the premise is a commit list
(`commit <sha> in <repo>`, `Author`, `Date`, subject/files) → the conclusion "<author>
worked on <repo> on <date>".

```bash
bin/git/graph.go --repo /x/2dph --dry-run        # how many commits/Persons before writing
bin/git/graph.go --repo /x/2dph --commit         # MERGE into kb.lbug (idempotent)
bin/git/facts.go --repo 2dph --since 2026-01-01  # fact cards
bin/git/facts.go --author a@b.c --commits --json # detail + audit cards
```

**Audit card template (commit fact):** claim from repo+period of the actual commit
dates; premises — commit ids (`repo:sha`, the first 5 + a counter of the remainder);
inference = `deduction`; verdict:

- `accept` — the subjects are substantive ("feat: mesh node", "fix: typo") → "what was
  done" as a hypothesis from message/files;
- `weaken` — the whole subject set is empty/service ("Update", WIP) → only
  "touched files in <repo>" is derived, not "what exactly"; gap `OPEN`;
- a partially weak set → `accept` + gap `OPEN` about the number of weak commits.

**Boundaries (otherwise a fallacy):**
- "what was done" — a hypothesis from message/files, NOT a fact about intent;
- authorship by the commit's `Author:`, not necessarily the sole author: merge
  commits are not written to the graph (gitlog.Log skips them), a co-author in the
  message body is invisible — OPEN;
- 1 commit ≠ "the whole week on X": the period is taken from the actual commit dates,
  no hasty induction is built;
- Leaf commits (premises, source=git) are not bloated — the graph is written as a
  separate layer Commit/Person/AUTHORED.

### 0a. Relationship network "who knows whom through whom" (L-9.5 #234)

The mail graph (Message/Person + SENT/TO/CC/BCC/REPLY_TO, D-1 #257) + git
(Commit/Person/AUTHORED, L-9.4 #233) → Person↔Person links with premises and
verdict; read-only tool `bin/network/network.go`.

```bash
bin/network/network.go --person eslider@gmail.com                # top links
bin/network/network.go --person eslider@gmail.com --json         # full JSON
bin/network/network.go --person alice@x --project demo --since 2026-01-01
bin/network/network.go --person alice@x --accept-only            # CRM export (YAML)
```

**Derivations** (from existing nodes/edges only): mail sender↔recipient
(weight TO 1.0/CC 0.5/BCC 0.25), shared recipient, REPLY_TO dialogue, threads
(thread_id), shared project (AUTHORED Commit.repo). Premises — Message.id /
Commit.id `repo:sha`.

**Audit card template (link fact):** claim "Q is connected to target: N mails, M
threads, period D"; inference = `deduction`; verdict:

- `accept` — ≥2 direct mails, or ≥1 REPLY_TO dialogue, or a shared project
  (both sides AUTHORED in repo) → export to CRM (ADR-0012 item 4);
- `weaken` — a single contact (1 mail without a reply and without a project) or
  only a shared recipient → gap `OPEN`, not exported to CRM (ADR-0012
  item 2 "1 mail ≠ a durable link").

**Boundaries (otherwise a fallacy):** email aliases are not merged (matching is strictly
by email); "what was discussed" is not derived from the mail body (Paragraph is not
materialized); transitive chains of "acquaintances of acquaintances" are outside the pilot
(depth=1); the mail↔repo binding is not invented (there is no such edge in the graph).
Design: docs/brain/graph-network.md.

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