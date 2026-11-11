# Brain read contract (P-9.4)

Normative response format of `search` / `get` / `stats` / `audit` — how clients
and agents read facts, info and audit from Ladybug. A mirror of the write contract
[`docs/brain/contract.md`](contract.md) (P-9.2/P-9.3): there — how to write a leaf
(source/external_id/kind/text, ContentHash, observed_at), here — how to read.

Schema implementation: `internal/contract/read.go` (cgo-free, version
`contract.ReadContractVersion`); response producers — `internal/brain`
(search/get/stats/audit), HTTP surface — `pkg/httpapi`
(`bin/brain/serve.go`, :8630) + MCP (`POST /mcp`), CLI — `bin/brain/*.go --json`;
the service client for consumers — `pkg/brainclient` (P-9.5, section
["How a client reads the brain"](#client-layer-p-95)).

## Read surfaces (one format)

| Surface | Command / path |
|-------------|----------------|
| CLI | `bin/brain/search.go "q" --json`, `bin/brain/get.go <id> --json`, `bin/brain/stats.go --json` |
| HTTP | `GET /search?q=`, `GET /get?id=`, `GET /stats`, `GET /audit` (:8630) |
| MCP | tools `search` / `get` / `audit` (`POST /mcp`; `stats` — HTTP-only, minimal MCP surface D20) |
| Client | SDK `pkg/brainclient`, CLI `bin/brain/client.go search|get|stats|audit` (P-9.5, section "Client layer") |

All surfaces return the same JSON (schemas below); HTTP/MCP — a thin
transport over the same structures. **Clients read the brain only through these
formats — never parse `var/kb.lbug` directly** (the Ladybug schema is
internal, changes without notice).

## Contract versioning

Every response carries the field **`contract_version`** (semver `MAJOR.MINOR`,
currently `1.0`). Contract edits:

- **additive** (minor bump): a new optional field, a doc clarification, a new
  value domain. Backward compatible — old clients work unchanged.
- **breaking** (major bump): removal/renaming of a required field, a change of
  type or semantics. Requires migration of all clients.

Client rules:

- Accept **any** response with a compatible MAJOR; do not require an exact MINOR.
- Ignore unknown fields (additive evolution) — do not fail on them.
- The absence of `contract_version` = the response was formed **before** the
  contract (an old service); the field format is the same — the gate admits such a response
  via `--relax-version` (warning, not failure).

Contract consumers: agents (PicoClaw gateway — `search → get → audit`),
`gator`/`cv` clients, `bin/brain` tools, stack scripts.

## Response formats (JSON schemas)

Common rules: **absence → absence** — an optional field that is missing is
simply absent from the JSON (`omitempty`), not `null` and not an empty string.
Required fields are always present. `results`/`by_confidence` are always an
array (may be empty), not `null`.

### search — `GET /search?q=…` / CLI `--json`

| Field | Type | Required | Meaning |
|------|-----|-------------|-------|
| `contract_version` | string | yes | contract version, `MAJOR.MINOR` |
| `query` | string | yes | the submitted query |
| `root_filter` | string | yes | `facts` / `info`; `""` = all roots |
| `as_of` | string | no | `YYYY-MM-DD` (D24), validity filter |
| `count` | int | yes | number of results (`>= 0`) |
| `results` | array | yes | hits (empty if there are no matches) |
| `web` | object | no | the second independent source (SearXNG) |

Hit (`results[]`):

| Field | Type | Required | Meaning |
|------|-----|-------------|-------|
| `id` | string | yes | sheet id |
| `text` | string | yes | full sheet text |
| `root` | string | yes | `facts` / `info` |
| `confidence` | string | no | `confirmed` / `estimated` / `inferred` / … |
| `score` | float | yes | relevance score (RRF/cosine) |
| `snippet` | string | no | up to 280 runes of text |
| `valid_from` / `valid_to` | string | no | `YYYY-MM-DD` (D24) |
| `hops` | array | no | `--hop N` traversal (`id`/`label`/`name`/`depth`) |

`web` block: `status` (required: `ok`/`throttled`/`skipped`/`refused`/…),
`note`, `cached`, `results[]` (`rank`/`title`/`url`/`snippet`/`engine`).

### get — `GET /get?id=…` / CLI `--json`

| Field | Type | Required | Meaning |
|------|-----|-------------|-------|
| `contract_version` | string | yes | contract version |
| `id` | string | yes | sheet id |
| `root` | string | yes | `facts` / `info` |
| `confidence` | string | yes | `confirmed` / `estimated` / `inferred` / … |
| `source` | string | yes | corpus: `mail` / `git` / `chats` / `docs` / `facts` |
| `type` | string | yes | sheet kind (`fact` / `mail` / `chat` / `commit` / …) |
| `valid_from` / `valid_to` | string | no | `YYYY-MM-DD` (D24) |
| `snippet` | string | no | without `--body` (up to 280 runes) |
| `text` | string | no | only with `--body` (full text) |

`text` and `snippet` are mutually exclusive: `--body` → `text`, otherwise `snippet`.

### stats — `GET /stats` / CLI `--json`

| Field | Type | Required | Meaning |
|------|-----|-------------|-------|
| `contract_version` | string | yes | contract version |
| `total` | int | yes | total leafs (`>= 0`) |
| `by_root` | object | yes | `{"facts": N, "info": N}` — actual roots |
| `db` | string | yes | path to kb.lbug |
| `model` | string | no | CLI extension: embedding model id |
| `ann` | object | no | HTTP extension: ANN index status |

The required core is the same for CLI and HTTP; `model` and `ann` are additive
surface extensions (a client must not rely on them).

### audit — `GET /audit` / MCP `audit`

| Field | Type | Required | Meaning |
|------|-----|-------------|-------|
| `contract_version` | string | yes | contract version |
| `status` | string | yes | `"ok"` |
| `by_confidence` | array | yes | root × confidence histogram |

The `by_confidence[]` row: `root` (`facts`/`info`), `confidence`, `count`
(`>= 0`). This is the **read response format** of the audit; the Vinogradov
audit cards themselves (L-9, issues #229–234) are a separate domain (`docs/facts/`,
`bin/facts/audit.go`) and are not part of this contract. The conformance audit against the
write contract — `bin/brain/audit-contract.go` (its own report schema, P-9.2).

## Semantics: root, confidence, (not confirmed)

Terms shared with the write contract (contract.md):

- **`root=facts`** — a claim confirmed by **≥2 independent
  sources**, `confidence: confirmed`. This is the only root from which an
  answer counts as a fact.
- **`root=info`** — a descriptive/narrative corpus leaf (mail/git/chats/docs),
  searchable, never asserted.
- **`confidence`** — an open domain: `confirmed` / `estimated` / `inferred` /
  `hypothesis` / `partial` / …. An empty value (legacy leafs) is treated
  as `confirmed`.
- **`(not confirmed)`** — the answer does not come from the facts root: an info hit,
  hypothesis/partial, or only one independent source. The agent must
  mark such an answer `(not confirmed)` and not present it as a fact.
- Deduction search order: facts → info → web. `--root facts` returns
  only confirmed ones; without the filter info hits are marked `(not confirmed)`.
- The `web` block is kept separate from the graph hits: "ours" vs "not ours".
  `throttled`/`skipped`/`refused` — not proof of absence.

## Time: `--as-of` (valid_from/valid_to) and observed_at

- **`valid_from` / `valid_to`** — validity of the fact **by days** (D24,
  `YYYY-MM-DD`), **orthogonal to the version** (version = id/ContentHash).
  `--as-of YYYY-MM-DD` keeps hits whose interval covers the day: an empty
  interval = "always" (legacy), an empty `valid_to` = an open end, both ends
  inclusive. This is a filter by the meaning of the fact, not by the write date.
- **`observed_at`** — when the content was **seen** (RFC3339), part of the write
  contract, **not part of the dedup key** (ContentHash): the same content
  written later dedups to the same version. In read responses the field is
  currently **not exposed** (additive future); it is read via the write-contract
  audit (`bin/brain/audit-contract.go`).

## Conformance gate (read-only)

The contract is checked by a script, not only by text. Both gates are read-only,
work in parallel with the live service (Ladybug: a second RW-open is impossible,
RO-open is allowed).

```bash
# Response format of the live service (HTTP :8630):
./bin/brain/read-contract.go                       # strict (requires contract_version)
./bin/brain/read-contract.go --relax-version       # service predating the contract: format is checked, version — warning
./bin/brain/read-contract.go --base URL --token T  # another service
./bin/brain/read-contract.go --json                # machine-readable report

# Data invariants on the live kb.lbug (no running service needed):
bin/cgo/zig go run -tags=system_ladybug,brain_readcontract_db bin/brain/read-contract-db.go
bin/cgo/zig go run -tags=system_ladybug,brain_readcontract_db bin/brain/read-contract-db.go --json
```

What the HTTP gate checks: search (general + `root=facts` + `as_of`), get (with and
without a body), stats, audit — required fields, types, domains (`root` ∈ facts|info,
dates `YYYY-MM-DD`), absence → absence. The DB gate reproduces the response
shapes from live data and runs the same validators (sample of 200 leafs).

Run on 2026-09-02 against the live kb.lbug (105210 leafs, service :8630):

```
$ bin/cgo/zig go run -tags=system_ladybug,brain_readcontract_db bin/brain/read-contract-db.go
read contract gate: db (contract_version=1.0)
  PASS   stats    total=105210 roots=2
  PASS   audit    rows=2
  PASS   sample   search/get fields ok (sample=200)
gate: PASS

$ ./bin/brain/read-contract.go --relax-version   # live :8630 before redeploy
read contract gate: http:http://127.0.0.1:8630 (contract_version=1.0)
  PASS*  search   ok (warnings)     # warning: service predates read-contract
  …
gate: PASS
```

## Client layer (P-9.5)

A single client layer on top of the read contract, so that gator/cv/agents/scripts
do not call HTTP/MCP/CLI each in their own way and do not open `var/kb.lbug`:

| Layer | Where | What |
|------|-----|-----|
| SDK | `pkg/brainclient` (cgo-free) | typed client: `Search`/`Get`/`Stats`/`Audit` → `internal/contract` responses; responses are validated by the contract validators (format + `contract_version`, an incompatible service — an error); the "facts gate" (`Gate`, `Facts`, `GateAudit`) |
| CLI | `bin/brain/client.go` (shebang, `brain_client`) | subcommands `search`/`get`/`stats`/`audit`, `--json`, `--root facts|info`, `--as-of`, `-n`, `--no-web`, `--base URL`, `--token T`; base by default from `internal/config` (host/port), otherwise `127.0.0.1:8630` |

The client goes only through the contract (HTTP :8630, `bin/brain/serve.go`) — the
client code contains not a single opening of kb.lbug, parsing of the Ladybug file, or
query against the internal DB schema. The package is cgo-free: tests on httptest
fixtures are part of the regular `go test ./...`.

```bash
# CLI examples
./bin/brain/client.go search "onlyoffice postgres" --root facts --json
./bin/brain/client.go get <id> --body
./bin/brain/client.go stats
./bin/brain/client.go audit
```

SDK example (an agent/script/service in Go):

```go
import "github.com/eSlider/2dph/pkg/brainclient"

cl := brainclient.New(brainclient.Config{Base: "http://127.0.0.1:8630"})
facts, err := cl.Facts(ctx, "where is the lexicon", brainclient.SearchOptions{})
for _, f := range facts.Confirmed { /* safe to quote as a fact */ }
for _, nf := range facts.NotConfirmed { /* (not confirmed): only with a marker */ }
```

### Facts gate

The server returns sheets as they lie in the DB; the client-side gate
(`pkg/brainclient`) is responsible for **what is not confirmed not being
presented as a fact**:

- `Gate(root, confidence)` — the verdict for a single hit: `true` only for
  `root=facts` with `confidence=confirmed` (empty = legacy, per the semantics above).
  `info`, `hypothesis`/`partial`/`estimated`/`inferred` → `false`.
- `Client.Facts` = `search --root facts` + the gate: the response is split into
  `confirmed[]` (confirmed facts) and `not_confirmed[]` (rejected, with a
  `reason` field — the `(not confirmed)` marker). 2v2 contradictions (D16) lie
  as `hypothesis` and never get into `confirmed`.
- `GateAudit` — a gate over the `audit` histogram: `confirmed_facts` vs
  `not_confirmed_facts` on the facts root. `not_confirmed_facts > 0` means: there are
  hypothesis/partial sheets on the facts root — such sheets cannot be served as
  facts until `bin/facts/audit.go contradict` / audit-card sorts them out
  (L-9, #229–234). In this case the CLI `audit` exits with code 1.
- The gate is a defense on read; writing still requires ≥2 independent
  sources (promote, `docs/brain/contract.md`). The client does not "trust" the
  server: an incompatible format/version — an error, a rejected one — is marked.

### Example 1: a search fact through the client

```bash
./bin/brain/client.go search "onlyoffice postgres" --root facts --json
```

```json
{
  "contract_version": "1.0",
  "query": "onlyoffice postgres",
  "root_filter": "facts",
  "count": 1,
  "confirmed": [
    {
      "id": "af77a292ba48f7f8be5c040c21e56dd5",
      "text": "container 'onlyoffice' is running and declared in docker-compose.yml",
      "root": "facts",
      "confidence": "confirmed",
      "score": 5.48,
      "snippet": "container 'onlyoffice' is running and declared in docker-compose.yml"
    }
  ]
}
```

If a hypothesis/partial appears on the facts root (e.g. a 2v2 sheet before the
audit), it goes into `not_confirmed[]` with a `reason`, not into `confirmed[]` —
the agent will not be able to quote it as a fact. `get <id> --body` shows the
two-source `source` ("docker ps x compose:docker-compose.yml") —
evidence per the write contract.

### Example 2: audit card (facts-root hygiene)

```bash
./bin/brain/client.go audit
facts          confirmed        21
info           confirmed    105199
facts gate: confirmed=21 not_confirmed=0
```

`facts gate: not_confirmed=0` — the facts root is clean: all 21 facts are confirmed.
If the gate shows `not_confirmed > 0`, this is a signal for an L-9 card:
`bin/facts/audit.go contradict` (2v2 analysis) or `bin/facts/audit-card.go`
(a verdict into `var/audit/source-of-truth.yml`) — until the analysis such sheets remain
`(not confirmed)` and are not counted as facts in client responses.

### Cross-repo consumers (gator/cv)

`internal/contract` is an internal package of the module, so today the client is
imported by 2dph tools and skills. For gator/cv to import the
client as an external go module, the read-contract types and the client need to be moved into a
public `go-*` module — this is decided by P-9.6 (the shared ADR of the
facts/audit/memory model, epic #239); before that, external consumers read the brain by
this document (formats) and via HTTP/MCP/CLI without duplicating logic.

## Cross-ref

- Write contract: [`docs/brain/contract.md`](contract.md) (P-9.2/P-9.3,
  issues #235/#13) — Leaf, ContentHash, observed_at, corpus adapters.
- Shared versioning pattern / ADR: P-9.6, issue #16.
- Vinogradov audit cards: L-9, issues #229–234 — a separate domain
  (`docs/facts/audit-recipes.md`, `bin/facts/audit.go`).
- Skill `brain` (skills/brain) — how to use search and add a corpus.
- HTTP/MCP surface: `pkg/httpapi` (OpenAPI `GET /openapi.json`).
