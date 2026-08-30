// struct-data ETL (epic #219): document → structured JSON (liteparse warm
// service) → YAML with metadata, content-addressed by sha256 of the source
// file. Written to var/struct-data/<hash>.yml. Idempotent: if the YAML exists
// and is non-empty, conversion is skipped (no re-digitization).
package research

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// StructDir is the root for content-addressed struct-data YAML files
// (gitignored, under var/). Relative to the repo root.
const StructDir = "var/struct-data"

// Meta carries document provenance next to the parsed structure (the piece a
// fact-audit / CRM-association step will consume later — not built yet).
type Meta struct {
	Hash         string `json:"hash" yaml:"hash"`
	SourcePath   string `json:"source_path" yaml:"source_path"`
	SourceURL    string `json:"source_url,omitempty" yaml:"source_url,omitempty"`
	Extension    string `json:"extension" yaml:"extension"`
	Format       string `json:"format" yaml:"format"`
	Size         int64  `json:"size" yaml:"size"`
	CreatedAt    string `json:"created_at" yaml:"created_at"`
	ModifiedAt   string `json:"modified_at" yaml:"modified_at"`
	DigitizedAt  string `json:"digitized_at" yaml:"digitized_at"`
	Engine       string `json:"engine" yaml:"engine"`
	OCR          bool   `json:"ocr" yaml:"ocr"`
	Blocks       bool   `json:"blocks" yaml:"blocks"`
	OriginalName string `json:"original_name,omitempty" yaml:"original_name,omitempty"`
}

// StructData is the YAML document stored at var/struct-data/<hash>.yml:
// the metadata envelope + the parsed structure.
type StructData struct {
	Meta     Meta     `json:"meta" yaml:"meta"`
	Document LitJSON  `json:"document" yaml:"document"`
	RawText  string   `json:"raw_text,omitempty" yaml:"raw_text,omitempty"`
	Blocks   []LitBlock `json:"blocks,omitempty" yaml:"blocks,omitempty"`
}

// DocSource is one input document to digitize.
type DocSource struct {
	Path string
	URL  string // optional original URL (OO Documents link etc.)
}

// ConvertOpts tune the digitization pass.
type ConvertOpts struct {
	OCR    bool // force OCR even if is-complex says no
	Blocks bool // --extract-blocks (semantic blocks + bboxes)
}

// StructPath returns the content-addressed path for a source hash.
func StructPath(hash string) string {
	return filepath.Join(StructDir, hash+".yml")
}

// Exists reports whether the struct-data YAML already exists and is non-empty
// (idempotency gate: a present non-empty file means no re-conversion needed).
func Exists(hash string) bool {
	st, err := os.Stat(StructPath(hash))
	return err == nil && st.Size() > 0
}

// Structify converts one document via the warm liteparse service and writes
// var/struct-data/<sha256>.yml. Returns the target path and whether it was
// newly written (skipped=false when already present).
func (r *Runner) Structify(ctx context.Context, src DocSource, o ConvertOpts) (string, bool, error) {
	if !DockerOK() {
		return "", false, errors.New("docker not on PATH (start `docker compose up -d liteparse`)")
	}
	hash, err := FileHash(src.Path)
	if err != nil {
		return "", false, fmt.Errorf("hash %s: %w", src.Path, err)
	}
	target := StructPath(hash)
	if Exists(hash) {
		return target, false, nil
	}
	if err := os.MkdirAll(StructDir, 0o755); err != nil {
		return "", false, fmt.Errorf("mkdir %s: %w", StructDir, err)
	}

	// Parse to JSON via the warm service; container sees host var/ at /v.
	rel, err := filepath.Rel(filepath.Dir(StructDir), src.Path)
	if err != nil {
		rel = src.Path
	}
	in := "/v/" + filepath.ToSlash(rel)
	po := ParseOpts{Format: "json", OCR: o.OCR, Blocks: o.Blocks}
	l, err := r.ParseToJSON(ctx, in, po)
	if err != nil {
		return "", false, err
	}

	meta := Meta{
		Hash:         hash,
		SourcePath:   src.Path,
		SourceURL:    src.URL,
		Extension:    strings.TrimPrefix(filepath.Ext(src.Path), "."),
		Format:       extToFormat(filepath.Ext(src.Path)),
		Size:         fileSize(src.Path),
		CreatedAt:    modTimeRFC(src.Path),
		ModifiedAt:   modTimeRFC(src.Path),
		DigitizedAt:  time.Now().UTC().Format(time.RFC3339),
		Engine:       "liteparse",
		OCR:          o.OCR,
		Blocks:       o.Blocks,
		OriginalName: filepath.Base(src.Path),
	}

	sd := StructData{Meta: meta, Document: *l}
	if o.Blocks {
		for _, p := range l.Pages {
			sd.Blocks = append(sd.Blocks, p.Blocks...)
		}
		for _, p := range l.Pages {
			sd.RawText += p.Text
		}
	}

	body, err := yamlMarshalSD(&sd)
	if err != nil {
		return "", false, fmt.Errorf("yaml marshal %s: %w", target, err)
	}
	if err := os.WriteFile(target, body, 0o644); err != nil {
		return "", false, fmt.Errorf("write %s: %w", target, err)
	}
	return target, true, nil
}

// extToFormat maps a file extension to a human format label.
func extToFormat(ext string) string {
	switch strings.ToLower(ext) {
	case ".pdf":
		return "pdf"
	case ".docx", ".docm":
		return "docx"
	case ".doc":
		return "doc"
	case ".xlsx", ".xlsm":
		return "xlsx"
	case ".xls":
		return "xls"
	case ".pptx", ".pptm":
		return "pptx"
	case ".ppt":
		return "ppt"
	case ".odt":
		return "odt"
	case ".ods":
		return "ods"
	case ".odp":
		return "odp"
	case ".rtf":
		return "rtf"
	case ".txt", ".md":
		return "text"
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".tiff", ".webp":
		return "image"
	case ".csv", ".tsv":
		return "csv"
	default:
		return strings.TrimPrefix(ext, ".")
	}
}

func fileSize(path string) int64 {
	st, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return st.Size()
}

func modTimeRFC(path string) string {
	st, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return st.ModTime().UTC().Format(time.RFC3339)
}

func yamlMarshalSD(sd *StructData) ([]byte, error) {
	return yaml.Marshal(sd)
}