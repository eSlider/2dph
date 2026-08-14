---
name: yq
description: >-
  Use https://github.com/mikefarah/yq to work with YAML, JSON, XML, CSV,
  TOML, HCL where it's efficient and less code. Use when slicing compose,
  config, --json tool output, CSV/TOML/HCL/XML, or converting between those
  formats. Not kislyuk Python yq. Not jq when yq already does the job.
---

# yq (mikefarah)

Use https://github.com/mikefarah/yq to work with YAML, JSON, XML, CSV, TOML, HCL where it's efficient and less code.

This is the Go `yq` (`yq --version` contains `mikefarah`). It is not
kislyuk/yq (Python, jq-syntax, YAML-only wrapper). `bin/db/psql-yq` already
calls this binary.

Prefer `yq` over `python3 -c`, `jq`, or ad-hoc parsers when one expression
reads or converts the file. Keep Python/Go for HTTP, binary protocols, and
in-process tests.

```bash
yq '.services.picoclaw.image' compose.yaml
yq -P . deploy/picoclaw/config.json          # JSON → YAML
yq -o=json '.gates'                           # JSON stdin (qa/system_perf.py --json)
yq -p=csv -o=json .
yq -p=xml -o=json .
yq -p=toml '.package.name' file.toml
bin/brain/search.go "LadybugDB" --json | yq '.[].ref'
bin/web/search.go "hypervisor" --json | yq -r '.results[].url'
```

Do not print secrets, PII, or `$HOME/.config/brain/` through `yq`.
