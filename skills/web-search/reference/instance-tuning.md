---
type: howto
status: current
---

# Tuning the SearXNG instance

The client works around a fragile instance. These changes fix the cause, and
they need shell access to the host behind `search.produktor.io`
(`90.169.228.16` / `produktor.mywire.org`), which is a different machine from
the one the agents run on.

## Why it is needed

Measured on 2026-08-10 from this host:

- The default engine set for the `general` category is only `duckduckgo`,
  `brave` and `startpage`. `brave` and `startpage` sit in
  `Suspended: too many requests` or `Suspended: CAPTCHA` almost permanently, so
  in practice a single engine carries every query.
- About 25 probe requests over a few minutes pushed `duckduckgo` into `CAPTCHA`
  as well. The instance then answered HTTP 200 with `results: []` and an empty
  `unresponsive_engines` - indistinguishable from "nothing found" without the
  client-side handling we added.
- Recovery took roughly six minutes.

## Changes

1. **Allow our egress IP through the limiter.** In `limiter.toml`:

   ```toml
   [botdetection.ip_lists]
   pass_ip = ["77.7.46.234"]
   ```

2. **Shorten the suspensions.** In `settings.yml` the defaults are 24 hours for
   a CAPTCHA and one hour for too-many-requests, which is far longer than the
   condition lasts:

   ```yaml
   search:
     suspended_times:
       SearxEngineCaptcha: 300
       SearxEngineTooManyRequests: 120
       SearxEngineAccessDenied: 300
   ```

3. **Give `general` more than one working engine.** `google` and `wikipedia`
   report `enabled: true` in `/config` yet never appear in a `general` response,
   so they are not in the default set. Put them in it; one live engine per
   category is a single point of failure.

4. **Keep the JSON API on.** `formats: [html, json]` must stay, otherwise every
   client here breaks.

## Verifying

Ten requests in a row used to suspend the instance for minutes. After the
change they should all answer:

```bash
for i in $(seq 10); do
  bin/web/search "test $i" -n 1 --refresh --json | jq -r .status
done
```

Ten lines of `ok` means it is fixed.
