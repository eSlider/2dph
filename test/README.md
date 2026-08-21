# 2dph tests

Tiers (refactor v1.0 P5):

| Tier | Dir | Live dep | Run |
|------|-----|----------|-----|
| system | `test/system/` | no | `go test ./test/system/...` |
| stress | `test/stress/` | live brain | `BRAIN_URL=… ./test/stress/stress.go --c 8 --d 30` |
| integration | `test/integration/` | OO/SearXNG/mail | `go test -tags=integration ./test/integration/...` |

CI runs the offline **system** tier by default.

Historical load baseline 2026-08-11: Gitea issue #58 (comment archive).
