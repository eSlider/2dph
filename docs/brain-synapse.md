# Brain Synapse Matrix — сервис для pc-agent (issue #82)

Мозг 2dph (`internal/brain`: leafs + edges) как HTTP-сервис. Метафора:
**leafs = нейроны**, **SYNAPTIC edges = синапсы**. pc-agent читает граф и
пишет новые наблюдения как leafs с `source=pc-agent`.

## Запуск

```bash
# loopback only, порт 8632 (по умолчанию)
scripts/stack/synapse-matrix

# с токеном и доступом снаружи (0.0.0.0 требует токен!)
KB_SYNAPSE_HOST=0.0.0.0 KB_SYNAPSE_TOKEN=<token> scripts/stack/synapse-matrix

# напрямую (shebang-тулза, Zig CGO)
./bin/brain/synapse-matrix.go
```

Конфиг — стек go-config (`etc/brain/config.yml → config.local.yml → .env →
env`), секция `synapse` (legacy `KB_SYNAPSE_*`):

```yaml
synapse:
  host: "127.0.0.1"   # 0.0.0.0 требует token
  port: 8632
  token: ""           # пусто = только loopback bind
```

## Auth

Политика (#82): **token ИЛИ bind 127.0.0.1**.

- Есть `synapse.token` → каждый маршрут кроме `/health` требует
  `Authorization: Bearer <token>`.
- Нет токена → сервис отказывается биндиться на что-либо кроме loopback.
- `/health` всегда открыт (для оркестраторов).

## Endpoints

| Метод | Путь | Что |
|-------|------|-----|
| GET | `/health` | liveness |
| GET | `/leafs?root=&type=&source=&q=&n=` | выборка leafs по фильтрам/тексту |
| GET | `/edges?id=` | adjacency (входящие+исходящие синапсы) |
| POST | `/addedge` | добавить синапс `{from,to,type}` |
| GET | `/path?from=&to=&max=` | кратчайший путь между leafs |
| GET | `/openapi.json` | OpenAPI 3 |
| POST | `/mcp` | JSON-RPC MCP (tools: search, get, audit, leafs, edges, addedge, path) |

Полное описание — `GET /openapi.json` (генерируется из `pkg/httpapi.Ops`).

## Примеры curl

```bash
HOST=127.0.0.1; PORT=8632; TOKEN=<token>
AUTH="Authorization: Bearer $TOKEN"

# leafs по root
curl -s -H "$AUTH" "http://$HOST:$PORT/leafs?root=facts&n=10"

# leafs по источнику (наблюдения pc-agent)
curl -s -H "$AUTH" "http://$HOST:$PORT/leafs?source=pc-agent&n=50"

# leafs по типу
curl -s -H "$AUTH" "http://$HOST:$PORT/leafs?type=fact&root=facts"

# full-text по тексту leaf
curl -s -H "$AUTH" "http://$HOST:$PORT/leafs?q=container&n=5"

# adjacency одной leaf (входящие+исходящие синапсы)
curl -s -H "$AUTH" "http://$HOST:$PORT/edges?id=<leaf-id>"

# добавить синапс (оба leaf должны существовать; MERGE идемпотентен)
curl -s -X POST -H "$AUTH" -H "Content-Type: application/json" \
  -d '{"from":"<id-a>","to":"<id-b>","type":"supports"}' \
  "http://$HOST:$PORT/addedge"

# кратчайший путь a -> c
curl -s -H "$AUTH" "http://$HOST:$PORT/path?from=<id-a>&to=<id-c>&max=6"
```

Без токена: 401 на всех маршрутах кроме `/health`.

## Контракт для pc-agent

1. **Читает** граф: `GET /leafs` (выборка по root/type/source/тексту),
   `GET /edges` (adjacency), `GET /path` (связность).
2. **Пишет** новые наблюдения как leafs с `source=pc-agent` через
   `POST /ingest` (существующий endpoint; режим add, без rebuild):
   ```bash
   curl -s -X POST -H "$AUTH" -H "Content-Type: application/json" \
     -d '{"text":"наблюдение pc-agent","root":"info","source":"pc-agent","type":"fact"}' \
     "http://$HOST:$PORT/ingest"
   ```
3. **Связывает** наблюдения синапсами через `POST /addedge`.

MCP-альтернатива: `POST /mcp` JSON-RPC `tools/call` c именами `leafs`,
`edges`, `addedge`, `path` — те же параметры, что в OpenAPI.

## Тесты

- `internal/brain/synapse_test.go` (cgo+system_ladybug, temp-DB фикстуры):
  `TestQueryLeafsByFilters`, `TestAddEdgeAndAdjacency`, `TestPathBetween`,
  `TestHTTPLeafsEdgesPathJSON`, `TestSynapseHTTPServerEndToEnd`.
- `pkg/httpapi/server_test.go` (offline, fake API): leafs/edges/addedge/path
  routing + auth (token required / bearer accepted).

```bash
go test ./pkg/httpapi/... ./internal/config/...
bin/cgo/zig go test -race -tags system_ladybug ./internal/brain/... -count=1
```
