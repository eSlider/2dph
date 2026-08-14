# Reasoner bake-off (D18)

Pluggable OpenAI-compatible URL. 2dph does not ship weights. PicoClaw is
compose profile `picoclaw` (`sipeed/picoclaw`); the bake-off hits the same
tool names (`search` → `get` → `audit` from `internal/httpapi.Ops`).

```bash
docker compose --profile reasoner up -d reasoner
docker compose --profile reasoner exec reasoner ollama pull qwen3.5:9b
REASONER_BASE_URL=http://127.0.0.1:11435/v1 REASONER_MODEL=qwen3.5:9b \
  ./bin/reasoner/bakeoff.go --json
```

JSON includes `latency_p50_ms` / `latency_p95_ms` from DuckDB (`internal/duckstats`, D22).

Host Ollama on `:11434` is left alone. This sidecar binds `127.0.0.1:11435`
with `OLLAMA_NUM_GPU=0` (CPU). Measure RSS (`/api/ps` `size`), not VRAM.

If Compose cannot allocate a project network (Docker IPAM pool exhausted),
the same sidecar is:

```bash
docker run -d --name 2dph-reasoner \
  -e OLLAMA_NUM_GPU=0 \
  -p 127.0.0.1:11435:11434 \
  -v 2dph-reasoner-ollama:/root/.ollama \
  ollama/ollama:latest
```

## Real Hugging Face ids

| Role | HF id | Ollama tag (this bake-off) |
|------|-------|----------------------------|
| RAM / 9B | `Qwen/Qwen3.5-9B` | `qwen3.5:9b` |
| Quality 27B (CPU) | `prism-ml/Bonsai-27B-gguf` (derived from Qwen3.6-27B) | `MichelRosselli/bonsai-27b:Q1_0` |
| Quality 27B (full) | `Qwen/Qwen3.6-27B` | not pulled on this CPU box |

There is **no official Qwen3.6-9B**. Do not invent that id.

Qwen3.5-9B has documented upstream tool-call XML bugs. A 9B win on tools
is only claimed if this bake-off records OpenAI `tool_calls` (not
`<tool_call>` XML in `content`).

The 2dph API image does not `COPY` GGUF/safetensors. Pull at runtime into
the `reasoner-ollama` volume.

## Live CPU run

Host sidecar: Ollama **0.32.9**, `OLLAMA_NUM_GPU=0`, `127.0.0.1:11435`,
`device: cpu`, `vram_mb: 0`. Date: 2026-08-13. Same three prompts
(`search` / `get` / `audit`). PicoClaw binary was not used; the OpenAI
tools payload is the surface it would send.

| Model | HF id | tool_call | xml_leak | rss_mb | latency_ms (search/get/audit) |
|-------|-------|-----------|----------|--------|-------------------------------|
| `qwen3.5:9b` | `Qwen/Qwen3.5-9B` | 3/3 | 0 | 5790 | 50059 / 51678 / 31980 |
| `MichelRosselli/bonsai-27b:Q1_0` | `prism-ml/Bonsai-27B-gguf` | 3/3 | 0 | 21951 | 327702 / 166830 / 119808 |
| `Qwen/Qwen3.6-27B` | `Qwen/Qwen3.6-27B` | not loaded | — | — | too heavy for this CPU box |

Both loaded models emitted OpenAI `tool_calls` (not `<tool_call>` XML) on
this runtime. **Do not claim 9B is better at tools** — the score is tied
at 3/3. 9B is smaller and faster. Bonsai RSS includes weights + KV
(`size` from `/api/ps`); first Bonsai prompt includes cold load.

Re-run:

```bash
REASONER_BASE_URL=http://127.0.0.1:11435/v1 REASONER_MODEL=qwen3.5:9b \
  ./bin/reasoner/bakeoff.go --json
REASONER_MODEL=MichelRosselli/bonsai-27b:Q1_0 ./bin/reasoner/bakeoff.go --json
```
