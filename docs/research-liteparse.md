---
type: research
status: current
related:
  - PLAN.md
  - docs/runbook.md
---

# A/B: internal/ocr.PDFFile vs liteparse — md quality + PDF→JSON→YAML (issue #221)

Research within epic [#219](https://git.produktor.io/eSlider/2dph/issues/219)
(structural document extraction). We compared the existing 2dph PDF pipeline
(baseline) with [liteparse](https://github.com/run-llama/liteparse) (candidate) on
three isolated samples from [#220](https://git.produktor.io/eSlider/2dph/issues/220):

- `invoice-text.pdf` — text-layer invoice: heading, table (3 rows), list.
- `invoice-scan.pdf` — scan-like: a full-page image (dark band) + one text run.
- `liteparse-sample.pdf` — a public one-page PDF (lorem ipsum + heading).

The production corpus `var/corpus` was not touched; everything is in `var/research/` (gitignored).

## How to reproduce

```bash
# tests (offline; the docker test skips when docker/image is absent)
go test -tags=research_ab ./bin/research/... -count=1

# run on samples (starts liteparse through docker)
./bin/research/ab.go
```

Tool: `bin/research/ab.go` (build tag `research_ab`). For each document it
runs: the `lit is-complex` verdict, the baseline (`internal/ocr.PDFFile`), `lit parse
--format markdown --extract-blocks --no-ocr` (+ the OCR variant, if the verdict asks for it),
and `lit parse --format json --extract-blocks` with conversion to YAML. The result is
`var/research/out/metrics.json` + artifacts `*.md`, `*.ocr.md`, `*.json`, `*.yaml`.

The liteparse launch command (the docker wrapper in the tool does the same thing):

```bash
P=/usr/local/lib/pdfium-rs/chromium_7897/pdfium-linux-x64/lib
docker run --rm -e PDFIUM_LIB_PATH=$P -e LD_LIBRARY_PATH=$P \
  -v "$PWD/var/research/samples:/s:ro" -v "$PWD/var/research/out:/o" \
  ghcr.io/run-llama/liteparse:latest lit parse /s/<f>.pdf \
  --format markdown --extract-blocks --no-ocr -o /o/<f>.md
```

The image `ghcr.io/run-llama/liteparse:latest` (lit 2.14.0). The pdfium path and the image are
constants of the tool, overridden by the flags `--pdfium-lib-path`/`--image`
or the env `LITEPARSE_PDFIUM_LIB_PATH`/`LITEPARSE_IMAGE`.

## Metrics (run 2026-08-29, docker 26.1.4)

| Document | verdict (`is-complex`) | baseline ms / bytes | lit md ms / bytes | lit md OCR ms / bytes | lit json ms / bytes | blocks (kind) | bboxes |
|---|---|---|---|---|---|---|---|
| invoice-text.pdf | needs-ocr (sparse-text) | 7 / 154 | 644 / 117 | 2519 / 117 | 2285 / 4860 | heading=1, paragraph=3 | 14 |
| invoice-scan.pdf | needs-ocr (scanned) | 23 / 11 | 699 / 11 | 2748 / 11 | 2552 / 665 | paragraph=1 | 1 |
| liteparse-sample.pdf | text (OCR not needed) | 12 / 2851 | 672 / 2852 | — | 669 / 46969 | heading=1, paragraph=2 | 147 |

Reading the numbers:

- **baseline** (pdftotext fast path) — 7–23 ms. `internal/ocr.PDFFile` never
  reaches OCR on any sample: on `invoice-scan.pdf` the text run `SCAN-9F31A1`
  makes the fast path succeed. Tesseract is not installed on the host — the
  gs+tesseract branch never fired in this run (this is honest pipeline behaviour).
- **liteparse md** — 644–699 ms, of which ~0.5–0.7 s is the docker container start;
  lit itself spends 1–25 ms (the `[liteparse] total` lines). For small
  documents the docker overhead dominates.
- **OCR variant** (2.5–2.7 s) fired for both invoices: on `invoice-text.pdf` the
  result is byte-for-byte the same as without OCR (117 bytes) — OCR was not needed,
  it was provoked by a false `sparse-text` verdict. On `invoice-scan.pdf` — only
  `SCAN-9F31A1` (a dark band without text — correctly empty).
- **liteparse json** — for text documents ~0.7 s, for the scan — 2.5 s (OCR inside).

## Markdown quality

| Check | invoice-text | liteparse-sample | baseline (pdftotext -layout) |
|---|---|---|---|
| Heading | `# INVOICE-7C2F9E` (level 1) | `# This is a simple PDF file.` (level 1) | a line of text |
| Table | **not recognized** — smeared into 3 paragraphs, columns/order lost | no tables | columns preserved (layout) |
| List / lines | `Client: Acme GmbH Item Widget A Widget B Total Note: net 14 days.` — all in one paragraph | — | lines separated |
| Reading order | broken: `Qty Price` and `2 10.00 1 25.00 45.00` moved out as separate paragraphs | fine; the line `Sample PDF` is moved out as a paragraph before the heading | preserved |

Conclusion on md: for text-layer documents with tables **liteparse markdown is worse
than the baseline** — the table is not detected (`kind: table` is absent from blocks) and
is smeared into paragraphs. Headings (level) liteparse determines correctly.

## PDF→JSON→YAML

`lit parse --format json --extract-blocks` produces a typed structure:

```yaml
pages:
  - page: 1
    width: 612
    height: 792
    text: |-
      INVOICE-7C2F9E
      Client: Acme GmbH
      Item    Qty  Price
      Widget A  2       10.00
      ...
    text_items:            # 14 items: geometry + font + confidence
      - text: INVOICE-7C2F9E
        x: 72
        y: 56.880005
        width: 129.808
        font_name: Helvetica
        font_size: 16
        confidence: 1
    blocks:                # semantics
      - kind: heading
        text: INVOICE-7C2F9E
        level: 1
        bbox: {x: 72, y: 56.88, width: 130, height: 19}
      - kind: paragraph
        text: "Client: Acme GmbH Item Widget A Widget B Total Note: net 14 days."
```

Converter: `bin/research/ab.go` → `JSONToYAML` (typed `LitJSON/LitPage/
LitItem/LitBlock`, `gopkg.in/yaml.v3`; yaml tags are explicit, otherwise yaml.v3 strips
underscores: `total_pages` → `totalpages`).

What the structure gives programmatically:

- **`pages[].text`** — flat text in reading order, columns preserved by position
  (unlike md).
- **`text_items[]`** — 14 positioned fragments with bbox and confidence.
  This is enough to restore the table: group by `y` → rows,
  sort by `x` → cells (for example, `Total` and `45.00` lie in the same row).
- **`blocks[]`** — only `heading`/`paragraph`; a table as `kind: table` is not
  recognized on any sample.
- **Invoice field extraction**: `INVOICE-7C2F9E` → the first block (`kind: heading`,
  `level: 1`); `Client: Acme GmbH` → the second line of `page.text` / `text_items[1]`;
  the total `45.00` → via the y-buckets of `text_items`. It is extracted programmatically, but via
  the geometry of `text_items`, not via the semantics of `blocks`.

## Conclusion

- **Replacing `internal/ocr.PDFFile` / `pdfHandler` with liteparse is not worth it.**
  For text-layer documents the baseline is 1–2 orders of magnitude faster (7–23 ms vs
  640+ ms) and preserves tables in the text better.
- **The value of liteparse is not md but structured JSON** (bbox, font,
  confidence, reading order). This is what pdftotext lacks, and what is useful for
  brain ingest (content addressing `#100` — node coordinates) and OO Documents.
- **Recommendation — a hybrid**: pdftotext fast path (text PDFs, <50 ms) +
  liteparse JSON only for complex/scan documents. Use the `lit is-complex` selection
  threshold with care: the verdict `sparse-text` on
  a small text-layer invoice is a false positive (an extra ~2.5 s of OCR on
  a document that does not need OCR).
- **Docker overhead** (~0.5–0.7 s per `docker run`) kills the gain on small
  documents; in production keep liteparse as a daemon/long-lived container,
  not starting a container per document.
- **Tables from text-layer PDFs** are not recognized as a structure by either method:
  pdftotext preserves columns as text, liteparse smears them into paragraphs.
  Real table recognition is post-processing over `text_items` (y/x buckets)
  or the OCR path for scans.

Bottom line: **not a replacement but an addition** — the hybrid "pdftotext fast path + liteparse
JSON for complex ones + `text_items` geometry for structural extraction".
The next step of epic #219 is a PoC of the hybrid handler with `text_items`-based table
reconstruction on a real corpus.

---

## Optimization: start-as-service + struct-data ETL (2026-08-30)

### The "daemon container" pattern confirmed by measurements

`docker run` per document pays 0.5–0.7s for the container lifecycle
(overlay mount + bridge network + `--rm`). The solution is **start-as-service**:
one long-lived container, invoked via `docker compose exec`.

```bash
docker compose up -d liteparse                 # warmup at start (entrypoint)
docker compose exec -T liteparse lit parse /v/<rel>.pdf --format json -o /data/work/<name>.json
```

The warmup in `scripts/liteparse-entrypoint.sh` runs `lit --version` + one parse
at startup — pdfium/tesseract land in the page cache, the first cold OCR does not
pay 2.5s.

**p95 (n=10, run 2026-08-30, docker 26.1.4):**

| Pattern | text md p95 | text json p95 | scan OCR p95 |
|---|---|---|---|
| `docker run` per document (the old A/B) | ~700 ms | ~700 ms | ~2.7 s |
| **daemon container `docker compose exec`** | **118 ms** | **113 ms** | **312 ms** |

Conclusion: the daemon pattern speeds up the text path ~**6x**, OCR ~**8.7x**. The 312ms OCR
is real tesseract work (not overhead); the 2.5s of cold OCR on the first
launch is removed by the warmup.

### struct-data ETL: document → YAML by hash

`bin/research/convert.go` — any document (pdf/docx/xlsx/pptx/odf/image) →
liteparse JSON (warm service) → YAML in `var/struct-data/<sha256>.yml`,
**content-addressed** (hash of the source). Idempotent: an existing non-empty
YAML → skip, a repeated run does not re-digitize.

```bash
./bin/research/convert.go <file-or-dir>...    # → var/struct-data/<hash>.yml
./bin/research/convert.go --ocr <scan.pdf>    # OCR path
./bin/research/convert.go --list              # list var/struct-data
```

YAML wrapper (meta + document):

```yaml
meta:
  hash: <sha256 of the source>
  source_path: var/research/samples/invoice-text.pdf
  extension: pdf
  format: pdf
  size: 922
  created_at: ...      # file mtime
  modified_at: ...
  digitized_at: ...    # digitization time
  engine: liteparse
  ocr: false
  blocks: true
  original_name: invoice-text.pdf
document:              # liteparse structured JSON→YAML
  total_pages: 1
  pages: [page, width, height, text, text_items[], blocks[]]
```

This is exactly what is needed for CRM/fact extraction: metadata (dates, path/URL,
extension, format, digitization time) + structure (`text_items` bbox, `blocks`).
The next stage (outside this research) is YAML association mapping into CRM /
fact-audit: reading `var/struct-data/*.yml` as a source of facts.

The liteparse types + the daemon-exec runner were moved into `internal/research` (single
implementation): used by `bin/research/ab.go`, `convert.go` and
`bench.go`. The measurement tool: `bin/research/bench.go` (warmup + p50/p95).
