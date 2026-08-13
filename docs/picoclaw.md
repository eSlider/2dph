# PicoClaw profile (reference agent)

2dph is the memory/fact gate. PicoClaw (or any MCP client) is the agent loop
and is **not** shipped in this repo.

```bash
docker compose --profile picoclaw up brain-mcp
```

The API listens on `127.0.0.1:8630`. Point the agent at
`http://127.0.0.1:8630/mcp` using [deploy/picoclaw/mcp.json.example](../deploy/picoclaw/mcp.json.example).

OpenAPI: `GET http://127.0.0.1:8630/openapi.json`.

Before a factual reply: `search` → `get` → `audit`. `throttled` is not a
negative finding. See `skills/picoclaw/SKILL.md`.

No Cursor required. A live PicoClaw binary/image is an operator choice.
