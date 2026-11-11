# Graph: git-commits → 2dph Commit/Person/AUTHORED (L-9.4 #233)

Design and implementation of the graph filler Commit/Person/AUTHORED from the git
history of the git.produktor.io repositories (eSlider/*, produktor/*). The goal of epic
L-9 (#229) — deduction "who worked on what, when, what they did": a source of facts for the
audit and the "project/time" axis of the contact network (L-9.5 #234, ADR-0012).

## 1. Place in the graph: two layers

| Layer | What | Path | Node |
|------|-----|------|------|
| premises | git history as Leaf text (kind=commit, source=git) | `internal/corpus/git.go` → `gitlog.Log`/`ToLeaf` → `bin/brain/import-git.go` | `Leaf` |
| graph | deductive layer: Commit/Person/AUTHORED | `internal/gitgraph` → `bin/git/graph.go` → `brain.UpsertCommits` | `Commit`, `Person`, `AUTHORED` |

Leaf and the Commit node are different graph layers (ADR-0012): git — premises for
deduction; the Commit node — the result. The duplicate corpus (`var/corpus/git/*.md` +
`2dph__corpus__git__*`) is eliminated (#10/P-9.3), the texts of the old #233 bodies about it
are outdated.

## 2. Source of git facts

- **Repositories**: local clones of git.produktor.io. The `--root DIR` scan —
  single-level over directories with `.git` (algorithm `corpus.Git.resolveRepos`),
  or an explicit `--repo path[,path]`. Repository name = `gitlog.RepoName`
  (base of the origin remote name; without origin — the directory name): stable when
  the clone moves.
- **Reading history**: `gitlog.Log` (go-git, no git binary) — the same canon
  as the premises corpus: **merge commits are skipped**, `Subject` = the first
  line of the message, author = `Author` (not committer), date = `Author.When`
  RFC3339. There is no duplicate reading — the connector reuses gitlog.
- **Git commit canon**: uniqueness by the pair `(repo, sha)` — a full sha
  is unique within a repository; the same sha in different repos (fork/copy) —
  different nodes. `Commit.id = <repo>:<sha>` (FileID format `repo:path`).
  Immutable history stays in the git repos; gator does not export commits (P-9).

## 3. Mapping into the schema

`gitlog.Commit` → `brain.CommitInput` (`internal/gitgraph.ToInputs`):

| gitlog.Commit | CommitInput / node | note |
|---|---|---|
| sha | `Commit.id = repo:sha` | canon |
| repo (RepoName) | `Commit.repo` | like leaf.Repo of the git corpus |
| subject (first line) | `Commit.subject` | "what they did" — a hypothesis from the message |
| author name | `Commit.author` | display name |
| author email | `Commit.email`, `Person.id` | **lowercase** — mail Person.id canon (D-1 #257): the same email in mail and git = one Person |
| author date | `Commit.date` | RFC3339 |

A Person without a display name in the mail graph gets the name from the git author
(name merge "last sync wins", SET in MERGE). A commit without an author email stays a
Commit node without AUTHORED (joining by email is impossible).

Edges: `(:Commit)-[:AUTHORED]->(:Person)`. There are no Commit→File/Leaf edges
(YAGNI, body #233): the project area at this stage = `Commit.repo`; no Project
table is introduced (it is derived from repo/files — decided by the #234 design).
Write ordering — by date ASC (parents first; flat commits — there are no edges between
commits), tie-break by id.

## 4. Write path

- `internal/brain/commitplan.go` (cgo-free): `CommitInput`, SQL forms
  `commitUpsertQuery` (MERGE Commit by id + SET properties),
  `authoredEdgeQuery` (MATCH Commit+Person + MERGE AUTHORED),
  `commitAuthor` (Person.id = email lowercase, name = author).
- `internal/brain/gitcommit.go` (cgo): `UpsertCommit`/`UpsertCommits` — a batch in
  one transaction (pattern `UpsertMessages` D-1.2 #259). Idempotency —
  MERGE by id (repo:sha / email): a repeated run gives 0 duplicates. `InitSchema`
  (write.go:130-133) already creates Commit/Person/AUTHORED — the schema does not change.
- CLI `bin/git/graph.go` (pattern `bin/mail/graph.go` D-1.3 #260): dry-run by
  default (`--commit` writes), `--repo`/`--root`, `--since`, `--limit`,
  `--db`, `--skip-existing` (incremental over existing Commit.id), live-holder
  guard (`--force`), `--json`, batches of 500. Report: dry-run — commits per repo /
  unique emails / weak subjects; commit — Commit/Person/AUTHORED before→after.

## 5. Deduction (git-facts tool)

`bin/git/facts.go` (read-only, RO-open in parallel with the live service): reads
`(:Commit)-[:AUTHORED]->(:Person)` triples and emits fact cards
"who worked on `<repo>` in `<period>`" (internal logic
`internal/gitgraph.GroupFacts`/`BuildFacts`).

Fact card (Vinogradov, audit card):

```
repo=2dph person=Ada Lovelace <ada@example.com>
  claim: «Ada Lovelace» worked on 2dph in 2026-08-10..2026-08-12 (3 commits)
  gap: OPEN: 1 of 3 commits has a service message — their "what exactly" is not derived
  inference: deduction; verdict: accept
```

Boundaries of deduction (otherwise fallacy, docs/audit-recipes.md):
- "what they did" — a hypothesis from the commit subject, NOT a fact about intent;
- weak/empty subject ("Update", WIP) → only "touched files", the card carries
  weaken + gap OPEN (the whole subject set is weak → verdict weaken);
- authorship by Author; merge commits are not imported (gitlog.Log),
  co-author in the message body is invisible — a limitation (OPEN);
- 1 commit ≠ "the whole week on X" — no hasty induction is built (the period is
  from the actual commit dates).

## 6. Joining with the mail graph

Person of the mail graph (D-1 #257, id=email lowercase, ~3 064 nodes) and git authors
with the same email merge into one Person node (MERGE by id): the author's git commit
gets AUTHORED to the already existing Person from mail. An unknown email —
a new Person (name from the git author). Result: one Person node per email, edges
SENT/TO/CC/BCC/REPLY_TO (mail) + AUTHORED (git) coexist.

## 7. Tests

- cgo-free unit (`internal/brain/commitplan_test.go`): forms of the MERGE queries,
  author derivation (email lowercase), no-email;
- cgo-free unit (`internal/gitgraph`): repo:sha canon, mapping
  gitlog.Commit → CommitInput, dedup/sorting (idempotency of a repeat),
  Resolve (root scan/explicit), ReadAll (go-git fixture), WeakSubject, Stats,
  GroupFacts/BuildFacts (filters, accept/weaken+OPEN verdicts, premises);
- cgo on the live Ladybug (`internal/brain/gitcommit_test.go`, temp DB): writing
  Commit/Person/AUTHORED and re-reading, a repeated run = 0 duplicates,
  mail↔git Person joining (SENT + AUTHORED on one node), no-email,
  coexistence with Leaf/Message + EnsureIndexes.

## 8. Contact network (L-9.5 #234) — done

Network layer on top of the graph (mail D-1 #257 + git L-9.4 #233): tool
`bin/network/network.go` "who with whom and through whom" — Person↔Person by shared
mails/threads (SENT/TO/CC/BCC/REPLY_TO) and shared projects (AUTHORED
Commit.repo — the "project/time" axis), premises (Message.id/Commit.id), verdict
accept/weaken, export of accept to CRM (ADR-0012 item 4). Design and boundaries:
[docs/brain/graph-network.md](graph-network.md). Remaining after #234 (not in
scope): grouping files → project area (currently the area = repo),
email aliases, transitive chains depth>1.

## Links

- Epic L-9 #229, body L-9.4 #233 (deductive pattern, boundaries).
- D-1 #257 (mail graph, Person.id=email), D-1.2 #259 (write path/InitSchema),
  D-1.3 #260 (CLI importer, pattern).
- ADR-0012 (§contact network), ADR-0013 (graph base); docs/facts/audit-recipes.md
  (audit card template).
