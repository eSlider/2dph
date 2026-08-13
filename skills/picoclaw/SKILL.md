---
name: picoclaw
description: >-
  2dph is the memory/fact gate, not the agent loop. Use when wiring PicoClaw
  or any MCP client: call brain search/get/audit before a factual reply.
  throttled is not a negative finding.
---

# PicoClaw — fact-check before assert

PicoClaw (or any agent) speaks MCP at `POST /mcp` on `bin/brain/serve.go`.
2dph does not run the agent loop. Compose: `docker compose --profile picoclaw up brain-mcp`
(see [docs/picoclaw.md](../../docs/picoclaw.md)).

## Tool order (before a factual reply)

1. **`search`** — facts root first, then info. The `web` block is a second
   source when there is no facts hit. Status `throttled` / `skipped` /
   `refused` is **not** evidence of absence.
2. **`get`** — full leaf body only when a hit `id` is needed.
3. **`audit`** — if recall or confidence looks wrong.

Then answer. Confirmed only from facts (≥2 independent sources). Anything
else is `(not confirmed)`. Missing graph ≠ “does not exist”.

Generated tool list: [../brain/tools.md](../brain/tools.md).
