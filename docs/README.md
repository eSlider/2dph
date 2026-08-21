---
type: reference
status: current
related:
  - docs/runbook.md
  - docs/design.md
  - PLAN.md
---

# 2dph docs

Evidence-first knowledge graph. Facts need proof or they are
`(not confirmed)`.

**Docs policy:** only what is true and current — what works, or what is
open (tracked as Gitea issues). Plans and historical proofs live as Gitea
issue comments, not here.

| Type | Doc |
|------|-----|
| howto (run) | [runbook](runbook.md) — build, config, serve/search/index |
| explanation | [design](design.md) — two roots, deduction, versioning, read path |
| reference | [PLAN.md](../PLAN.md) — decisions D1–D25 |
| howto (brain ops) | [brain/rebuild](brain/rebuild.md) — parallel write, resume |
| howto (facts) | [facts/audit-recipes](facts/audit-recipes.md) — audit recipes |
| howto (agent) | [picoclaw](picoclaw.md) — MCP gateway (`bin/stack/start-assistant`) |
| howto (reasoner) | [reasoner](reasoner.md) — CPU bench (D18) |
| testing | [test/README.md](../test/README.md) — system / stress / integration tiers |
| config | `etc/{searxng,picoclaw}` — operator config (FHS) |

Historical: roadmap (gap to v1, closed epic #16) kept for context;
load baseline 2026-08-11 → Gitea #58.

## Quick start

```bash
bin/brain/search.go "query"          # deduction: facts → info → web
bin/brain/serve.go                   # HTTP + OpenAPI + MCP (:8630)
bin/brain/index.go --rebuild         # bulk rebuild (Zig CGO)
```

MCP surface: `search` / `get` / `audit`. Full OpenAPI: `GET /openapi.json`.

## Work board

[Gitea issues](https://git.produktor.io/eSlider/2dph/issues):
epic #66 (import → brain + CRM, priority), #62 (cash sprint),
#64 (conversations v2), milestone [v2](https://git.produktor.io/eSlider/2dph/milestone/13).
PRs and CI: GitHub [`eSlider/2dph`](https://github.com/eSlider/2dph).
