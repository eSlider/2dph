# OO CRM: connector for network accept links (N-1.2 #269)

Transfer of the contact-network accept links (L-9.5 #234, output
`bin/network --accept-only`) into OnlyOffice CRM as **business contacts** —
so that marketing/sales work from the graph: each contact with a premises
reference (verifiable in the graph via `bin/network`) and without duplicates
(ADR-0012 §contact network item 4). Pilot — top-60 accept of
andriy.oblivantsev@wheregroup.com (N-1.3 #270).

## Tool

```bash
bin/network/network.go --person X --accept-only > /tmp/net.yml   # manifest (read-only)
ONLYOFFICE_URL/USER/PASS ./bin/onlyoffice/import-network.go --manifest /tmp/net.yml   # report only
./bin/onlyoffice/import-network.go --manifest /tmp/net.yml --dry-run                   # preview: lookups, writes nothing
./bin/onlyoffice/import-network.go --manifest /tmp/net.yml --write --limit 60          # create up to 60
./bin/onlyoffice/import-network.go --manifest /tmp/net.yml --write --backfill          # + backfill tag/about for matched (N-1.4)
./bin/onlyoffice/import-network.go --manifest /tmp/net.yml --write --companies c.yml   # + role mailboxes onto a company by domain (N-1.4)
cat /tmp/net.yml | ./bin/onlyoffice/import-network.go --manifest - --write             # stdin
```

Flags: `--manifest <file|->` (required), `--dry-run` (preview, writes nothing),
`--write` (create; report-only by default), `--limit N`
(max new per run), `--tag` (default `2dph:network:<target-email>`),
`--backfill`, `--companies <file|->` (both — N-1.4 #271, idempotent).
Report: `created / matched / skipped / failed / pending`; failures — as a list
(`failed` returns exit 1). Creds — `ONLYOFFICE_URL/USER/PASS`
(go-onlyoffice `GetEnvironmentCredentials`), not logged.

## Mapping (link → contact)

| What in the manifest | What in OO CRM |
|---|---|
| accept link (kind person/company) | **Person** contact: `firstName/lastName` from `name` (fallback — the local part of the email, `mailconv.SplitPersonName`); `about` — summary |
| link email | `AddContactInfo` infoType email, category Work, **primary** (idempotency key, lowercase) |
| source | contact tag `2dph:network:<target-email>` (created if missing) |
| link strength | `about`: `{msgs} mails / {threads} threads / {replies} replies, period {period}` |
| premises (first 5 + extraPremises) | `about`: `premises: mail <Message.id>; commit <repo:sha>; … (+N more)` — stable key in kb.lbug |
| manifest source | `about`: `source: <manifest.Source>` (text from the manifest) |

A "business link" in OO = the existence of a contact with the email + the source tag
(OO has no native Person↔Person record — we do not invent one). The full manifest
(all premises, projects) — an artifact outside CRM (`--json` in bin/network).

## Class boundaries (per N-1.1 #268)

- **kind=service** (GitLab/PayPal/LinkedIn service/markets-platform/
  trackers/mailing lists) — skipped (`skipped`), do not go into CRM;
- **linkedin relays of people** (`hit-reply@linkedin.com` with a real display
  name: Ruby Rodriguez etc.) — Person as is: email = platform relay,
  name = real person (N-1.1 classifier: person);
- **role mailboxes/companies** (`info@`/`alle@`, organizations) — also a Person
  contact (fallback name from the local part): company grouping by domain
  (`--companies`, N-1.4 #271) links them to a company; matched existing ones
  get the tag/about additively (`--backfill`, N-1.4 #271).

## Idempotency

Key = email (lowercase). One pass over CRM (`BuildContactEmailIndex`);
an existing contact by email (including those imported from VCF/MAB #85) — **skip**
(`matched`), not overwritten (we do not touch manual edits). A repeated
run = 0 creations / 0 changes. If attaching the email/tag fails, the
just-created contact is deleted (rollback) — "created" = a fully
created contact, a repeat does not create a duplicate.

## N-1.4 #271: backfilling matched (--backfill) and company grouping (--companies)

N-1.4 steps are performed by the same tool AFTER creation (see the examples
above); each step is idempotent, report-only/dry-run writes nothing by default.

### Backfilling existing matched (`--backfill`)

Owner decision (#267, question 3): an existing contact by email (incl. the
VCF/MAB #85 import) is **not silently skipped**, but gets additively:

- the tag `2dph:network:<target>` — if the contact does not have it;
- the about-premises summary from the manifest — **only if about is empty** (manual
  edits, incl. Org from #85 in about, are not overwritten).

The mechanism is by the email key from the manifest (no hardcoding to
ids); reused by N-1.5 #275 for eslider@. Tag state — a set of ids under the tag
(`ListContactsByTag`), about/names — `GetContact`. Repeat = 0 changes
(`updated=0 unchanged=N`). Report: `updated / would (dry-run) / unchanged /
missing / failed`.

### Company grouping of role mailboxes (`--companies`)

The "email domain → company" mapping — a YAML file, e.g. `companies-wheregroup.yml`:

```yaml
wheregroup.com: WhereGroup
```

Only **role mailboxes/organizations of the manifest** are linked (`kind=company`:
alle@, info@, wartung@…), whose domain is in the mapping: `FindCompany` → if
absent, `CreateCompany` (both idempotent by normalized name) →
`UpdatePerson` with `companyId`. **We do not touch person-kind** (real people are not a
"role mailbox"); company-kind without a rule remains a Person contact as is
(`unmapped`, in pilot #270 — events@suse). Repeat = 0 changes (`found`,
`already`, `unmapped`). Report: `found / created / linked / already /
notfound / unmapped / failed` (+ `missing / wouldlink` in dry-run).

The company is looked up by the normalized display name (without GmbH suffixes and
slogans, go-onlyoffice `CompanyGroupingKey`) — **the Name value in the mapping
must match the real company in CRM** (analysis — dry-run: found vs
missing); if the name does not match, the dry-run will show `missing`, and the mapping
is fixed before `--write` (otherwise CreateCompany will create a duplicate legal entity).

## Code and tests

- `internal/network/manifest.go` — the `network.Manifest` contract
  (shared by the producer `bin/network` and the connector; previously — crmDoc/crmLink in
  bin/network);
- `internal/ooimport` (cgo-free): `ParseManifest` → `BuildPlan` (mapping,
  service filter, tag, about, link kind) → `Run` (lookups + creation,
  dry-run/limit); N-1.4: `RunBackfill` (backfilling matched, additively) +
  `RunCompanies` (`ParseCompanies`/`GroupCompanyLinks`, linking role mailboxes
  to a company by domain);
- CLI `bin/onlyoffice/import-network.go` (shebang, tag
  `onlyoffice_import_network`);
- tests: offline (`internal/ooimport/*_test.go` — plan/mapping/roundtrip
  producer-consumer, split by existing: repeat = 0 new; ParseCompanies,
  GroupCompanyLinks, BackfillChanges additivity) + integration
  (`//go:build integration`, live OO, creds from the environment, without creds —
  skip; created test contacts are deleted at the end; the tag stays
  0-count — the client API does not delete tags); SplitPersonName cosmetics
  (mailconv) — the trailing " (Org)" decoration is stripped before parsing the name.

## Links

- Epic N-1 #267; children: N-1.1 #268 (filter), N-1.3 #270 (pilot), N-1.4 #271
  (companies).
- Network: docs/brain/graph-network.md; filter: docs/brain/crm-network-filter.md.
- go-onlyoffice: primitives FindPersonByEmail/BuildContactEmailIndex/
  CreatePerson/AddContactInfo/CreateContactTag/AddContactTag.
