---
name: picoclaw
description: >-
  2dph is the memory/fact gate. Compose runs the official PicoClaw gateway.
  Use when wiring PicoClaw or any MCP client: call brain search/get/audit
  before a factual reply. throttled is not a negative finding.
---

# PicoClaw — fact-check before assert

PicoClaw speaks MCP at `POST /mcp` on `bin/brain/serve.go`. Compose profile
`picoclaw` runs the official `sipeed/picoclaw` gateway plus `brain-mcp`
(see [docs/picoclaw.md](../../docs/picoclaw.md)).

```bash
scripts/stack/start-assistant     # brain + qwen3.5:9b + gateway + picoclaw agent
scripts/stack/status
scripts/stack/stop
```

## Tool order (before a factual reply)

1. **`search`** — facts root first, then info. The `web` block is a second
   source when there is no facts hit. Status `throttled` / `skipped` /
   `refused` is **not** evidence of absence.
2. **`get`** — full leaf body only when a hit `id` is needed.
3. **`audit`** — if recall or confidence looks wrong.

Then answer. Confirmed only from facts (≥2 independent sources). Anything
else is `(not confirmed)`. Missing graph ≠ “does not exist”.

Generated tool list: [../brain/tools.md](../brain/tools.md).
