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
[#17](https://git.produktor.io/eSlider/2dph/issues/17) `--hop N` walks
FROM_FILE / HAS_VERSION / AUTHORED.
[#18](https://git.produktor.io/eSlider/2dph/issues/18) `--with-facts` /
`--with-chats` on rebuild (WhatsApp out of v1).
[#19](https://git.produktor.io/eSlider/2dph/issues/19) CI recall SoT =
`bin/brain/eval.go` via Zig.
Epic [#16](https://git.produktor.io/eSlider/2dph/issues/16) closed.

## v2

[#6](https://git.produktor.io/eSlider/2dph/issues/6) OCR — **in**.
[#30](https://git.produktor.io/eSlider/2dph/issues/30) OQ3 duckdb-go — **in**.
[#29](https://git.produktor.io/eSlider/2dph/issues/29) OQ1 contradiction
resolution.

## Blockers

None for epic #16 (closed). Remaining v2: OQ1, OQ4.

```
question
    │
    ├─ FTS + HNSW          ← in
    ├─ facts / info roots  ← in
    ├─ web (D17)           ← in
    ├─ brain/add ACID      ← in
    ├─ Cypher hop          ← in
    └─ facts+chats corpus  ← in
```

## Not v1

OQ1 contradiction resolution, OQ4 YAML-first leafs.
OCR (OQ2) and duckdb-go (OQ3/D22) are in.

## Close epic #16 when

Children #14, #15, #17, #18, #19 are closed. MCP tool order stays gated by tests.
