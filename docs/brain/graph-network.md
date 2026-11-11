# Graph network: "who with whom and through whom" from mail+commits (L-9.5 #234)

Design of the network layer on top of the ready kb.lbug graph (mail D-1 #257: Message/
Person + SENT/TO/CC/BCC/REPLY_TO; git L-9.4 #233: Commit/Person/AUTHORED).
The goal of epic L-9 (#229, ADR-0012 §contact network): reconstruction of the social-
professional network — "who I worked with and through whom, on what, when" — for
CRM/marketing. The tool is read-only: it does not write the graph, does not change the schema.

## 1. Derivations (only from existing nodes/edges)

For the target Person (email), links with other Persons are derived:

| Channel | Derivation | From nodes/edges | Weight |
|-------|-----------|----------------|-----|
| mail direct | a mail where target is the sender and Q is the recipient, or vice versa | SENT, TO/CC/BCC | TO 1.0 / CC 0.5 / BCC 0.25 |
| mail shared | a mail where target and Q are both recipients (the sender is a third party) | TO/CC/BCC | +0.25 |
| mail reply | a mail where Q replies to target's mail (and vice versa) | REPLY_TO | +0.5 |
| mail thread | the number of thread_ids where both participate | Message.thread_id | context |
| git project | target and Q both AUTHORED commits in the same repo | Commit.repo, AUTHORED | separate fact |

Person.id = email lowercase (mail canon D-1 #257; git authors are joined by
email in L-9.4 #233) — the same email in mail and git = one Person node.

## 2. Premises and verdict (audit card by Vinogradov, ADR-0012)

Each link fact carries:

- **premises** — references to Message.id (mail) / Commit.id `repo:sha` (git);
  without premises the link is not derived (sufficient reason, ADR-0012 item 1);
- **verdict**:
  - `accept` — ≥2 direct mails, or ≥1 REPLY_TO dialogue, or a shared project
    (both sides AUTHORED in repo); exported to CRM;
  - `weaken` — a single contact (1 mail without a reply and without a project) or
    only a shared recipient; gap `OPEN`, does NOT go into the CRM export (ADR-0012
    item 4 "verified links only"; item 2 "1 mail ≠ a stable link");
- **inference** = `deduction`; **period** = min..max of the evidence dates.

## 3. Code

- `internal/network/network.go` (cgo-free): `BuildLinks(Rows, Filter)` — all the
  derivation, deterministic (sorted by weight, tie-break email).
- `internal/network/query.go` (cgo, read-only): `LoadRows(conn)` — reads
  Message/Person/edges + Commit/AUTHORED into network rows.
- `bin/network/network.go` (cgo, read-only): CLI.

```bash
bin/network/network.go --person eslider@gmail.com
bin/network/network.go --person eslider@gmail.com --json
bin/network/network.go --person alice@x --project demo --since 2026-01-01
bin/network/network.go --person alice@x --accept-only     # CRM export YAML
```

Flags: `--person EMAIL` (required), `--project REPO` (git axis: only links
with a shared project), `--since/--until`, `--limit`, `--json`,
`--accept-only` (YAML accept only, without duplicates — one record per link,
aggregate of the mail+git channels), `--exclude-services` (without service
accounts, kind=service, N-1.1 #268), `--db PATH` (default `<root>/var/kb.lbug`).
`--depth` is reserved (pilot #234 — only direct links, depth=1).

## 3a. Link classification: people/companies vs services (N-1.1 #268)

Each link gets a `kind` field: `person` | `company` | `service`
(classifier `mailconv.ClassifySender`, rules — in
[docs/brain/crm-network-filter.md](crm-network-filter.md)). Service accounts/
subsystems/mailing lists (GitLab, PayPal, LinkedIn, markets-platform,
chiliproject@trac and the like) do not get into the CRM export: `--accept-only`
excludes `kind=service` (previously they got into the export by verdict accept).

## 4. Export to CRM

`--accept-only` → YAML: target + list of accept links (person, name, kind,
msgs, threads, replies, period, projects[], premises[] — the first 5 +
extraPremises counter; the full list in `--json`). No duplicates: each link
appears once with all channels. Service links (kind=service) are excluded
(N-1.1 #268). The source is marked `source: 2dph graph mail+git (L-9.5 #234)`.
The format — type `network.Manifest` (`internal/network/manifest.go`), a shared
contract with the connector.

**Write to CRM (N-1.2 #269):** `bin/onlyoffice/import-network.go` reads this
manifest and writes each non-service link as a Person contact
(email/name/tag `2dph:network:<target>`/about with a summary and premises),
idempotently by email. Mapping and commands — [docs/brain/oo-network-import.md](oo-network-import.md).

## 5. Boundaries (what is NOT derived)

- "what exactly was discussed" — from the mail body (Paragraph/PART_OF are
  not materialized, D-1.1 YAGNI);
- transitive chains "friends of friends" — depth>1 outside pilot #234;
- email aliases (one person, different addresses) — the graph joins strictly by
  email (L-9.4 #233), alias resolution is a separate task;
- mail ↔ repo binding (a mail "about project X") — the graph has no such edge,
  it is not invented.

## Links

- Epic L-9 #229; body L-9.5 #234 (pilot/acceptance).
- D-1 #257 (mail graph), L-9.4 #233 (Commit/Person/AUTHORED, git-facts).
- ADR-0012 §contact network (premises/verdict/accept export), ADR-0013.
- docs/facts/audit-recipes.md (audit card template).
