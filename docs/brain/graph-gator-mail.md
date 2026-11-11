# Graph: gator kind=mail → 2dph Message/Person (D-1, ADR-0013)

Design document for the connector and the transfer of the conversation canon (#99) into Ladybug.
D-1.1 (#258). Goal: 2dph builds the Message/Person/edges graph from the gator
kind=mail canon, not from the raw var/mail corpus (ADR-0013).

## 1. Input: gator kind=mail (mail canon)

- **Where**: `/mnt/8TB/projects/produktor/gator/var/gator/parquet/mail/source=mail/channel={wheregroup,gmail}/dt=*/data_*.parquet`
  (hive directory on the same host arc-01; 2dph and gator share /mnt/8TB).
- **Live channels**: `wheregroup` (3984 unique, andriy.oblivantsev@wheregroup.com),
  `gmail` (18007, eslider@gmail.com).
- **Reading**: `query.Mail` (gator internal/query/mail.go) — SQL over parquet
  via `MailFilter` (latest per message_id by default; `History=true` all
  versions; `Limit` default ~100 → pagination needed). Or directly
  `read_parquet(glob, union_by_name=true)` — faster for the full channel, bypassing
  gator pagination. The 2dph connector reads **only the gator canon** (parquet/mail),
  NOT the raw `var/mail`.
- **Tombstone**: `deleted=true` (G-9.3) — latest by default excludes
  deleted items. On re-sync 2dph must account for this: a mail deleted in
  Roundcube → excluded from the graph (or marked), History sees the deleted one.

## 2. Mapping gator row → canon.Message

gator row kind=mail (query.Mail columns, agreed in G-9.2 #89):

| gator row | canon.Message (internal/canon #99) | note |
|---|---|---|
| message_id | Message.ID | already the Message-ID canon (CanonMessageID/Fallback) |
| folder | — (attribute) | a field on the node, not in the edges |
| from {name,email} | Message.From (Person) | gator already parsed it — MIME parsing NOT needed |
| to/cc/bcc []{name,email} | Message.To/CC/BCC ([]Person) | as above |
| in_reply_to [] | Message.ReplyTo / ThreadID | see §3 |
| date (RFC3339) | Message.SentAt | |
| subject | — (attribute) | on the node, if needed |
| body | Message.Body | processed body (without quotes) |
| content_hash | gator_ref | deeplink `kind=mail#v-<hash8>` |

Person.ID for mail = lowercased email (like canon Person), Name — display name
from gator.

## 3. Thread_id / REPLY_TO

- The gator read schema has no References — only `in_reply_to` (list of Message-IDs
  that the mail replies to).
- Thread strategy: the Message node carries `thread_id`. Root = a mail without
  in_reply_to (or with an empty one). REPLY_TO edge: `(m)-[:REPLY_TO]->(parent)` by
  in_reply_to. Thread grouping for Person↔Project↔Time (L-9.5 #234) is built
  transitively over REPLY_TO (contact network client), not materialized as a separate
  Thread node in the first stage (YAGNI until requested).
- fallback: mails without in_reply_to and without References → root message
  (thread_id = message_id).

## 4. Moving #99 into the Ladybug schema

- **Model/edges**: `canon.Person`/`canon.Message`/`Message.Edges()` (SENT/TO/
  CC/BCC/REPLY_TO/PART_OF) — reused **as-is** (already in main, golden test).
- **Ladybug schema** (InitSchema, internal/brain/write.go) — add additively:
  - `Message` node table (id STRING PK = message_id; thread_id, folder,
    subject, sent_at, gator_ref, body, ...);
  - rel tables `SENT (FROM Person TO Message)`, `TO/CC/BCC (FROM Message TO
    Person)`, `REPLY_TO (FROM Message TO Message)`;
  - `Person` node table — ALREADY EXISTS (id/name/email), do not create it again;
    fill it from gator.
  - Paragraph/PART_OF (from canon.Edges) — do NOT include at the first stage (AD decision:
    the Person/Message adjacency graph is primary; parsing bodies into Paragraph — later
    when semantics are requested, YAGNI).
- **Write**: Ladybug `MERGE` by id (idempotent, like WriteCorpus). Not the
  store.go JSON manifest #99 — that role is taken over by Ladybug.
- **Increment**: re-sync is idempotent (MERGE); tombstone → node marking
  or exclusion.

## 5. Connector (architecture)

```
gator parquet/mail (channel X) --read_parquet/query.Mail--> []canon.Message
    └── canon.Message.Edges() ──► Ladybug MERGE (Person/Message + edges)
```

- New `bin/mail/graph.go` (or an extension of the existing import path):
  reads a channel → canon.Message → MERGE into kb.lbug.
- Read path: direct `read_parquet` (full channel without gator pagination) —
  preferable for 3984/18007; query.Mail — for pinpoint/incremental reads.
- Config: channels from gator (like registry), hive path.

## 6. What from #99 is NOT transferred to the gator path

- `frommail.go` (emersion/go-message MIME parsing) — gator has already parsed from/
  to/cc/bcc/body; do not pull in the dependency, do not write a duplicate.
- `store.go` (JSON manifest on disk) — replaced by Ladybug MERGE.
- `fromchat.go` — chats as a separate channel later (outside D-1), the model is unified.

## 7. Deliverables / next step (D-1.2)

- Message/edges schema in InitSchema + write path (MERGE) — TDD.
- wheregroup importer (3984) → pilot → gmail (18007).
- Size/time estimate: 18k mails ~ a plain MERGE (seconds to minutes).

## 8. D-1.2 implementation (#259) — schema and write path

Code-only (not the import; the import is D-1.3 #260).

**InitSchema** (internal/brain/write.go) — additive, `IF NOT EXISTS` only,
nothing is DROPped:

```cypher
CREATE NODE TABLE IF NOT EXISTS Message (
 id STRING, thread_id STRING, folder STRING, subject STRING,
 sent_at STRING, gator_ref STRING, body STRING,
 PRIMARY KEY(id))
CREATE REL TABLE IF NOT EXISTS SENT     (FROM Person TO Message)
CREATE REL TABLE IF NOT EXISTS TO       (FROM Message TO Person)
CREATE REL TABLE IF NOT EXISTS CC       (FROM Message TO Person)
CREATE REL TABLE IF NOT EXISTS BCC      (FROM Message TO Person)
CREATE REL TABLE IF NOT EXISTS REPLY_TO (FROM Message TO Message)
```

The `Person` table already existed (id/name/email, PK id) — it is not recreated.
`Message.id` = message_id, `gator_ref` = deeplink `kind=mail#v-<hash8>`.
Paragraph/PART_OF are NOT materialized at this stage (D-1.1 decision, YAGNI).

**Write path** (internal/brain/graphplan.go — pure derivation, cgo-free;
graph.go — execution via execParams, cgo):

- Input `MessageInput{ canon.Message; Folder; Subject; GatorRef }` — canon #99
  is used as-is, edges are taken from `canon.Message.Edges()`.
- `planGraph`: unique Person nodes (order From → To → CC → BCC, name of the
  first occurrence; a repeated sync overwrites the name with the last one — SET in
  MERGE) and `Edges()` minus PART_OF and self-REPLY_TO.
- The write is idempotent: `MERGE (n:Label {id:$id}) SET ...` for nodes and
  `MATCH (a..), (b..) MERGE (a)-[:REL]->(b)` for edges — a repeated run
  yields 0 new nodes/edges.
- REPLY_TO to a parent that is not imported yet is silently skipped (MATCH does not
  find the end) — the edge appears on a repeated run after the parent is
  imported; the D-1.3 importer orders by date (parents before replies).
- Transactions: `UpsertMessages` writes a batch in one BEGIN/COMMIT (pattern
  `AddLeafs`).

**Read**: the read path (search/get/stats/audit, P-9.4 #240) is untouched; Message/
Person nodes are read with plain Cypher (`MATCH (m:Message)-[:TO]->(p:Person)`),
P-9 clients (#241) work on top of the Leaf contract as before.

## Links

- ADR-0013 (mail canon → gator; 2dph = Message/Person/Leaf graph)
- ADR-0012 (§contact network), domain-model.md (Message/Person #99)
- gator: G-9 (#87), query.Mail (#89), tombstone (#90), wheregroup/gmail channels
- 2dph: P-9.6 #242 (memory ADR — referenced, not duplicated), L-9.4 #233
