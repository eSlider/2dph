# 2dph (deductionphile)

Evidence-first knowledge graph + hybrid RAG over the operational
Brain/ops/eSlider stack. Facts need proof or they are
`(not confirmed)`.

- [PLAN.md](../PLAN.md) — decisions, execution order, open questions (v2)
- [design](design.md) — schema, deduction model, sources
- [Gitea issues](https://git.produktor.io/eSlider/2dph/issues) — work board (origin)

Search: `bin/brain/search.go "query"` (HTTP: `bin/brain/serve.go` —
`/health` `/search` `/get` `/stats` `/audit` `/ingest`). `--hop` is
not a walk; the flag errors until File/FROM_FILE edges exist.

Published docs live here and mirror the project state.
