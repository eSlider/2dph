# CRM association proof (oo CLI ↔ corpus)

Proven with `oo` (eslider/go-onlyoffice) against the OnlyOffice portal
(`office.produktor.io`). Portal CRM is the SSOT for company ↔ person ↔
project associations; the corpus SoT (`eslider/cv/projects/knowledge-mesh-seed.yaml`)
is the second, independent source. Facts that can be backed by both are
written to the brain under `root=facts` by `bin/facts/crm`.

## What was verified

- Logical counts (portal MySQL): 1300 contacts = 897 persons + 404 companies,
  198 projects, 998 deals, 939 project↔contact links.
- Every client company linked to a project has ≥1 person underneath.
- Every person `company_id` resolves to an existing company.
- Corpus org list (9) maps 1:1 onto CRM companies:
  ProProdukt SL / produktor.io, Dyvenia, Immowelt AG, WhereGroup,
  Keynote SIGOS, D2S/SYSTEMS, GRID, Pack und Cup, Markets Platform.
- 78 person↔company association facts written to the brain
  (`how=crm-crosscheck`, `type=association`). Recall@5 in `bin/kb/eval` = 1.0.

## Mistakes found

| # | Mistake | Fix |
|---|---------|-----|
| 1 | Duplicate legal entity `GoldenRatio.Exchange` (contact 759) vs `Golden Ratio Exchange` (763); 3 deals (211, 287, 559) were linked to 759 | `oo contacts merge 759 763` — 763 kept, 759 removed, deal links re-pointed to 763 |
| 2 | `env/`-wide: OnlyOffice creds file used wrong UX (user `eslider`, password with `$2` suffix) making `oo` auth fail | `.env` fixed to `eslider@gmail.com` + clean password; `.env` stays gitignored |

## Gates after fix

- `uv run python -m unittest discover -s bin/tools -t .` → 26 tests OK
- `bin/facts/audit self` + `bin/facts/audit db` → ok
- `bin/kb/eval` → recall@5 = 1.0
- `go test ./...` (bin/server + bin/watch) → ok