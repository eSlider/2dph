# 2dph — deductionphile

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Python](https://img.shields.io/badge/Python-3.12+-3776AB.svg)](https://python.org)
[![uv](https://img.shields.io/badge/uv-managed-261230.svg)](https://docs.astral.sh/uv)
[![Tests](https://github.com/eSlider/2dph/actions/workflows/ci.yml/badge.svg)](https://github.com/eSlider/2dph/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/tag/eSlider/2dph?sort=semver&label=release)](https://github.com/eSlider/2dph/releases)
[![GitHub Stars](https://img.shields.io/github/stars/eSlider/2dph?style=social)](https://github.com/eSlider/2dph/stargazers)

An evidence-first brain. **Facts need two independent sources, or they are
`(not confirmed)`.** Cursor is not the runtime.

`2dph` is a single embedded knowledge graph (LadybugDB) with native **HNSW
vector** + **BM25 full-text** indexes. Search is *deduction*: confirmed facts
first, supporting info second, `web-search` as the independent second source
when the local graph cannot confirm.

Run it: [docs/runbook.md](docs/runbook.md). Design: [docs/design.md](docs/design.md).
Docs index: [docs/README.md](docs/README.md).

## Architecture

```mermaid
graph TB
    subgraph corpus["Corpus"]
        OPS["ops stack<br/>chat · onlyoffice · npm · observability · ai-bot"]
        SSH["~/.ssh/config"]
        CV["portfolio yaml"]
        GH["git history · authors"]
    end

    subgraph dph["2dph tools"]
        EX["bin/facts/extract.go<br/>2-source pairing"]
        AU["bin/facts/audit.go<br/>confidence + staleness"]
        IDX["bin/brain/index.go<br/>chunk + embed"]
        MD["bin/markdown/import.go<br/>H2 leaf split"]
        SR["bin/brain/search.go<br/>deduction"]
    end

    subgraph store["Ladybug var/kb.lbug"]
        FACTS["facts root<br/>confirmed"]
        INFO["info root<br/>narrative"]
        VEC["HNSW cosine"]
        FTS["BM25 FTS"]
        GR["File→HAS_VERSION→Commit→AUTHORED→Person"]
    end

    subgraph ai["AI"]
        M2V["model2vec<br/>potion-multilingual-128M"]
    end

    subgraph ext["External"]
        WS["web-search skill<br/>(2nd independent source)"]
    end

    OPS --> EX
    SSH --> EX
    CV --> EX
    GH --> EX
    EX --> FACTS
    EX --> INFO
    MD --> GR
    MD --> INFO
    IDX --> M2V
    IDX --> VEC
    IDX --> FTS
    IDX --> FACTS
    SR --> FACTS
    SR --> INFO
    SR --> VEC
    SR --> FTS
    SR --> WS
    AU --> FACTS
```

## The method

Every assertion is `Who / What / How / Where / When + evidence + confidence`,
mirroring the detective method: **≥2 independent sources confirm a
fact; conflicting sources or a single source → `hypothesis` → `(not confirmed)`.**

| root | meaning | used for answers |
|------|---------|------------------|
| `facts` | assertions backed by ≥2 sources (`confirmed`) | yes, with evidence links |
| `info` | descriptive/narrative leafs (how-tos, notes) | context only, marked `(not confirmed)` |

## Deduction search

```bash
bin/brain/search.go "Matrix federation over HTTPS"   # facts → info → web
bin/brain/search.go "onlyoffice postgres" --root facts
bin/brain/search.go "where is cs-lexicon" --json | yq '.'
bin/brain/search.go "upstream flag" --no-web         # local graph only
bin/brain/get.go <id> --body                         # full chunk on demand
bin/brain/stats.go                                   # index health
bin/brain/eval.go                                    # recall@5 gate
```

`--hop N` walks File/Commit/Person from each hit (max 3). `bin/kb/search` is a deprecated wrapper around `bin/brain/search.go`.

Git history is read with [go-git](https://github.com/go-git/go-git) (no git binary):

```bash
bin/git/import.go --json --limit 100              # commit leafs for this repo
bin/git/import.go --root "$PROJECTS_ROOT" --json  # one pass per .git under root
```

Conversion only. Graph write (`File-[:HAS_VERSION]->Commit-[:AUTHORED]->Person`) stays with `bin/brain/index.go`.

Web search (second independent source) goes through SearXNG. Empty results mean **throttled**, not “nothing exists”:

```bash
bin/web/search.go "LadybugDB vector index" --json
# Optional local instance (skip if BRAIN_SEARCH_URL already points at one):
# SEARXNG_SECRET=$(openssl rand -hex 32) docker compose --profile searxng up -d
```

Mail is a first-class corpus (retrievable through the same search):

```bash
bin/mail/sync.go --source onlyoffice,gmail --workers 8 --out var/mail  # raw sync (Go)
bin/mail/import.go --from-raw var/mail                                  # JSON → markdown
bin/brain/add.go --text T --root facts --source "a.md x b.md"
bin/brain/index.go --rebuild                                            # rebuild brain (incl. mail)
bin/brain/search.go "invoice from last week"                            # same search over mail leafs
```

## Storage

- **LadybugDB** — single `var/kb.lbug`, Cypher + HNSW + BM25, embedded.
  Read tools (`get` / `stats` / `eval`) are Go + Zig CGO (`bin/cgo/zcc`).
  Python fallbacks stay for CI until the runner fetches Zig. Incremental
  write is `bin/brain/add.go` (Python `kblib.add_leafs`). Bulk rebuild is
  Compose profile `index` (`bin/brain/index.go --rebuild`).
- **model2vec** — `potion-multilingual-128M` (256-dim), CPU, no Ollama
  runtime dependency.
- facts and info split by `root` but written in the same transaction.

Ladybug 0.19 DROP INDEX warning: [docs/runbook.md](docs/runbook.md).

## Tooling conventions

`bin/{subject}/{method}.go` — self-describing: shebang on line 1, usage comment
from line 2. Shared code in `internal/`. YAML default output, `--json` for
machines. Tests gate every commit. HTTP: `bin/brain/serve.go` calls
`internal/brain` in-process (`/health` `/search` `/get` `/stats` `/audit` `/ingest` `/openapi.json` `/mcp`).

## Development

See the portable runbook: [docs/runbook.md](docs/runbook.md).

```bash
uv venv .venv
uv pip install -r requirements.lock.txt
bin/facts/audit.go self
go test ./... && uv run python -m unittest discover -s bin/tools -t .
```

Docker (optional, cached model + var volumes):

```bash
docker compose up -d brain                     # API (Zig CGO serve :8630)
docker compose --profile index run --rm index  # Python Ladybug rebuild
docker compose --profile picoclaw up brain-mcp # MCP on 127.0.0.1:8630
docker compose --profile reasoner up -d reasoner  # CPU Ollama 127.0.0.1:11435
docker compose up brain-watch                  # auto re-index on change
```

## Related

- [go-second-brain](https://github.com/eSlider/go-second-brain) — the earlier
  Neo4j + Qdrant + Matrix RAG brain
- [agent-skills](https://github.com/eSlider/agent-skills) — upstream
  skills (`web-search`, `postgres`, …) that 2dph integrates
- detective method — the two-source method

Work board (issues): [epic #16](https://git.produktor.io/eSlider/2dph/issues/16)
on [git.produktor.io/eSlider/2dph/issues](https://git.produktor.io/eSlider/2dph/issues).
PRs and CI: GitHub [`eSlider/2dph`](https://github.com/eSlider/2dph).

See [PLAN.md](PLAN.md) for decisions, [docs/roadmap.md](docs/roadmap.md) for
the gap to v1, and v2 open questions.
