---
name: db-yaml
description: >-
  Read any Postgres as compact YAML through db/psql-yq, with a read-only guard and
  named profiles. Use when a task needs table contents, column types or a SELECT
  against cs_brain or another project database.
---

# db-yaml

`agent-skills/bin/db/psql-yq` talks to Postgres and returns YAML, which is far
cheaper than a psql ASCII table and easy to slice with `yq`.

```bash
bin/db/offline -s kunde              # column list          (brain-ui wrapper)
bin/db/offline -t tour -l 20         # 20 sample rows as YAML
bin/db/offline -c 'SELECT ...'       # query -> YAML
bin/db/live -c 'SELECT ...'          # live database instead of the snapshot
```

Ad-hoc targets without a profile:

```bash
agent-skills/bin/db/psql-yq --container my-pg --db app -c 'SELECT 1'
agent-skills/bin/db/psql-yq --dsn 'postgres://user@host:5432/db' -c 'SELECT 1'
```

## Profiles

Connection details live in `~/.config/brain/db-profiles.yml` (mode 600), never in a
project repo. A profile names either a `container` or a `host`; passwords are read
from a separate `password_env_file` and never appear in argv.

## Rules

- **Read-only.** Any `insert|update|delete|drop|truncate|alter|create|grant|
  revoke|vacuum|copy` is rejected with exit 3. Do not work around it.
- **PII.** `cs_brain` holds client data. Aggregate and count freely; never copy
  names or addresses into chat, issues or docs.
- Use `-l` to keep samples small. Twenty rows answer most questions.
