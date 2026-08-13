---
name: web-search
description: >-
  Search the public web through SearXNG using bin/web/search.go. Use for vendor
  documentation, public standards, and any fact that is not in our own repos —
  and as the second independent source the detective method requires.
---

# web-search

```bash
bin/web/search.go "LadybugDB vector index"
bin/web/search.go "model2vec multilingual" --category it
bin/web/search.go "hypervisor" --site example.com --json | jq -r '.results[].url'
bin/web/search.go "postgres partial index" --lang en --fresh year
```

URL and optional Basic Auth live in `$BRAIN_SEARCH_ENV` (default
`$HOME/.config/brain/search.env`): `BRAIN_SEARCH_URL` is required;
`BRAIN_SEARCH_USER` / `BRAIN_SEARCH_PASS` only if the instance uses Basic Auth.
A host that already runs SearXNG should set `BRAIN_SEARCH_URL` and not start
the Compose profile.

## Web or knowledge base

`bin/brain/search.go` holds our own facts: the ops stack, portfolio, ssh hosts,
the lexicon. Go there first. Reach for `bin/web/search.go` when the answer is
outside our repos: upstream library behaviour, vendor documentation, public
standards.

Keep the two apart. A finding is stronger when the reader can see that one
source was ours and one was not.

## PII: this query leaves the host

The search goes to external engines. Never put a client or staff identifier in
it. The tool refuses long digit runs, `Personalnummer`, `KV-Nr`, dates of birth
and street-with-number, and exits 2. Rephrase rather than reaching for `--force`.

## Read the status, not just the results

The instance answers HTTP 200 with an empty list when it throttles, so an empty
answer is ambiguous by construction. The tool resolves that for you:

| status | exit | meaning |
|--------|------|---------|
| `ok` | 0 | engines answered |
| `throttled` | 3 | nobody answered; say nothing about what exists |

Never turn a `throttled` result into "there is no information about X".

## Etiquette

Calls are serialised host-wide and kept ten seconds apart, and answers are
cached for seven days. Do not loop over queries, and do not use `--refresh`
unless the cached answer is genuinely stale: a burst suspends the engines for
several minutes for everyone.
