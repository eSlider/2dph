---
name: postgres
description: >-
  Read Postgres as compact YAML through bin/postgres/query.go (read-only
  guard, named profiles). Use when a task needs table contents, column types,
  or a SELECT against an ops database.
---

# postgres

`bin/postgres/query.go` wraps vendored `bin/db/psql-yq`. Output is YAML
(cheaper than psql ASCII, easy to slice with mikefarah/yq).

```bash
bin/postgres/query.go --profile onlyoffice -s document_asset   # column list
bin/postgres/query.go --profile onlyoffice -t task_result -l 20  # sample rows
bin/postgres/query.go --profile onlyoffice -c 'SELECT ...'     # query → YAML
```

Ad-hoc targets without a profile:

```bash
bin/postgres/query.go --container my-pg --db app -c 'SELECT 1'
bin/postgres/query.go --dsn 'postgres://user@host:5432/db' -c 'SELECT 1'
```

## Profiles

Connection details live in `$HOME/.config/brain/db-profiles.yml` (mode 600),
never in a project repo. A profile names either a `container` or a `host`;
passwords are read from a separate `password_env_file` and never appear in argv.

## Rules

- **Read-only.** Any `insert|update|delete|drop|truncate|alter|create|grant|
  revoke|vacuum|copy` is rejected with exit 3. Do not work around it.
- **PII.** Client CRM databases: aggregate and count freely; never copy names
  or addresses into chat, issues or docs.
- Use `-l` to keep samples small. Twenty rows answer most questions.
