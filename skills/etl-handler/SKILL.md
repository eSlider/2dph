# Skill: etl-handler — canonical Go ETL extraction pattern

Use when implementing file-type handlers, extractors, decoders or transforms
for the corpus pipeline. Mandatory before writing such code (AGENTS.md, code
standards #8–#11).

Core idea: any format may nest any other format (.eml → zip → .eml → pdf).
One handler per type; a central registry recurses; results are typed trees of
URL-addressable nodes.

## Contracts

```go
// Blob is an addressable unit of content flowing through the pipeline.
type Blob struct {
	SHA256 string        // integrity + idempotency key
	Name   string        // sanitized filename
	MIME   string        // declared × sniffed (mismatch wins for safety)
	Size   int64
	Open   func(ctx context.Context) (io.ReadCloser, error) // lazy
}

type Attachment = Blob // child inside a Result

type Result struct {
	MIME     string      // resolved type of this node
	Meta     Meta        // typed per-handler metadata (struct, not map)
	Text     []byte      // extracted text/markdown when applicable
	Children []Attachment // nested parts — recurse via Registry
}

type Handler interface {
	Match(declaredMIME string, sniff []byte) bool
	Handle(ctx context.Context, src Blob) (*Result, error) // sync, ctx-first
}
```

- Node identity: URL `scheme://platform/thread/msg/path-segments[#anchor]`
  (e.g. `mail://gmail/T42/M17/body/p[3]/table[0]#r2,c5`); node ID =
  `sha256(URL)[:16]`; separate `sha256(body)` for integrity.
- Parts reference each other only via URLs; href/src become `LINKS_TO` edges.
- Content splits granularly BEFORE insertion: html/md/pdf yield mime-typed
  blocks (paragraph / table / image / link / page).

## Walker safety (hard limits)

- Max depth 10; per-item size cap; total-size and file-count caps;
- zip-ratio guard: uncompressed/compressed > N → refuse;
- reject names with `..`, absolute paths, null bytes, control chars,
  length > 255; collisions get `-<hash8>` suffix;
- large payloads spool to `var/tmp` (cleaned after processing); `Body []byte`
  only under the small-size threshold (~1 MB);
- per-child errors are collected — never fatal to the whole tree.

## Format rules

- EML/MIME: `github.com/emersion/go-message` only — `mail.CreateReader`
  envelope, `entity.Walk()` recursion, streaming bodies, charset via
  `x/text`. enmime is legacy parity until cutover [#95].
- ZIP: standard `archive/zip`, iterate members as child Blobs.
- HTML: keep raw bytes by default; processors may opt into
  `golang.org/x/net/html` (link/image extraction).
- PDF: preserve original in corpus; Ghostscript normalization (strip export
  protection → shrink) produces working artifact in `var/tmp`; then
  pdftotext fast path, tesseract fallback [#102].
- Detection: `http.DetectContentType` plus magic-byte checks.

## Conversation canon (mail AND chats)

Handlers for interpersonal streams emit one canonical model:

```go
type Message struct {
	ID          string
	ThreadID    string
	Platform    string // email | telegram | whatsapp | linkedin | ...
	From        Participant
	ReplyTo     *string
	To          []Participant
	CC          []Participant
	BCC         []Participant
	SentAt      time.Time
	Body        BodyPart
	Attachments []Attachment // lazy children → Registry
}
```

Stored on disk (`var/corpus/{mail,chats}` JSON + sha256 manifest) BEFORE any
brain write; upsert-on-conflict idempotency; soft-delete timestamps keep
edited-message history (`valid_to`). The brain ingests only this canon.

## Orchestration: sync wave + OO CRM flows (#81)

Handlers and reconcilers are orchestrated as deterministic, idempotent waves
(`bin/stack/sync.go`) and standalone OnlyOffice tools:

```bash
go run ./bin/stack/sync.go --dry-run                       # print the wave
go run -tags=onlyoffice_reconcile_contact ./bin/onlyoffice/reconcile-contact.go
go run -tags=onlyoffice_import_interaction ./bin/onlyoffice/import-interaction.go --write
```

Corpus root for mail reconcilers defaults to `var/corpus/mail` (overridable
with `--sources`). OnlyOffice tools need `ONLYOFFICE_URL`/`USER`/`PASS` env.
See AGENTS.md `Tools` for the full command set.

## Checklist before PR

- Single implementation — no duplicate parser anywhere else (delete copies).
- TDD against fixture tree `.eml > .zip > nested.eml > .pdf`; offline only.
- `go test -race ./...`; cancel mid-walk test asserts workers exit.
- `-benchmem` before/after attached for bulk handlers.
- No hardcoded paths/URLs; config via go-config stack.
