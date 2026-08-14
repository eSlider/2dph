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
bin/brain/add.go --text T --root facts --source "a.md x b.md"
bin/brain/get.go <id> --body                           # full chunk only when needed
bin/brain/stats.go                                     # index health
bin/brain/eval.go                                      # recall@5 >= 0.95 gate (Go; Python bin/kb/eval is CI fallback)
```

`bin/kb/search` is a deprecated wrapper. `--hop N` walks
`FROM_FILE` / `HAS_VERSION` / `AUTHORED` from each hit (1=File, 3=Person).

## Rules

- Search before you read. Never grep a repo for a concept the graph covers.
- `--root facts` returns only confirmed evidence-linked answers. Default shows
  facts first, then info leafs clearly marked `(not confirmed)`.
- If there is no facts hit, `bin/brain/search.go` consults SearXNG and adds a
  `web` block (kept apart from graph hits). `throttled` / `skipped` / `refused`
  are not evidence of absence. `--root facts|info` and `--no-web` skip the web.
- If recall looks wrong, run `bin/brain/eval.go`; it gates control questions and
  should stay at or above 95% recall@5.
- Agents: `GET /openapi.json` and `POST /mcp` on `bin/brain/serve.go` (same
  handlers; tool names match paths `search`/`get`/`stats`/`audit`). Generated
  list: [tools.md](tools.md).
- Never report an unconfirmed single-source local answer as fact.