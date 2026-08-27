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

## Recipes

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