# PicoClaw profile (reference agent)

2dph is the memory/fact gate. Compose profile `picoclaw` runs the official
PicoClaw gateway (`docker.io/sipeed/picoclaw:v0.3.1`) plus `brain-mcp` and the
CPU reasoner. Default agent model is `qwen3.5:9b` (RAM path, D18). Weights stay
in the reasoner volume, not in the 2dph image.
No secrets in git: Ollama needs no key; MCP is local HTTP.

```bash
scripts/stack/start-assistant
scripts/stack/start-assistant --no-attach
scripts/stack/start-assistant -- -m "search the 2dph brain for LadybugDB"
scripts/stack/status
scripts/stack/stop
```

`start-assistant` reuses a healthy brain on `:8630`, starts the CPU reasoner,
pulls `qwen3.5:9b` if missing, brings up the gateway with `--no-deps picoclaw`,
then `picoclaw agent` (MCP `search` → `get` → `audit`). Gateway-only Compose:

```bash
docker compose --profile picoclaw up -d
# already serving :8630 / :11435:
docker compose --profile picoclaw up -d --no-deps picoclaw
```

Gateway: `127.0.0.1:18790`. Brain MCP: `http://127.0.0.1:8630/mcp`.
Cursor-style clients can use [etc/picoclaw/mcp.json.example](../etc/picoclaw/mcp.json.example).
PicoClaw itself uses [etc/picoclaw/config.json](../etc/picoclaw/config.json)
(`127.0.0.1` + host network — loopback publishes are not reachable via docker0).

OpenAPI: `GET http://127.0.0.1:8630/openapi.json`.

Before a factual reply: `search` → `get` → `audit`. `throttled` is not a
negative finding. See `skills/picoclaw/SKILL.md`.

System performance (MCP gates + qwen3.5:9b tool_call + PicoClaw gateway):

```bash
./test/system/system_perf.go --json | yq '.gates'
REASONER_MODEL=qwen3.5:9b ./test/system/system_perf.go --reasoner --picoclaw --json | yq '.reasoner'
```

The default agent model is `qwen3.5:9b`. PicoClaw `context_window` is 8192
(heuristic `max_tokens*4` at 512 is 2048, too small for MCP tool schemas).
`request_timeout` is 600s for a CPU turn (tool_call + MCP search + answer).
