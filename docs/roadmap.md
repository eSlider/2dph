---
type: explanation
status: current
related:
  - PLAN.md
  - docs/design.md
  - docs/runbook.md
---

# Gap to v1 — detective brain

Goal: a brain that does not assert without proof. Search is deduction
(`facts` ≥2 sources → `info` → `web`). `confirmed` only from the facts root.

**v1 is a living graph the agent can write and walk**, not “more RAG”.

Epic: [Gitea #16](https://git.produktor.io/eSlider/2dph/issues/16).
Milestone: [v1 detective brain](https://git.produktor.io/eSlider/2dph/milestone/12).
Decisions: [PLAN.md](../PLAN.md).

## In (do not reopen)

Read path Go + Zig CGO (D21). HTTP + OpenAPI + MCP (D20). PicoClaw compose
profile + CPU reasoner (D18). Mail sync → import → rebuild. D14 shebangs.
Compose `api` (no CPython) / `index` (Python write). Issues #1–#5, #7–#13.
[#15](https://git.produktor.io/eSlider/2dph/issues/15) lever/loop.
[#14](https://git.produktor.io/eSlider/2dph/issues/14) `bin/brain/add.go` /
`POST /ingest` (Python `kblib.add_leafs`; no Go upsert port).

## Blockers

```
question
    │
    ├─ FTS + HNSW          ← in
    ├─ facts / info roots  ← in
    ├─ web (D17)           ← in
    ├─ brain/add ACID      ← in
    ├─ Cypher hop          ← #17  schema yes, search no
    └─ facts+chats corpus  ← #18
```

1. **[#17](https://git.produktor.io/eSlider/2dph/issues/17) hops** —
   `Leaf-[:FROM_FILE]->File-[:HAS_VERSION]->Commit-[:AUTHORED]->Person`
   exists; `--hop` still errors. Without a walk, D9/D10 are paper.
2. **[#18](https://git.produktor.io/eSlider/2dph/issues/18) corpus** —
   rebuild loads repo markdown + mail as `info`. `facts/extract` pairing
   and `bin/chats` are not indexed. WhatsApp is a stub. PII stays in `var/`.
3. **[#19](https://git.produktor.io/eSlider/2dph/issues/19) CI eval** —
   recall SoT should be `bin/brain/eval.go` via Zig, not Python `bin/kb/eval`.

## Not v1

[#6](https://git.produktor.io/eSlider/2dph/issues/6) OCR (OQ2), OQ1
contradiction resolution, OQ3 duckdb-md export, OQ4 YAML-first leafs.

## Close epic #16 when

- `--hop` stops erroring and runs a Cypher path from search hits
- ops pairing + chat import land as leafs on rebuild
- MCP tool order is documented and still gated by tests
