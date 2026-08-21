---
type: howto
status: current
---

# Tuning the SearXNG instance

The client treats HTTP 200 + `results: []` as **throttled**, not as absence.
Fix the cause on the instance you point `BRAIN_SEARCH_URL` at, or use the
optional Compose profile in this repo.

## Optional Compose profile

Do not start this on a host that already runs SearXNG — set `BRAIN_SEARCH_URL`
instead (D3).

```bash
SEARXNG_SECRET=$(openssl rand -hex 32) docker compose --profile searxng up -d
```

Pinned image: `docker.io/searxng/searxng:2026.8.10-0a118066d`.
Settings: `etc/searxng/settings.yml` + `limiter.toml` (RFC1918 `pass_ip`,
short `suspended_times`, `formats: [html, json]`). No secrets in git. Bind is
`127.0.0.1:8888`.

## Why the client classifies empty as throttled

A default engine set under load answers HTTP 200 with `results: []` (sometimes
with empty `unresponsive_engines`). That is indistinguishable from "nothing
found" unless the client refuses to call it absence.

## Instance-side levers

1. **Allow the callers through the limiter** (`limiter.toml` `pass_ip`). The
   Compose file uses RFC1918 only.
2. **Shorten suspensions** (`settings.yml` `search.suspended_times`) so a
   CAPTCHA does not last a day.
3. **More than one engine in `general`.** One live engine is a single point of
   failure. This repo enables bing, google, duckduckgo, wikipedia.
4. **Keep the JSON API on.** `formats: [html, json]` must stay.

## Verifying

```bash
for i in $(seq 10); do
  bin/web/search.go "test $i" -n 1 --refresh --json | yq -r '.status'
done
```

Ten lines of `ok` means the instance is healthy. Any `throttled` means say
nothing about whether the subject exists.
