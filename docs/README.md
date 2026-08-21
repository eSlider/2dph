---
type: reference
status: current
related:
  - docs/runbook.md
  - docs/design.md
  - PLAN.md
  - docs/roadmap.md
---

# 2dph docs (Diataxis)

Evidence-first knowledge graph. Facts need proof or they are
`(not confirmed)`.

| Type | Doc |
|------|-----|
| tutorial / howto | [runbook](runbook.md) — run anywhere (uv, Go, Docker) |
| explanation | [design](design.md) — two roots, deduction, D17/D20/D18 |
| explanation | [roadmap](roadmap.md) — gap to v1 (epic #16) |
| howto | [picoclaw](picoclaw.md) — MCP agent (`bin/stack/start-assistant`) |
| howto | [reasoner](reasoner.md) — CPU bake-off (D18) |
| reference | [PLAN.md](../PLAN.md) — decisions D1–D24 |
| testing | [test/README.md](../test/README.md) — system / stress / integration tiers |
| config | `etc/{searxng,picoclaw}` — operator config (FHS) |

Decisions the public face must name: **D3** SearXNG compose, **D6** Go service /
Python write sidecar, **D14** `bin/{subject}/{method}.go`, **D15** Gitea origin,
**D17** assertion gate (facts → info → web), **D18** pluggable reasoner.

Search: `bin/brain/search.go "query"` (HTTP: `bin/brain/serve.go` —
`/health` `/search` `/get` `/stats` `/audit` `/ingest`). `--hop N` walks
`FROM_FILE` → Commit → Person from each hit (max 3). Rebuild writes
File edges ([#17](https://git.produktor.io/eSlider/2dph/issues/17)).

Work board: [Gitea issues](https://git.produktor.io/eSlider/2dph/issues)
([epic #16](https://git.produktor.io/eSlider/2dph/issues/16)).
PRs and CI: GitHub [`eSlider/2dph`](https://github.com/eSlider/2dph).

Published docs live here and match live commands.
