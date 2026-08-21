# 2dph tests

Test taxonomy (refactor v1.0 P5):

| Tier | Dir | Requires live dep | Run |
|------|-----|-------------------|-----|
| system | `test/system/` | no (offline source-gate) | `go test ./test/system/...` |
| stress | `test/stress/` | yes (live brain REST) | `BRAIN_URL=… ./test/stress/stress.go --c 8 --d 30` |
| integration | `test/integration/` | yes (OnlyOffice CRM, SearXNG, mail) | opt-in, not run by default |

CI runs the offline **system** tier by default (`go test ./test/system/...`).

## system

Offline-gated tests that verify tool source invariants without a live brain.
They read the sibling `.go` file and assert expected flags/gates, and forbid
secrets, `kb.lbug`, and file writes.

```bash
go test ./test/system/...
```

## stress

Live-brain load generator against the REST surface. Read-only (search/get/stats/audit),
never ingests, writes nothing. Gates: health < 500ms, search p95 < 1000ms,
per-type error rate < 1%. Exit 1 if any gate fails.

```bash
BRAIN_URL=http://127.0.0.1:8630 ./test/stress/stress.go --c 8 --d 30 --json
```

## integration

Planned for tests that need a live dependency (OnlyOffice CRM via `oo`, SearXNG,
mail). Gated by a build tag / env so it is not part of the default `go test ./...`.

```bash
go test -tags=integration ./test/integration/...
```

Historical load-test report: [docs/load-test-summary-2026-08-11.md](../docs/load-test-summary-2026-08-11.md).
