//usr/bin/env go run -tags=research_convert "$0" "$@"; exit
//go:build research_convert
//
// bin/research/convert.go — struct-data ETL (epic #219): any document
// (pdf/docx/xlsx/pptx/odf/image, via the warm liteparse service) → structured
// YAML under var/struct-data/<sha256>.yml, content-addressed by the source
// file hash. Idempotent: an existing non-empty YAML is skipped, so re-runs
// never re-digitize.
//
//	./bin/research/convert.go <file-or-dir>...        # convert docs
//	./bin/research/convert.go --ocr <scan.pdf>        # force OCR path
//	./bin/research/convert.go --blocks <doc.docx>     # + semantic blocks/bbox
//	./bin/research/convert.go --list                  # show var/struct-data
//	./bin/research/convert.go --json <file>           # print JSON, no write
//
// Requires the warm service: `docker compose up -d liteparse`.
// The YAML envelope: meta (hash, source path/URL, extension, format, sizes,
// created/modified/digitized, engine) + document (liteparse structured JSON).
// NOTE: never run gofmt -w on this file — it breaks the shebang.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/eSlider/2dph/internal/research"
)

func main() {
	fs := flag.NewFlagSet("convert", flag.ExitOnError)
	ocr := fs.Bool("ocr", false, "force OCR path (scans)")
	blocks := fs.Bool("blocks", true, "extract semantic blocks + bboxes (--extract-blocks)")
	list := fs.Bool("list", false, "list existing var/struct-data entries")
	jsonOut := fs.Bool("json", false, "print structured JSON to stdout, do not write YAML")
	service := fs.String("service", "liteparse", "compose service name")
	timeout := fs.Duration("timeout", 3*time.Minute, "per-doc liteparse timeout")
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: convert.go [flags] <file-or-dir>...\n")
		fs.PrintDefaults()
	}
	_ = fs.Parse(os.Args[1:])

	r := research.NewRunner(*service, *timeout)

	if *list {
		listStructData()
		return
	}
	args := fs.Args()
	if len(args) == 0 {
		fs.Usage()
		os.Exit(2)
	}

	ctx := context.Background()
	var docs []research.DocSource
	for _, a := range args {
		st, err := os.Stat(a)
		if err != nil {
			fmt.Fprintf(os.Stderr, "convert: %v\n", err)
			continue
		}
		if st.IsDir() {
			fs, _ := filepath.Glob(filepath.Join(a, "*"))
			sort.Strings(fs)
			for _, f := range fs {
				if !isDir(f) {
					docs = append(docs, research.DocSource{Path: f})
				}
			}
			continue
		}
		docs = append(docs, research.DocSource{Path: a})
	}
	if len(docs) == 0 {
		fmt.Fprintln(os.Stderr, "convert: no input documents")
		os.Exit(1)
	}

	opts := research.ConvertOpts{OCR: *ocr, Blocks: *blocks}
	for _, d := range docs {
		if *jsonOut {
			printJSON(ctx, r, d, opts)
			continue
		}
		target, written, err := r.Structify(ctx, d, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "convert: %s: %v\n", d.Path, err)
			continue
		}
		state := "written"
		if !written {
			state = "skipped (exists)"
		}
		fmt.Printf("%-9s %s\n", state, target)
	}
}

func printJSON(ctx context.Context, r *research.Runner, d research.DocSource, o research.ConvertOpts) {
	hash, err := research.FileHash(d.Path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "convert: %v\n", err)
		return
	}
	rel, _ := filepath.Rel(filepath.Dir(research.StructDir), d.Path)
	in := "/v/" + filepath.ToSlash(rel)
	l, err := r.ParseToJSON(ctx, in, research.ParseOpts{Format: "json", OCR: o.OCR, Blocks: o.Blocks})
	if err != nil {
		fmt.Fprintf(os.Stderr, "convert: %s: %v\n", d.Path, err)
		return
	}
	fmt.Printf("### %s\nhash: %s\n", d.Path, hash)
	body, _ := l.MarshalYAML()
	fmt.Print(string(body))
}

func listStructData() {
	entries, _ := filepath.Glob(filepath.Join(research.StructDir, "*.yml"))
	sort.Strings(entries)
	if len(entries) == 0 {
		fmt.Println("var/struct-data: empty")
		return
	}
	for _, e := range entries {
		st, _ := os.Stat(e)
		fmt.Printf("%s  %8d  %s\n", time.Now().Sub(st.ModTime()).Round(time.Second), st.Size(), filepath.Base(e))
	}
	fmt.Printf("%d file(s) in %s\n", len(entries), research.StructDir)
}

func isDir(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.IsDir()
}