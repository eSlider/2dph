# CRM-network filter: people/companies vs service accounts (N-1.1 #268)

Deterministic classifier of the contact-network address (L-9.5 #234): **which
accept links are real business contacts (people/companies) and which are
service accounts/subsystems/mailing lists**, so that
GitLab/PayPal/LinkedIn/markets-platform/chiliproject@trac and the like do not
end up in OnlyOffice CRM.

Epic N-1 #267 ("graph → OO CRM"): N-1.1 (this filter) → N-1.2 connector →
N-1.3 wheregroup pilot → N-1.4 company grouping.

## Classes

| kind | Meaning | In CRM |
|------|----------|-------|
| `person` | human: personal address or display name = a real name | yes |
| `company` | organization/role mailbox (`info@`, `kundenservice@`, department, group) | yes (N-1.4 groups by domain) |
| `service` | automaton/service domain/subsystem/tracker/mailing list | **no** |

## Where it lives

- `internal/mailconv/classify.go` (cgo-free): `ClassifySender(ParsedAddress)`
  → `person|company|service`. The basis is mailconv sender heuristics
  (`IsMachineSender`, `junkDomains`/`junkLocalParts`, messages.go):
  the domain junk list is reused through the shared `isJunkDomain` (one
  implementation, rule #10). The network deliberately does NOT use mailconv's
  local junk SUBSTRINGS ("info@", "reply", "news") — see "Differences from
  IsMachineSender".
- `internal/network/network.go`: the `Link.Kind` field is filled in `BuildLinks`
  for every link (backward compatible: the field is additive, the old output
  does not break).
- `bin/network/network.go`: `--exclude-services` (shared filter) and
  `--accept-only` (CRM export) **exclude `kind=service`**; kind is printed
  in YAML/JSON/text.

## Rule cascade (first match)

1. Empty email / no `@` → `service` (guard).
2. **Platform relays for people** (LinkedIn InMail): `hit-reply` /
   `inmail-hit-reply` @ `linkedin.com` → `person`. The display name is a real
   person (Ruby Rodriguez, Johanna Eckerstorfer), the messages go through the
   platform relay, not from a personal address. It stands BEFORE junk domains
   (linkedin.com is junk). Digest relays (`messaging-digest-noreply@…`) — `service`.
3. **Service**:
   - mailconv junk domain (`isJunkDomain`: linkedin.com, github.com,
     google.com, facebook.com, amazon.com, slack.com and others) or a subdomain;
   - network service domains `networkServiceDomains` (from the real lists #268 +
     obvious account platforms): `gitlab.com`, `paypal.com`, `paypal.de`,
     `markets-platform.com`, `xing.com`, `djinni.co`, `wellfound.com`,
     `workablemail.com`, `coinbase.com`, `dynadot.com`, `hetzner.com`,
     `netlify.com`, `letsencrypt.org`, `steampowered.com`, `instagram.com`,
     `discord.com`, `microsoft.com`, `apple.com`;
   - local part automaton (suffixes after stripping `._-`): `noreply`,
     `donotreply`, `mailerdaemon`, `postmaster`, `mailrobot`, `mailer`,
     `robot`, `bounce(s)`, `newsletter`, `notifications`, `jobalert(s)`,
     `security`, `digest`, `alerts`; tokens: `news`, `marketing`,
     `subscribe`;
   - subsystem/tracker: tool name in the local part or the left label of the
     domain (`toolNames`: gitlab, projeqtor, chiliproject, trac, redmine, jira,
     confluence, wiki, mediawiki, apache, svn, jenkins, mailman, sympa,
     nagios, zabbix) — catches `gitlab@wheregroup.com`, `projeqtor@wheregroup.com`,
     `chiliproject@trac.wheregroup.com`, `apache@wiki.wheregroup.com`,
     `gitlab@gitlab`, `dev@trac.example.com`;
   - mailing lists: local part on `-request`/`-owner`, left label
     `lists` (`mapbender_dev-request@lists.osgeo.org`).
4. **Person**: the display name looks like a person's name — ≥2 tokens after
   stripping decorations (" (Org)", " via X"): Title-case / ALLCAPS / initials /
   particles (van/von/de/of/…); without corporate markers (GmbH/AG/e.V./Ltd/Inc/…)
   and role words (Team/Wartung/Neuigkeiten/Digest/…).
5. **Person**: local part `firstname.lastname@domain` (initial.lastname
   too), display name empty/uninformative.
6. Otherwise → `company` (role/organization). Conservative: we do NOT exclude —
   losing a real contact is worse than an extra role.

## Special exceptions and boundaries (OPEN)

- **Company-context domain ≠ service**: `wheregroup.com` is NOT one of the
  service domains — colleagues (`astrid.emde@wheregroup.com`) and role mailboxes
  (`info@wheregroup.com` → company) pass. Company subsystems on the same
  domain are caught by tool names/labels (`gitlab@wheregroup.com`,
  `trac.wheregroup.com`).
- **Differences from `IsMachineSender`**: mailconv's local junk substrings
  ("info@", "reply", "news" as a substring) are not applied in the network —
  otherwise `info@company` (role mailbox) and people's relays
  (`hit-reply@linkedin.com`) would be lost. IsMachineSender for reconcile tools
  is unchanged.
- **Commercial/consumer companies** (amazon.de, ebay, banks, telecoms,
  airbnb, opodo etc.) are classified `company` (they have goods-for-money
  relationships with the user, but this is not a "service account"); filtering
  such domains out of the CRM import is a separate owner decision (N-1.2/N-1.4).
- **Corporate domains of the giants** (google.com, microsoft.com, apple.com and
  others) — `service`: if a real person from such a domain becomes a business
  contact — a pinpoint exception is added.
- **People via relays**: the address is a platform relay (`messenger@webex.com`,
  `hit-reply@linkedin.com`), not a personal one; the contact is assigned the
  class by display name.
- **Two-word brands** that look like names ("BMW Deutschland") → person
  by shape; pinpoint errors are documented in the table on the issue.
- **The user's own aliases** (eslider@gmail.com in the wheregroup-target
  network) — an alias problem, out of scope (#267 YAGNI).

## Usage

```bash
bin/network/network.go --person andriy.oblivantsev@wheregroup.com --accept-only   # CRM YAML, no services
bin/network/network.go --person andriy.oblivantsev@wheregroup.com --exclude-services
bin/network/network.go --person eslider@gmail.com --json                          # full audit + kind
```

## Links

- Epic N-1 #267; body/acceptance N-1.1 #268; network L-9.5 #234 (docs/brain/graph-network.md).
- `mailconv.IsMachineSender` (internal/mailconv/messages.go), `mailconv.SplitPersonName`.
