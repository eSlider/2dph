---
type: plan
status: draft
related:
  - docs/runbook.md
  - docs/design.md
  - PLAN.md
---

# Plan: rewrite mail import with a Go MIME email library + type-handler attachments

## Goal

Replace the current extension-only mail → markdown conversion with a proper
MIME email parser that:

1. Reads the **raw email** (headers + body + multipart) instead of relying on
   the pre-parsed `message.json` shape.
2. Preserves the **original email date** as a first-class, sortable leaf field
   (prerequisite for time-based deduction — see background below).
3. Determines **attachment type by MIME type + extension** and routes each
   attachment through a **type handler** (PDF/OCR, image/OCR, text, office,
   structured, unknown).

## Background / why

- The brain cannot do date-based reconstruction/deduction today: mail leafs
  carry **no date**. `msgDate()` (`internal/brain/corpus.go:222`) reads
  `message.json receivedDate/receivedAt` **only** as a `--since` sync filter;
  `WriteCorpus` (`corpus.go:240`) never sets `LeafInput.ValidFrom/ValidTo`.
  `LeafInput` and the Leaf schema already have `valid_from/valid_to` (D24).
- Attachment handling is by **file extension** only
  (`internal/mailconv/mailconv.go:69` `ConvertAttachment`), so a `.pdf` named
  `.txt` or an inline `image/*` part is mis-handled. MIME type + extension is
  more reliable.

## Library selection

Primary: **`github.com/jhillyerd/enmime/v2`** (v2.4.1, 2026).

- Purpose-built MIME email parser (Inbucket project), production quality.
- Parses headers, multipart/alternative, inline + attachment parts.
- Per-part: `ContentType`, `FileName`, `Disposition`, decoded `Content`.
- Header decoding (RFC 2047), charset handling.

Alternative: **`github.com/emersion/go-message`** (streaming RFC 5322/2045-2047,
charset via `.../charset`). Use if streaming memory profile is preferred.

Decision: **enmime/v2**. Rationale: highest-level email API, built-in attachment
decoding, active maintenance (v2.4.x 2026).

## Architecture

```
raw email (EML / .eml / oo mail body)
   │  enmime.ReadEnvelope(r)
   ▼
Envelope{ From, To, Date, Subject, Text, HTML, Attachments: []Part }
   │  normalize + StripHTML(HTML)→Markdown
   ▼
mailconv.Message{ id, source, folder, subject, from, to, date, body_md, attachments }
   │  per attachment: TypeHandlerRegistry.Route(MIME, ext) → handler
   ▼
message.md (frontmatter: date, from, to, subject, ...) + attachments/*.md
   │  WriteCorpus(LeafInput{ ValidFrom: date, ... })
   ▼
brain leaf (info, dated) → date-sortable search/deduction
```

## Type-handler registry (MIME + extension)

`internal/mailconv/handlers.go` — a deterministic map keyed by
`(mimeType, extension)` → handler func `(path, name, mime) (md string, err)`.

| Priority | MIME type | Ext | Handler |
|----------|-----------|-----|---------|
| 1 | `application/pdf` | `.pdf` | PDF: `pdftotext -layout`; if textless → `pdftoppm`+`tesseract eng+deu` (reuse `internal/ocr`) |
| 2 | `image/*` | `.png .jpg .jpeg .gif .webp .tiff` | image OCR (`internal/ocr.ImageFile`) when `--ocr` |
| 3 | `text/plain` | `.txt .log .csv` | pass-through (or CSV→markdown table) |
| 4 | `text/html` | `.html .htm` | `StripHTML` → markdown |
| 5 | `text/calendar` | `.ics` | `golang-ical` → summary events (already a dep) |
| 6 | `application/json`/`xml` | `.json .xml` | structured → code block |
| 7 | `application/vnd.openxmlformats-...` | `.docx .xlsx .pptx` | (optional) unzip+text extraction; else metadata-only |
| 8 | `application/msword`, `application/vnd.ms-excel` | `.doc .xls` | metadata-only (v1) |
| 9 | default / unknown | any | metadata-only: `name · MIME · size` |

Rules:
- Route on **MIME first**, extension second (MIME is authoritative).
- Handler failures must not fail the import → emit `<!-- <name>: <err> -->`.
- Registry is data-driven (table) so handlers can be added without touching
  the loop.

## Date preservation

- Parse `Envelope.Date` (enmime gives a parsed `time.Time`) → store in
  `mailconv.Message.Date`.
- Emit `date:` in `message.md` frontmatter (already present, keep).
- Set `LeafInput.ValidFrom = date`, `ValidTo = ""` in `WriteCorpus` so the
  leaf is date-queryable. (Schema already supports it; no migration.)

## File/layout changes

- `go.mod`: add `github.com/jhillyerd/enmime/v2`.
- `internal/mailconv/mailconv.go`: rework `Message` to hold MIME-derived data;
  `FromRaw` parse raw email via enmime, keep producing `message.md`.
- `internal/mailconv/handlers.go` (new): type-handler registry.
- `internal/mailconv/handlers_test.go` (new): fixtures per MIME/ext.
- `internal/brain/corpus.go`: `WriteCorpus` sets `ValidFrom` from `CorpusLeaf.Date`.
- `internal/brain/search.go` + MCP `search`: date filter (`--as-of` → apply to
  info/mail too) + `sort by valid_from asc|desc` + return date in `get`.

## Verification

- Fixtures: `.eml` files with multipart/alternative + attachments of each
  MIME/ext class; assert correct body + handler routing.
- Golden test: a known `.eml` → expected `message.md` + `attachments/*.md`.
- Re-import sample (e.g. `bin/mail/import.go --from-raw var/mail --ocr`) →
  leafs have `valid_from`.
- `bin/brain/search.go "defacto" --as-of <date>` returns dated mail; `get`
  shows the date; `--sort date asc` orders correctly.
- Memory: confirm enmime streamed parsing keeps RSS bounded on the ~18k corpus.

## Rollout

1. Add enmime dep + registry + unit tests (offline fixtures).
2. Rework `mailconv.FromRaw` to parse via enmime (keep output shape).
3. Wire `ValidFrom` in `WriteCorpus`.
4. Extend `search`/`get` for date filter + sort.
5. Re-import + rebuild the ~18k mail corpus.
6. Verify search/sort/deduction; document in `docs/`.

## Out of scope (follow-ups)

- Importing the `@defacto.de` mailbox (separate source) so defacto-era emails
  become searchable by date.
- Time-based deduction algorithms on top of the now-dated corpus.
