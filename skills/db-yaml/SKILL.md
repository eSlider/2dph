---
name: db-yaml
description: >-
  Read any Postgres as compact YAML through db/psql-yq, with a read-only guard and
  named profiles. Use when a task needs table contents, column types or a SELECT
  against cs_brain or another project database.
---

# db-yaml

`bin/db/psql-yq` (vendored in this repo) talks to Postgres and returns YAML,
which is far cheaper than a psql ASCII table and easy to slice with `yq`.

```bash
bin/db/psql-yq --profile onlyoffice -s document_asset   # column list
bin/db/psql-yq --profile onlyoffice -t task_result -l 20  # sample rows as YAML
bin/db/psql-yq --profile onlyoffice -c 'SELECT ...'     # query -> YAML
```

Ad-hoc targets without a profile:

```bash
bin/db/psql-yq --container my-pg --db app -c 'SELECT 1'
bin/db/psql-yq --dsn 'postgres://user@host:5432/db' -c 'SELECT 1'
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
