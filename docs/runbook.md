---
type: howto
status: current
related:
  - docs/README.md
  - PLAN.md
---

# Run 2dph (portable)

No laptop-absolute paths. Config lives in env files under `$HOME/.config/brain/`
(mode 0600), not in git.

## Toolchain

- Go (see `go.mod`)
- Python 3.12 + [uv](https://docs.astral.sh/uv)
- Optional: Docker, Zig CGO via `bin/cgo/zig` (not gcc)

```bash
uv venv .venv
uv pip install -r requirements.lock.txt
eval "$(bin/cgo/zig env)"   # when compiling Ladybug read tools
go test ./...
uv run python -m unittest discover -s bin/tools -t .
```

## Config

| File / env | Purpose |
|------------|---------|
| `$BRAIN_SEARCH_ENV` (default `$HOME/.config/brain/search.env`) | `BRAIN_SEARCH_URL` (SearXNG). Optional Basic Auth. |
| `$HOME/.config/brain/db-profiles.yml` | read-only Postgres profiles (OnlyOffice via tunnel) |

If the host already runs SearXNG, point `BRAIN_SEARCH_URL` at it. Do not start
a second copy (D3). Optional Compose instance:

```bash
SEARXNG_SECRET=$(openssl rand -hex 32) docker compose --profile searxng up -d
```

That binds `127.0.0.1:8888`. JSON format must stay enabled.

## Index then search

Write path is Compose profile `index` (Python Ladybug rebuild) until
`brain/add` is v2. The operator command is `bin/brain/index.go`.

```bash
bin/brain/index.go --rebuild
bin/brain/search.go "LadybugDB vector index"     # facts → info → web (D17)
bin/brain/search.go "upstream flag" --no-web
bin/brain/get.go <id> --body
bin/brain/stats.go
```

`--hop` is not implemented. Empty web results are `throttled`, not absence.

Ladybug 0.19: never `DROP INDEX` FTS/VECTOR (ghost catalog). Fresh indexes =
delete `var/kb.lbug` then `--rebuild`.

## HTTP / MCP

```bash
docker compose up -d brain                       # :8630 Zig CGO serve
docker compose --profile index run --rm index    # rebuild
docker compose --profile picoclaw up brain-mcp   # MCP 127.0.0.1:8630
```

`GET /openapi.json`, `POST /mcp`. Agent tool order: `search` → `get` → `audit`.

## Reasoner (optional, D18)

CPU sidecar on `127.0.0.1:11435`. Weights are not in the 2dph image.

```bash
docker compose --profile reasoner up -d reasoner
REASONER_BASE_URL=http://127.0.0.1:11435/v1 ./bin/reasoner/bakeoff.go --json
```

See [reasoner.md](reasoner.md).
