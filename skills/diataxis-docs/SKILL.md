---
name: diataxis-docs
description: >-
  Classify and structure documentation by Diataxis (tutorial, howto, reference,
  explanation) with PARA status, using frontmatter that the knowledge-base index
  reads. Use when creating a document, when one file tries to do two jobs, or when
  auditing a docs folder.
---

# diataxis-docs

Every markdown file answers exactly one of four questions. A file that answers
two should be split; that is the whole method.

| Type | Answers | Reader is |
|------|---------|-----------|
| `tutorial` | "teach me by doing" | learning |
| `howto` | "help me do this task" | working |
| `reference` | "tell me the facts" | looking up |
| `explanation` | "help me understand why" | studying |

## Frontmatter

```yaml
---
type: reference          # one of the four above
status: current          # current | archive  (PARA)
related:
  - docs/OPS.md
---
```

`bin/kb/index` reads this. `type` becomes a searchable column. `related:` is
frontmatter for humans; graph hops from it are not implemented yet.

```bash
bin/brain/search.go "deploy"
```

## Audit checklist

1. Does the title match the type? A reference that starts "first, open..." is a
   howto wearing a disguise.
2. More than one H2 topic that a reader would search separately? Split the file.
   The indexer chunks on H2, so split files also search better.
3. Is `status: archive` set on anything superseded? Archived files stay indexed
   but stop competing with current ones for a reader's attention.
4. Does every explanation link the reference it explains, and vice versa?

## Rule

Do not invent a fifth type. If a document does not fit, it is usually two
documents.
