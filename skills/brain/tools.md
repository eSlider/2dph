# brain HTTP / MCP tools

Generated from `internal/httpapi.Ops`. Do not edit by hand.

Serve: `bin/brain/serve.go` (`GET /openapi.json`, `POST /mcp`).

- `search` — deduction search (facts → info → web)
- `get` — read one leaf by id
- `stats` — index health
- `audit` — facts confidence histogram
- `ingest` — add a leaf without rebuild
