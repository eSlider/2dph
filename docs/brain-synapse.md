# Brain Synapse Matrix — service for pc-agent (issue #82)

The 2dph brain (`internal/brain`: leafs + edges) as an HTTP service. Metaphor:
**leafs = neurons**, **SYNAPTIC edges = synapses**. pc-agent reads the graph and
writes new observations as leafs with `source=pc-agent`.

## Startup

```bash
# loopback only, port 8632 (default)
scripts/stack/synapse-matrix

# with a token and outside access (0.0.0.0 requires a token!)
KB_SYNAPSE_HOST=0.0.0.0 KB_SYNAPSE_TOKEN=<token> scripts/stack/synapse-matrix

# directly (shebang tool, Zig CGO)
./bin/brain/synapse-matrix.go
```

Config — the go-config stack (`etc/brain/config.yml → config.local.yml → .env →
env`), section `synapse` (legacy `KB_SYNAPSE_*`):

```yaml
synapse:
  host: "127.0.0.1"   # 0.0.0.0 requires a token
  port: 8632
  token: ""           # empty = loopback bind only
```

## Auth

Policy (#82): **token OR bind 127.0.0.1**.

- If `synapse.token` is set → every route except `/health` requires
  `Authorization: Bearer <token>`.
- No token → the service refuses to bind to anything but loopback.
- `/health` is always open (for orchestrators).

## Endpoints

| Method | Path | What |
|-------|------|-----|
| GET | `/health` | liveness |
| GET | `/leafs?root=&type=&source=&q=&n=` | selection of leafs by filters/text |
| GET | `/edges?id=` | adjacency (incoming+outgoing synapses) |
| POST | `/addedge` | add a synapse `{from,to,type}` |
| GET | `/path?from=&to=&max=` | shortest path between leafs |
| GET | `/openapi.json` | OpenAPI 3 |
| POST | `/mcp` | JSON-RPC MCP (tools: search, get, audit, leafs, edges, addedge, path) |

The full description — `GET /openapi.json` (generated from `pkg/httpapi.Ops`).

## curl examples

```bash
HOST=127.0.0.1; PORT=8632; TOKEN=<token>
AUTH="Authorization: Bearer $TOKEN"

# leafs by root
curl -s -H "$AUTH" "http://$HOST:$PORT/leafs?root=facts&n=10"

# leafs by source (pc-agent observations)
curl -s -H "$AUTH" "http://$HOST:$PORT/leafs?source=pc-agent&n=50"

# leafs by type
curl -s -H "$AUTH" "http://$HOST:$PORT/leafs?type=fact&root=facts"

# full-text over the leaf text
curl -s -H "$AUTH" "http://$HOST:$PORT/leafs?q=container&n=5"

# adjacency of one leaf (incoming+outgoing synapses)
curl -s -H "$AUTH" "http://$HOST:$PORT/edges?id=<leaf-id>"

# add a synapse (both leafs must exist; MERGE is idempotent)
curl -s -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"from":"<id-a>","to":"<id-b>","type":"supports"}' \
  "http://$HOST:$PORT/addedge"

# shortest path a -> c
curl -s -H "$AUTH" "http://$HOST:$PORT/path?from=<id-a>&to=<id-c>&max=6"
```

Without a token: 401 on all routes except `/health`.

## Contract for pc-agent

1. **Reads** the graph: `GET /leafs` (selection by root/type/source/text),
   `GET /edges` (adjacency), `GET /path` (connectivity).
2. **Writes** new observations as leafs with `source=pc-agent` via
   `POST /ingest` (an existing endpoint; add mode, without rebuild):
   ```bash
   curl -s -X POST -H "$AUTH" -H "Content-Type: application/json" \
     -d '{"text":"pc-agent observation","root":"info","source":"pc-agent","type":"fact"}' \
     "http://$HOST:$PORT/ingest"
   ```
3. **Links** observations with synapses via `POST /addedge`.

MCP alternative: `POST /mcp` JSON-RPC `tools/call` with the names `leafs`,
`edges`, `addedge`, `path` — the same parameters as in OpenAPI.

## Tests

- `internal/brain/synapse_test.go` (cgo+system_ladybug, temp-DB fixtures):
  `TestQueryLeafsByFilters`, `TestAddEdgeAndAdjacency`, `TestPathBetween`,
  `TestHTTPLeafsEdgesPathJSON`, `TestSynapseHTTPServerEndToEnd`.
- `pkg/httpapi/server_test.go` (offline, fake API): leafs/edges/addedge/path
  routing + auth (token required / bearer accepted).

```bash
go test ./pkg/httpapi/... ./internal/config/...
bin/cgo/zig go test -race -tags system_ladybug ./internal/brain/... -count=1
```
