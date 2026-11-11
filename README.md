# 2dph — deductionphile

[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](https://opensource.org/licenses/MIT)
[![Go](https://img.shields.io/badge/Go-1.26+-00ADD8.svg)](https://go.dev)
[![Tests](https://github.com/eSlider/2dph/actions/workflows/ci.yml/badge.svg)](https://github.com/eSlider/2dph/actions/workflows/ci.yml)
[![Latest Release](https://img.shields.io/github/v/tag/eSlider/2dph?sort=semver&label=release)](https://github.com/eSlider/2dph/releases)
[![GitHub Stars](https://img.shields.io/github/stars/eSlider/2dph?style=social)](https://github.com/eSlider/2dph/stargazers)

An evidence-first brain. **A fact needs two independent sources, or it is
`(not confirmed)`.**

2dph is a single embedded knowledge graph (LadybugDB) with BM25 full-text and
vector search. One query runs a deduction pass: confirmed facts first,
supporting notes second, web search as the independent second source when the
local graph cannot confirm.

## What it solves

A normal RAG answer is fluent but unsourced, and an LLM will happily invent
the missing part. 2dph answers from a graph where a fact needs two independent
sources — and says `(not confirmed)` when it cannot.

| You ask | You get |
|---------|---------|
| “Where does the onlyoffice postgres run?” | the host and container, plus the sources that agree — or `(not confirmed)` if one source is gone |
| “When did the last invoice from this supplier arrive?” | the mail leaf with the message and its date; mail is a first-class corpus |
| “What is rule R-F14?” | the exact rule text — rulebooks and decisions are searchable reference leafs, not a paraphrase |
| “Who worked on this file?” | `--hop` walks File → Commit → Person, so history is a graph query, not a log grep |

The same HTTP/MCP `search` is what an agent calls before answering. The model
receives evidence-backed facts with source and confidence, not raw chunks, so
it cannot silently turn a guess into a fact.

## How it works

- **Two roots, one transaction** — `facts` (confirmed) and `info` (context)
  live in one ACID store, so the audit always sees a consistent snapshot.
- **Deduction read path** — `facts → info → web`; every hit carries evidence,
  confidence and freshness.
- **Evidence gate** — two-source pairing, contradiction and staleness checks.
- **Hybrid retrieval** — BM25 FTS + 256-dim model2vec embeddings, merged in
  the ranker.
- **Graph hops** — `--hop N` walks File → Commit → Person.

## Architecture

```mermaid
graph LR
    SRC["sources<br/>docs · mail · chat · git · contacts"]
      --> ADAPT["adapters → leafs"]
    ADAPT --> STORE["Ladybug — one kb.lbug<br/>graph · facts/info · BM25"]
    ANN["IVF ANN · 256-dim"] --- STORE
    STORE --> READ["deduction search<br/>facts → info → web"]
    READ --> API["HTTP · OpenAPI · MCP"]
    GATE["evidence gate<br/>2-source pairing"] --> STORE
```

## Why IVF, not HNSW

Ladybug's own HNSW index crashes once the graph grows (~1.3k nodes, #192), so
vector search lives **outside** liblbug. `coder/hnsw` was tried and rejected:
on 313k real embeddings only 34% of nodes stayed reachable and recall@5 fell
to 0.16.

The replacement is a small pure-Go **IVF** index (`internal/brain/ann`):
k-means cells plus exact cosine inside the probed cells, persisted as
`var/state/vector.ann` with an append-only WAL. It is updated incrementally,
rebuilt only when missing or more than 10% behind, and falls back to a linear
scan when absent or damaged — search never breaks.

Measured on 313,689 vectors: recall@5 vs exact scan = **1.0**, p50 ≈ **470 ms**
(#204/#206).

## Quick start

```bash
git clone https://github.com/eSlider/2dph && cd 2dph
bin/brain/index.go --rebuild                          # build the brain
bin/brain/search.go "Matrix federation over HTTPS"    # ask a question
bin/brain/serve.go                                    # HTTP + OpenAPI + MCP :8630
```

Incremental write is `bin/brain/add.go`; background indexing runs in the
compose `index-sync` cycle. Full setup: [docs/runbook.md](docs/runbook.md).

### Run with Docker

`ghcr.io/eslider/2dph` — the API image (HTTP + OpenAPI + MCP), `linux/amd64`
only.

```bash
docker pull ghcr.io/eslider/2dph:latest

docker run --rm -p 127.0.0.1:8630:8630 \
  -v "$PWD/var:/data/var" \
  -e HOME=/data -e KB_HOST=0.0.0.0 -e KBSEARCH_NO_DAEMON=1 \
  --user "$(id -u):$(id -g)" \
  ghcr.io/eslider/2dph:latest serve

curl http://127.0.0.1:8630/health  # {"status":"ok"}
```

| Surface | URL / path |
|---------|-------------|
| API, OpenAPI | `http://127.0.0.1:8630` · `/openapi.json` |
| MCP | `http://127.0.0.1:8630/mcp` |
| Data | `./var` (`kb.lbug`, `state/vector.ann`) |

The image carries no `kb.lbug`, so mount a `var/` built with
`bin/brain/index.go --rebuild`; `--user` keeps the host uid as the DB owner
(the DB is opened read-write, same rule as `KB_UID` in `compose.yaml`).

Tags: `latest` = last stable release · `vX.Y.Z` = pinned release · `api` = edge
of `main` · `sha-<12>` = one commit — which one to use, and how to read the
image version: [docs/runbook.md#docker-images-and-tags](docs/runbook.md#docker-images-and-tags).

## What's inside

| Path | Role |
|------|------|
| `bin/brain` | search · index · serve |
| `bin/facts` | extract · audit — the evidence gate |
| `bin/mail`, `bin/chat`, `bin/git`, `bin/web` | corpus adapters |
| `internal/brain` | store, rank, ANN, FTS |
| `scripts/stack` | compose dispatcher |

## Docs

[runbook](docs/runbook.md) · [design](docs/design.md) ·
[docs index](docs/README.md) · [decisions](PLAN.md).

## Related

- [go-second-brain](https://github.com/eSlider/go-second-brain) — the earlier
  Neo4j + Qdrant + Matrix RAG brain
- [agent-skills](https://github.com/eSlider/agent-skills) — upstream skills
  (`web-search`, `postgres`, …) that 2dph integrates
