# brain HTTP / MCP tools

Generated from `pkg/httpapi.Ops`. Do not edit by hand.

Serve: `bin/brain/serve.go` (`GET /openapi.json`, `POST /mcp`).

- `search` — deduction search (facts → info → web)
- `get` — read one leaf by id
- `audit` — facts confidence histogram
- `leafs` — query leafs by root/type/source/text
- `edges` — adjacency: synapses of one leaf
- `addedge` — add a synapse (leaf A → leaf B)
- `path` — shortest path between two leafs
