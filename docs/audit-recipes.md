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

Read-only Postgres (OnlyOffice, when the VM is up):

```bash
bin/db/ssh-tunnel                       # 127.0.0.1:5433 -> vm:5432
bin/db/psql-yq --profile onlyoffice -s document_asset   # columns
bin/db/psql-yq --profile onlyoffice -c 'SELECT ...'     # read-only query -> YAML
```

Profile lives in `~/.config/brain/db-profiles.yml` (secrets never in the repo).
Blocked on #53.

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

- mail: `var/mail/md` (M365 sync → `bin/mail/sync.go` → `brain/index --with-mail`)
- telegram/n chat: `var/chats` (`bin/chats/sync.go n|linkedin`)
- corpus: `cv/`, `projects/knowledge-mesh-seed.yaml`

```bash
bin/brain/get.go <id> --body     # source + loc
test -f "$KB_ROOT/<loc>"         # artifact exists on disk
```

Claims whose source points at a missing artifact or a chat thread that does not
support the claim are downgraded to `(not confirmed)` pending re-audit.

## Test plan (on one known deal)

1. `search` a real dossier → `get` the top leaf → confirm `source` has two refs.
2. Feed a deliberately conflicting claim pair to `audit contradict` → expect
   `(not confirmed)` (no rule fires).
3. Confirm the operator flow above is usable by the `cv/ai-bot` assistant (docs
   + `/mcp`).