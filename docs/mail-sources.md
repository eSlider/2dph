---
type: reference
status: current
related:
  - PLAN.md
  - docs/runbook.md
---

# Mail + contacts on /mnt/8TB — source inventory (issue #79)

Truth table: which on-disk mail/contact sources under /mnt/8TB are covered by the
import pipeline, and which are still blocked. The import pipeline itself: mbox/.eml →
`internal/mailconv` → `message.json`/`message.md` (corpus `var/corpus/mail`),
contacts — via `oow catalog scan-contacts` + CRM reconcile (#68).

## Sources

| Source | Path (under /mnt/8TB) | Format | Import status |
|---|---|---|---|
| TB profile Andriy (old ImapMail) | `contacts/Andriy Oblivantsev/emails/Thunderbird/Profilordner/{Mail,ImapMail}` | mbox tree | imported (wave-1): `tb-andriy-profile` |
| Outlook PST Andriy | `contacts/Andriy Oblivantsev/MS Office Outlook.pst` | pst | imported (#185): `pst-andriy` |
| .eml scattered per person | `contacts/<Person>/**.eml` | eml | imported (wave-1): `contacts-eml`, 125 files |
| Backup 128Gb: E-Mails dir | `admin/Backups/Backup 128 Gb Ubuntu 20.02/E-Mails/{andriy.oblivantsev@gridfactor.de,Local Folders,pska2160@gmail.com,viscreation@gmx.de}` | TB mbox | imported (wave-1): `tb-backup-128g` |
| Backup 128Gb: zip 2010 | `.../E-Mails/E-Mails(2010-07-07 18-35).zip` | TB mbox inside zip | imported (wave-1): `tb-2010-zip` (unpacked into a temporary root) |
| PST in the backup | `.../Documents/Vorlagen/MS Office Outlook.pst` | pst | imported (#185): `pst-backup-128g-vorlagen` (byte-identical to Andriy's PST) |
| VCF per person | `contacts/<Person>/**.vcf` + `Telegram Desktop/*.vcf` | vcf | partial: `oow catalog scan-contacts` |
| VCF in bulk | `contacts/Contacts VCF's/contacts-*.vcf` | vcf | partial: `oow catalog scan-contacts` |
| TB address books | `Profilordner/{abook.mab,history.mab}` | Mork (.mab) | parser exists: `pkg/contact/mab.go` |

Policy exceptions (we do NOT import, #79): `Aleksey Krylov` (someone else's
correspondence), Drafts/Templates/Trash/Junk/Spam/Unsent (including the German
Outlook names:
`Entwürfe`/`Vorlagen`/`Gelöschte Objekte`/`Junk-E-Mail`/`Postausgang`),
`*.msf`, `filterlog.html`, `soft/drivers/**/*.pst` (firmware).

## M365 inline attachments (#312)

`internal/mailsync` (Graph, `m365.go`) also stores inline attachments — they used
to be dropped. Layout rule:

- inline attachments live in the same `attachments/` as regular ones; the name
  is deterministic and collision-free: `inline-<hash8 graph-id>-<name>`
  (e.g. `inline-d7bdd545-image001.png`), so two `image001.png` of the same
  mail do not overwrite each other and do not clobber a regular attachment with
  the same name.
- in `message.json` an inline one is marked `isInline: true` and `contentId`
  (both `omitempty`); regular attachments serialize as before, older consumers
  (`internal/mailconv`, ETL, office-assistant `internal/mailcorpus`) ignore
  unknown fields.

## M365: all 13 tenant mailboxes (#321)

`compose.edelweiss.yml` → `M365_USERS` = 13 mailboxes (@edelweiss-pflegedienst.de):
`a, info, kontakt, m.schumann, pdl, ea, fax, handys, i.rudi, lbnr,
Lilia.Olenberg, Buchhaltung, rechnung`. Corpus:
`var/corpus/mail/m365/<localpart>` (localpart lowercased). The secret
`~/.config/brain/m365.env` `M365_USERS` is extended by the operator separately (not in git).

An ApplicationAccessPolicy refusal in Graph looks like `ErrorAccessDenied`
("Access to OData is disabled"); the sync logs contain none of these — the app reads
all mailboxes. The policy itself is legacy, Exchange PowerShell only
(`Get-/Test-ApplicationAccessPolicy`), EAC has no separate page; replaced by
App RBAC.

Exchange throttle: `rechnung@` periodically answers 429/503
(`ErrorDirectoryConcurrencyLimit` / `CommandConcurrencyLimitReached` /
`Application Request Queue Full`). Sync recognizes this in the client (`m365.go`) and
adapts (`sync.go`):

- the schedule is **one mailbox at a time** (concurrency 1): parallel Graph requests
  to one mailbox run into `CommandConcurrencyLimit` on their own; `m365Source`
  implements `Sequencer`, `Run` gives such a source exactly one worker;
- `Retry-After` (seconds or an HTTP date) is honoured and overrides exponential
  backoff with jitter (cap `RetryPolicy.MaxDelay`); the server `Retry-After`
  is capped at 60s so one mailbox cannot hang a run;
- a mailbox listing failure is retried a limited number of times (`RetryPolicy.MaxAttempts`), then
  the mailbox is skipped (logged to stderr, non-fatal), the others sync; the delta commit
  happens only for successful ones, so a skipped mailbox is caught up on the next run
  without duplicates;
- flags: `--throttle <dur>` — pause between Graph requests inside a mailbox
  (e.g. `250ms`), `--max-concurrency N` — how many mailboxes/sources to sync
  at once (default 1).

Idempotency: `message.json` is written to a deterministic path and
skipped on a repeat, so a mailbox retry does not create duplicates.

## mbox import

The splitter lives in `internal/mailconv` (`SplitMbox`, `SplitMboxDir`) — a single
implementation (rule #10), testable offline; `bin/mail/convert-mbox.go` is
a thin CLI wrapper. Output is content-addressed
(`<out>/<source>/<rel-dir>/<sha256:16>/<sha256:16>.eml`), so a repeated
run is idempotent (no duplicates, like the seen-set in #74).

```bash
./bin/mail/convert-mbox.go --in DIR --out var/corpus/mail --dry-run   # count
./bin/mail/convert-mbox.go --in DIR --out var/corpus/mail             # import
./bin/mail/import.go --from-eml var/corpus/mail                       # eml → md/json
```

source tags: `tb-backup/<acct>`, `tb-profile/<folder>` — via `--source` or a
top-level directory under `--in`.

## PST import (issue #185)

`bin/mail/import-pst.go` (a thin CLI wrapper; orchestration in
`internal/source` — adapter `source.PST` + `ImportPST`/`PlanPST`) reads the
`pst.*` section of the typed config: a list `sources: [{label, path}]`
(paths come from inventory #79, put them in `config.local.yml`), optional
`readpst` (binary), `out` (corpus, default
`var/corpus/mail/pst`), `state` (checkpoint, default `var/state/pst.json`).

For each source: `readpst -e <file.pst> -o var/tmp/pst/<label>` (the scratch
dir is wiped before every run — readpst is not idempotent on a non-empty
output directory), then each extracted `.eml` is copied content-addressed
into `var/corpus/mail/pst/<label>/<folder>/<sha256:16>/<sha256:16>.eml` and the tree is
converted via the shared `mailconv.FromEML` (source tag `pst/`). Idempotency:
sha256 seen-set in `var/state/pst.json` (driver `source.Sync`, pattern #97/#98)
+ content-addressed output. readpst inserts a random token
`LibPST-iamunique-<n>` into the MIME boundary on every run — the adapter normalizes
it so content IDs stay stable (otherwise a repeated run would create duplicates).

Policy #79 applies here too: Drafts/Templates/Trash/Junk/Spam/Unsent
folders (including the German names) are not imported (`mailconv.SkipFolder`).

```bash
./bin/mail/import-pst.go --dry-run    # plan without writing
./bin/mail/import-pst.go              # import (idempotent)
```

readpst (`pst-utils` package on Ubuntu 24.04, formerly `libpst-utils`): on a host without
root access it is installed locally into `var/dist/readpst` (dpkg-deb -x), the path
is set via `pst.readpst`. Result for both PSTs (the files are byte-identical —
the second one is a copy from the backup): 1 unique mail (an Outlook greeting from 2005) in
`Posteingang`, 78 VCF contacts + 6 ICS events extracted by readpst but
imported by other pipelines (#68); no excluded folders (they are empty).

## Mail incubator: legacy `.eml` → docker-mailserver (epic #250, issue #252)

Mail revision zone: ETL from the legacy corpora (`var/mail`, 37GB) goes **through the
mail-server** (docker-mailserver, `info@` reads the mailboxes as shared). Autosync into
gator `kind=mail` is epic B (outside A).

**Mailbox model (decision 2026-09-02, #252):** an incubator mailbox = the **historical
owner address** of the period, NOT an abstract "source". The wheregroup period →
`andriy.oblivantsev@wheregroup.com`; for future periods — a separate account for
each historical address (`eslider@gmail.com`, `…@viscreation.de`). Mails
are laid out by addressee: owner in `From` → **Sent**; owner in
`To`/`Cc`/`Delivered-To` → **INBOX**; owner nowhere (foreign/ticket lists,
unparsed) → **INBOX/Unmatched** (quarantine, handled separately, not
thrown away silently). The account's service folders are the standard ones (Sent/Drafts/
Trash/Junk, like any normal mailbox). The A1 mailbox `wheregroup@produktor.io`
(an abstract source) was a collapsed model: mails were moved into
`andriy.oblivantsev@wheregroup.com`, the account deleted (#252).

`bin/mail/incubator.go` (a thin CLI wrapper; orchestration in `internal/incubator`
— `Run`/`Scan`/`MailboxOfDir`/`LayoutOf`) reads the `incubator.*` section of the
typed config: `imports: [{label, source, user, owner, state}]`
(corpus paths come from inventory #79, put them in `config.local.yml`), `docker`,
`container`.

Pipeline per source (label = owner; wheregroup → `andriy.oblivantsev@wheregroup.com`):

1. **Scan**: walk `**/*.eml` of the TB profile (numeric id directories = mails;
   copies under `attachments/` are skipped — in wheregroup this is the only .eml without
   a Message-ID).
2. **Canon**: Message-ID from the header (stdlib `net/mail`, header-only): the first
   token, trim `<>`, lowercase — the gator `kind=mail` contract key (epic B).
   Mails without a Message-ID → fallback `body-sha256:<hex>` of the body, flagged in
   the log. (emersion/go-message is NOT used for this: it fails on the legacy
   charset `iso-8859-15` in wheregroup headers — the regression was recorded during
   the first live run.)
3. **Layout** (when `owner` is set): `LayoutOf` — owner in From → Sent,
   owner in To/Cc/Delivered-To → INBOX, otherwise INBOX/Unmatched. Address lists
   are parsed (addr-spec, case-insensitive); if a value cannot be parsed —
   substring fallback, so a legacy mail is attributed to the owner.
   Without `owner` — legacy mode: flat into INBOX or a folder tree (`--folders`).
4. **Dedup/idempotency**: the manifest `var/state/incubator-<label>.json`
   (Message-ID canon → path + folder) — the source of truth. A repeated run with the
   same `--limit` yields 0 new. `doveadm save` itself does **not** deduplicate
   (verified live, 2026-09-02) — we do not rely on doveadm. The checkpoint is written
   atomically every 25 imports (crash-resume). `--force` recreates the manifest
   (re-layout after an account/model change).
5. **Global dedup vs already imported sources** (gator #101):
   `import.skip_state` — a list of read-only manifests whose keys are considered
   already imported (wheregroup+gmail are already in gator kind=mail; plus adjacent
   slices of the same corpus). The import skips a Message-ID canon that already
   arrived from another channel (there are no two identical raw mails in different channels);
   the counter `already-other` is in the report. With `--force` foreign keys are NOT reset —
   re-importing one slice does not duplicate a mail already lying in gator from
   another channel.
6. **Sender filter**: `import.skip_from` — mails with From from these addresses
   (addr-spec, case-insensitive) are skipped before dedup: not imported and
   not written to the manifest (counter `filtered`) — the Loewe newsletter
   `gewinnspiel@loewe.de` (92% of viscreation@gmx_de), owner's decision
   2026-09-05.
7. **Multi-owner passes**: `import.owner_strict` — a mail where the owner is in none of
   From/To/Cc/Delivered-To does not go to INBOX/Unmatched but is skipped
   (counter `foreign`) — it is imported by a pass of the neighbouring owner of the same
   tree (defacto/Local_Folders: eslider@gmail.com + viscreation@gmail.com
   in two passes, layout by Delivered-To).
8. **Import**: `docker exec -i mailserver doveadm save -u <user> -m <mb>` with
   the .eml on stdin — **no bind-mount of the legacy corpus into the container is needed** (decision
   on open question #250/Q3). Target folders are created with `doveadm mailbox
   create` (doveadm save does not auto-create a folder — verified live); after importing
   into new folders (INBOX/Unmatched, --folders tree) — a repeated run of
   user-patches.sh (info@ rights on the new folders).

```bash
./bin/mail/incubator.go --dry-run               # scan + plan (folders, count)
./bin/mail/incubator.go --limit 1000            # 1000 mails laid out by owner from the config
./bin/mail/incubator.go --owner <addr> --limit 1000  # layout by an explicit address
./bin/mail/incubator.go --limit 1000 --folders  # legacy: folder tree (no owner)
./bin/mail/incubator.go --limit 1086 --force    # re-import a window, recreating the manifest
```

Pilot result (2026-09-02, the "historical address" model): 4249
mail messages found (4250 .eml on disk, of which 1 is an attachment copy
`0000640/attachments/ForwardedMessage.eml`, skipped), unique Message-IDs
**3986** (263 duplicates in the corpus); into `andriy.oblivantsev@wheregroup.com`
1000 were moved (Sent/INBOX/INBOX-Unmatched — the layout is in report #252);
a repeated run → 0 new. Verification: `doveadm search -u
andriy.oblivantsev@wheregroup.com mailbox INBOX ALL` (count).

### defacto (multi-owner + global dedup, gator #101)

Import manifest for the 4 account directories (`var/mail/archive/defacto/`, owner's decision
2026-09-05): viscreation@gmx_de (19 994 eml, unique 19 649) →
mailbox viscreation@gmx.de with `skip_from: [gewinnspiel@loewe.de]`;
andriy_oblivantsev@gridfactor_de (671, unique 542) → andriy.oblivantsev@gridfactor.de;
Local_Folders (19 594, unique 15 014, mixed) → **two strict passes** —
eslider@gmail.com and viscreation@gmail.com (`owner_strict: true`, layout by
Delivered-To); pska2160@gmail_com (90) — a foreign account, not imported.
Each slice additionally deduplicates against `skip_state` = the manifests of
wheregroup + gmail + adjacent defacto slices — eslider mails from the gmail channel
are not imported again (global dedup, the Epic C criterion).

```yaml
# etc/brain/config.local.yml (machine-local paths, #79; do NOT commit)
incubator:
  imports:
    - {label: defacto-viscreation-gmx, source: "<defacto>/viscreation@gmx_de",
       user: viscreation@gmx.de, owner: viscreation@gmx.de,
       skip_from: [gewinnspiel@loewe.de],
       skip_state: ["<state>/incubator-wheregroup.json", "<state>/incubator-gmail.json"]}
    - {label: defacto-gridfactor, source: "<defacto>/andriy_oblivantsev@gridfactor_de",
       user: andriy.oblivantsev@gridfactor.de, owner: andriy.oblivantsev@gridfactor.de,
       skip_state: ["<state>/incubator-wheregroup.json", "<state>/incubator-gmail.json",
                    "<state>/incubator-defacto-viscreation-gmx.json"]}
    - {label: defacto-local-eslider, source: "<defacto>/Local_Folders",
       user: eslider@gmail.com, owner: eslider@gmail.com, owner_strict: true,
       skip_state: ["<state>/incubator-wheregroup.json", "<state>/incubator-gmail.json",
                    "<state>/incubator-defacto-viscreation-gmx.json",
                    "<state>/incubator-defacto-gridfactor.json"]}
    - {label: defacto-local-viscreation, source: "<defacto>/Local_Folders",
       user: viscreation@gmail.com, owner: viscreation@gmail.com, owner_strict: true,
       skip_state: ["<state>/incubator-wheregroup.json", "<state>/incubator-gmail.json",
                    "<state>/incubator-defacto-viscreation-gmx.json",
                    "<state>/incubator-defacto-gridfactor.json",
                    "<state>/incubator-defacto-local-eslider.json"]}
```

```bash
./bin/mail/incubator.go --dry-run   # plan for all slices (the figures after #101 are in the issue report)
```

Live import order: gmail/wheregroup are already in gator → viscreation-gmx (Loewe
filter) → gridfactor → Local_Folders eslider → Local_Folders viscreation
(each next slice puts its manifest into the `skip_state` of the following ones).

**Fill result (2026-09-06, live, gator #101):** **15 708** mails imported
(sum of the `incubator-defacto-*.json` manifests, the doveadm per-mailbox count
matches):

| slice | scanned | filtered (Loewe) | new/imported | already-other | foreign (strict) | rejected → imported |
|---|---|---|---|---|---|---|
| defacto-viscreation-gmx | 19 994 | 18 332 | **1 350** | 0 | — | 0 |
| defacto-gridfactor | 671 | 0 | **546** | 0 | — | 6 → 0 (after the 200M limit) |
| defacto-local-eslider | 19 594 | 0 | **7 400** | 881 | 8 911 | 8 → 0 |
| defacto-local-viscreation | 19 594 | 0 | **6 412** | 10 753 | 775 | 4 → 0 |

Rejected: mails >10M (the DMS default `quota_max_mail_size`) — the limit was raised to
200M (`POSTFIX_MESSAGE_SIZE_LIMIT` in the mail-server compose), a repeated run
imported them, re-run = new 0 (idempotent). Mails foreign to both Local owners
(neither eslider nor viscreation; the gridfactor/RPF era is outside the
`andriy_oblivantsev@gridfactor_de` directory) — **775**, skipped by `owner_strict`
(they did not get into the manifest and are not in gator). pska2160@gmail.com is untouched.
A repeated incubator run (after the fill): all 4 slices new=0, rejected=0.

### tb-2010 RPF era (gator #110, owner's decision 2026-09-09)

`tb-2010-zip/pop_osn_de` = the RPF work mailbox of 2007. Owner address: **ao@rpf.de** —
the short AO address of Andriy Oblivantsev (831 mails To/Cc/Delivered-To ao@rpf.de,
0 From as ao; `rpf30@pop.osn.de` — POP delivery, Delivered-To 839). Mails from RPF
colleagues to eslider@gmail.com (139) and foreign lists (wlgore/Gore-Tex)
were skipped by `owner_strict` (foreign 228) — the eslider part is covered by the gmail channel /
the deferred gmail_lenovo.

```yaml
    # etc/brain/config.local.yml
    - label: tb2010-pop-osn
      source: "<archive>/tb-2010-zip/pop_osn_de"
      user: "ao@rpf.de"
      owner: "ao@rpf.de"
      owner_strict: true
      state: "<state>/incubator-tb2010-pop-osn.json"
      skip_state: [ ... all 6 wheregroup/gmail/defacto-* manifests ... ]
```

**Fill result (2026-09-09, live):** new=619 (doveadm INBOX messages=619,
repeated run new=0). gator channel `rpf` (shared/ao@rpf.de): full scan
records=619 **written=563** (56 = content-hash duplicates with the gmail channel),
ETL Done=563 (gator_status). mail_google_de (early gmail viscreation/eslider
2006-07) and visauto are part of the gmail_lenovo superset and were imported with it.

### gmail_lenovo superset (gator #111, owner's decision 2026-09-09)

5 account directories `var/mail/archive/gmail_lenovo/`, one slice per
historical address; global dedup via `skip_state` against all already
imported manifests + adjacent slices:

| label | source | owner (mailbox) | new |
|---|---|---|---|
| gmail-lenovo-eslider | `imap_gmail_com` | eslider@gmail.com | 22 195 |
| gmail-lenovo-wheregroup | `192_168_2_10` | andriy.oblivantsev@wheregroup.com | 5 276 |
| gmail-lenovo-viscreation | `imap_gmail-1_com` | viscreation@gmail.com | 1 416 |
| gmail-lenovo-andriy-gmail | `imap_gmail-2_com` | andriy.oblivantsev@gmail.com (new) | 808 |
| gmail-lenovo-viscreation-de | `imap_viscreation_de` | viscreation@viscreation.de (new) | 0 (2 deduped) |

**Fill result (2026-09-10, live):** ~29 695 new mails; a repeated
dry-run of all slices new=0 (idempotent). The new mailboxes
andriy.oblivantsev@gmail.com and viscreation@viscreation.de were created in DMS
(passwords in `.env`, shared ACL via the user-patches `INCUBATOR_OWNERS`). gator
kind=mail: gmail 25 414→47 603, wheregroup 3 985→9 261, viscreation-gmail
6 412→7 828, the new channel `andriy-gmail` 808; corpus 38 569→**68 258**.
The first run aborted on eslider (14 533/22 195) — the manifest is written
incrementally, the repeated run imported the remainder without duplicates.

**Bugfix pack (gator #111):** COPY ... `PARTITION_BY (source, channel, dt)` on the
mail corpus (68k mails, 10 714 dt partitions) crashed with OOM even with memory_limit —
DuckDB held the buffers of all partitions. The fix in `internal/pack/pack.go`:
`preserve_insertion_order=false`, `threads=4`, `memory_limit='24GB'`,
`temp_directory` (spill), `partitioned_write_flush_threshold=1000`,
`partitioned_write_max_open_files=50` — COPY ~13s.
