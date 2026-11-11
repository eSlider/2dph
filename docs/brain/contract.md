# Leaf write contract in Ladybug (P-9.2 + P-9.3)

Single contract for writing a leaf into Ladybug for all corpus sources (mail,
git, chats, docs, facts). Formalization following the gator `contract.Record`
model (G-8.0, issue #73) — see also the shared versioning ADR P-9.6 (#16).

Implementation: package `internal/contract` (cgo-free) + corpus adapters
`internal/corpus` (cgo-free) + write path `internal/brain` (columns
`observed_at`, `external_id`; id = `ContentHash`).

## Leaf fields

| Field | Required | Meaning |
|------|-------------|-------|
| `source` | yes | corpus source: `mail` / `git` / `chats` / `docs` / `facts` (P-9.3: corpus name, not an evidence pointer) |
| `external_id` | yes | stable ref inside the corpus: message id / commit sha / chat message id / file rel-path |
| `kind` | yes | leaf type: `fact` / `mail` / `chat` / `commit` / `reference` / `seed` / … |
| `text` | yes | leaf content |
| `root` | no | `facts` (claim, ≥2 sources) or `info` (narrative); default `info` |
| `confidence` | no | `confirmed` / `estimated` / `inferred` / …; default `confirmed` |
| `source_rev` | no | source revision: `working-tree` / sha; default `working-tree` |
| `how` | no | how it was obtained: `kb/index`, `mail/import`, `git-log`, `facts/extract`, `brain/add` |
| `loc` | no | local context (path/repo); default = `source` |
| `valid_from` / `valid_to` | no | D24 validity day (YYYY-MM-DD) — **orthogonal to the version**: it is about the fact, not about the record |
| `observed_at` | no | when the content was **seen** (RFC3339); empty → stamped with `now()` on write; **not part of the dedup key** |

Required-field validation — `Leaf.Validate()`; an invalid leaf is cut at the
write boundary (before upsert).

## Dedup (ContentHash) — the DB id

The dedup key of the version **and the id of the record in the DB** —
`ContentHash()` = `sha256(source|external_id|kind|text)[:32]` (P-9.3: the
runtime id moved from `LeafID(text, source)[:24]` to the contract
`ContentHash` — one key, dedup key == id). Before hashing, the text is
normalized by `contract.NormalizeText` (ToValidUTF8 → LF → trim): the same
content from different paths (CRLF vs LF, trailing spaces) yields one id.
`observed_at` is deliberately outside the hash — gator semantics (G-8.0 #73):
the same content written later dedups to the same version (the first
`observed_at` is kept); the content changed → new key.

## Versioning

One version = one id. A change of content (text) or identity (source /
external_id / kind) → new `ContentHash` → new id, the old version stays in the
graph. Rewriting the same version — MERGE (upsert), without a duplicate.

`valid_from/valid_to` — validity of the fact by days (D24, `facts.NormalizeDay`),
used in `--as-of` search. This is not record versioning: the version is
determined by id/ContentHash, validity — by an interval.

## Ref format (external_id) — P-9.3

`source` = corpus name; `external_id` = stable ref; `loc` = evidence pointer
(path/repo) for FROM_FILE and the audit.

| Corpus | source | external_id | Example loc |
|--------|--------|-------------|------------|
| mail | `mail` | content-address `sha256(NormalizeText(text))[:16]` — one scheme for live (OO numeric id) and legacy (sha256:16 dir), so that one message from both corpora merges by ContentHash (#5.2) | `/var/corpus/mail/<folder>/<id>/message.md` |
| git | `git` | commit sha (full) | repository path |
| chats | `chats` | frontmatter `id` (first message id); no id → content-address | `.../var/corpus/chats/md/<platform>/<chat>/messages.md` |
| docs | `docs` | relative path in the repo (stable across machines) | absolute file path |

Leaf text = `Heading + "\n\n" + body` in all corpora (git moved from a single
`\n` to a double one, #5.3) — uniformity for ContentHash.

## Corpus adapters (P-9.3)

Each corpus is an adapter following the gator `Source` contract
(`internal/contract`):

```go
type Source interface {
    Name() string                          // mail | git | chats | docs
    Stream(ctx context.Context, emit func(Leaf) error) error
}
```

Adapters live in `internal/corpus` (cgo-free): `mail.go`, `git.go`, `chats.go`,
`docs.go`. The common index driver (`bin/brain/index.go`) collects leafs via
`corpus.StreamAll` and writes them with a single `brain.WriteCorpus([]contract.Leaf)`.
**Adding/removing a corpus = an adapter in `sources()` + `--rebuild`**
(deterministic ids = ContentHash → rebuild without manual magic).
The flags `--with-mail` / `--with-chats` / `--corpus` / `--git-root` enable the
corresponding adapters. facts remain a separate path (not a corpus but an
evidence gate) — the semantics "facts = ≥2 independent sources" is untouched.

The git duplicate is eliminated: git history is written only by the git adapter
(`var/corpus/git/*.md` and `2dph__corpus__git__*` in the docs corpus are excluded
by the docs adapter).

## Conformance audit

`bin/brain/audit-contract.go` — read-only audit against the live kb.lbug
(opens the DB with `ReadOnly=true`, does not hold the file, aggregates in a
single pass):

    ./bin/brain/audit-contract.go            # summary
    ./bin/brain/audit-contract.go --json     # full aggregates

Counts total, missing required/recommended fields, distribution by corpus
(grouping by the `source` field directly — P-9.3; on the live DB before the
rebuild source still contains old evidence pointers, such rows are shown as
they are). Run on the live DB
(2026-09-01, 313817 leafs): `missing_external_id: 313817` (the column does not
exist yet — the migration will be applied when the service starts),
`missing_valid_from: 10099`,
`missing_how/loc/kind: 4`, `observed_at: 0`.

## Known discrepancies (as of P-9.3)

- The live `var/kb.lbug` (313k leafs) has not been rebuilt yet: old ids
  (`sha256(source\0text)[:24]`), old source (evidence pointers), git duplicate.
  The rebuild is a separate step (devops, after merge): `index --rebuild
  --with-mail --with-facts [--with-chats] [--git-root DIR]`, full id rehash.
- The facts path (`factsFromJSON`) writes directly via `UpsertLeaf` without
  `FROM_FILE` — left as is, recorded by the contract as an allowed write path.

## Cross-ref

- G-8.0 gator write contract: `internal/contract` (gator), issue #73.
- Shared versioning pattern / ADR: P-9.6, issue #16.
- Corpus adapters + id rehash: P-9.3, issue #13.
- Skill `brain` (skills/brain) — how to use search and how to add a source.
