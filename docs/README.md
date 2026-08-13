---
type: reference
status: current
related:
  - docs/runbook.md
  - docs/design.md
  - PLAN.md
---

# 2dph docs (Diataxis)

Evidence-first knowledge graph. Facts need proof or they are
`(not confirmed)`.

| Type | Doc |
|------|-----|
| tutorial / howto | [runbook](runbook.md) — run anywhere (uv, Go, Docker) |
| explanation | [design](design.md) — two roots, deduction, D17/D20/D18 |
| howto | [picoclaw](picoclaw.md) — MCP agent profile |
| howto | [reasoner](reasoner.md) — CPU bake-off (D18) |
| reference | [PLAN.md](../PLAN.md) — decisions D1–D21 |

Decisions the public face must name: **D3** SearXNG compose, **D6** Go service /
Python write sidecar, **D14** `bin/{subject}/{method}.go`, **D15** Gitea origin,
**D17** assertion gate (facts → info → web), **D18** pluggable reasoner.

Search: `bin/brain/search.go "query"` (HTTP: `bin/brain/serve.go` —
`/health` `/search` `/get` `/stats` `/audit` `/ingest`). `--hop` is
not a walk; the flag errors until File/FROM_FILE edges exist.

Work board: [Gitea issues](https://git.produktor.io/eSlider/2dph/issues).
PRs and CI: GitHub [`eSlider/2dph`](https://github.com/eSlider/2dph).

Published docs live here and match live commands.
