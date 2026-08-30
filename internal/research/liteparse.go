// Package research holds the liteparse integration primitives shared by the
// A/B harness (bin/research/ab.go) and the struct-data ETL tool
// (bin/research/convert.go). Single implementation (AGENTS #10): the liteparse
// JSON envelope types + the warm daemon exec runner live here, not per tool.
//
// liteparse runs as a long-lived compose service (epic #219). Per-document
// calls go through `docker compose exec -T liteparse lit parse ...` on the warm
// container (~40–120ms) instead of `docker run` per document (~0.7s overhead).
package research

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// LitJSON mirrors the liteparse `lit parse --format json` envelope.
type LitJSON struct {
	TotalPages int       `json:"total_pages" yaml:"total_pages"`
	Pages      []LitPage `json:"pages" yaml:"pages"`
}

// LitPage is one page: full-text dump, positional text_items and, with
// --extract-blocks, semantic blocks.
type LitPage struct {
	Page      int        `json:"page" yaml:"page"`
	Width     float64    `json:"width" yaml:"width"`
	Height    float64    `json:"height" yaml:"height"`
	Text      string     `json:"text" yaml:"text"`
	TextItems []LitItem  `json:"text_items" yaml:"text_items"`
	Blocks    []LitBlock `json:"blocks" yaml:"blocks"`
}

// LitItem is a positioned text fragment (bbox = x/y/width/height + font).
type LitItem struct {
	Text       string  `json:"text" yaml:"text"`
	X          float64 `json:"x" yaml:"x"`
	Y          float64 `json:"y" yaml:"y"`
	Width      float64 `json:"width" yaml:"width"`
	Height     float64 `json:"height" yaml:"height"`
	FontName   string  `json:"font_name" yaml:"font_name"`
	FontSize   float64 `json:"font_size" yaml:"font_size"`
	Confidence float64 `json:"confidence" yaml:"confidence"`
}

// LitBBox is the positional box of a block.
type LitBBox struct {
	X      float64 `json:"x" yaml:"x"`
	Y      float64 `json:"y" yaml:"y"`
	Width  float64 `json:"width" yaml:"width"`
	Height float64 `json:"height" yaml:"height"`
}

// LitBlock is one semantic block: heading/paragraph/table/list/...
type LitBlock struct {
	Kind  string   `json:"kind" yaml:"kind"`
	Text  string   `json:"text" yaml:"text"`
	Level *int     `json:"level,omitempty" yaml:"level,omitempty"`
	BBox  *LitBBox `json:"bbox,omitempty" yaml:"bbox,omitempty"`
}

// ParseOpts control a single liteparse run. OCR defaults off; EnableOCR turns
// tesseract on (scans). Blocks/TextItems add --extract-blocks / the item list.
type ParseOpts struct {
	Format   string // json | markdown | text
	OCR      bool
	Blocks   bool
	Password string
}

// litBase are the args that never vary for `lit parse`.
func litParseArgs(o ParseOpts, in, out string) []string {
	args := []string{"parse", in}
	switch o.Format {
	case "markdown":
		args = append(args, "--format", "markdown")
	case "text":
		args = append(args, "--format", "text")
	default:
		args = append(args, "--format", "json")
	}
	if !o.OCR {
		args = append(args, "--no-ocr")
	}
	if o.Blocks {
		args = append(args, "--extract-blocks")
	}
	if o.Password != "" {
		args = append(args, "--password", o.Password)
	}
	if out != "" {
		args = append(args, "-o", out)
	}
	return args
}

// Runner runs `lit` inside the warm compose service (docker compose exec).
type Runner struct {
	Service string // compose service name, default "liteparse"
	Timeout time.Duration
}

// NewRunner returns a Runner bound to the liteparse compose service.
func NewRunner(service string, timeout time.Duration) *Runner {
	if service == "" {
		service = "liteparse"
	}
	if timeout <= 0 {
		timeout = 3 * time.Minute
	}
	return &Runner{Service: service, Timeout: timeout}
}

// DockerOK reports whether docker compose is usable at all.
func DockerOK() bool {
	_, err := exec.LookPath("docker")
	return err == nil
}

// Parse runs liteparse inside the warm service for one document.
// inPath/outPath are container-side paths (host var/ mounts at /v).
func (r *Runner) Parse(ctx context.Context, inPath, outPath string, o ParseOpts) ([]byte, error) {
	args := append([]string{"compose", "exec", "-T", r.Service, "lit"}, litParseArgs(o, inPath, outPath)...)
	cctx, cancel := context.WithTimeout(ctx, r.Timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, "docker", args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := err.Error()
		if len(stderr.Bytes()) > 0 {
			msg = strings.TrimSpace(stderr.String())
		}
		return nil, fmt.Errorf("lit %s: %w (%s)", inPath, err, msg)
	}
	return out, nil
}

// ParseToJSON parses one document and decodes the structured envelope.
func (r *Runner) ParseToJSON(ctx context.Context, inPath string, o ParseOpts) (*LitJSON, error) {
	raw, err := r.Parse(ctx, inPath, "", o)
	if err != nil {
		return nil, err
	}
	var l LitJSON
	if err := json.Unmarshal(raw, &l); err != nil {
		return nil, fmt.Errorf("liteparse json: %w", err)
	}
	return &l, nil
}

// MarshalYAML re-encodes a LitJSON as YAML (explicit tags: yaml.v3 strips _).
func (l *LitJSON) MarshalYAML() ([]byte, error) {
	return jsonToYAML(l)
}

// JSONToYAML decodes liteparse JSON and re-encodes it as YAML.
func JSONToYAML(src []byte) ([]byte, error) {
	var l LitJSON
	if err := json.Unmarshal(src, &l); err != nil {
		return nil, fmt.Errorf("liteparse json: %w", err)
	}
	return jsonToYAML(&l)
}

// BlockKinds counts blocks per kind across all pages.
func (l *LitJSON) BlockKinds() map[string]int {
	m := map[string]int{}
	for _, p := range l.Pages {
		for _, b := range p.Blocks {
			m[b.Kind]++
		}
	}
	return m
}

// BBoxCount counts positioned text_items (structural evidence: geometry).
func (l *LitJSON) BBoxCount() int {
	n := 0
	for _, p := range l.Pages {
		n += len(p.TextItems)
	}
	return n
}

// RawText concatenates per-page full text in reading order.
func (l *LitJSON) RawText() string {
	var b strings.Builder
	for _, p := range l.Pages {
		b.WriteString(p.Text)
	}
	return b.String()
}

func jsonToYAML(l *LitJSON) ([]byte, error) {
	// yaml.v3 with explicit struct tags; the LitJSON tags carry "yaml" names
	// so underscores survive (plain yaml.Marshal of map would strip them).
	return yaml.Marshal(l)
}

// FileHash returns the lowercase hex sha256 of a file (content addressing,
// #100) — the struct-data key.
func FileHash(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", h.Sum(nil)), nil
}