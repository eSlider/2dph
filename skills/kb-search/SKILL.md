---
name: kb-search
description: >-
  Deduction search over the 2dph brain (Ladybug graph: ops corpus, portfolio,
  ssh hosts) with bin/kb/search instead of reading files or grepping repos.
  Use whenever a question starts with "where is", "what runs on", "which file
  describes", "who is", "how is X done", before opening any documentation.
---

# kb-search — deduction over facts and info

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
bin/kb/search "Matrix federation"                # pointers + snippets, YAML
bin/kb/search "what runs on arc-2" --hop 1       # follow graph edges
bin/kb/search "onlyoffice postgres" --root facts # restrict to confirmed
bin/kb/search "where is cs-lexicon" --json | yq '.[].ref'
bin/kb/get <id> --body                           # full chunk only when needed
bin/kb/stats                                     # index health
bin/kb/eval                                      # recall@5 >= 0.95 gate
```

## Rules

- Search before you read. Never grep a repo for a concept the graph covers.
- `--root facts` returns only confirmed evidence-linked answers. Default shows
  facts first, then info leafs clearly marked `(not confirmed)`.
- If recall looks wrong, run `bin/kb/eval`; it gates control questions and
  should stay at or above 95% recall@5.
- `--hop N` follows sibling leaves, owning files, `related:` links and
  vector-neighbours — that is the deduction walk, not random expansion.
- Escalate to `web-search` (the `web-search` skill) as the independent second
  source when both local roots cannot confirm; never report an unconfirmed
  single-source local answer as fact.