//usr/bin/env go run -tags=rules_book "$0" "$@"; exit
//go:build rules_book
//
// bin/rules/book.go — download + extract + split the rule source book
// (epic #324, P0). Produces the gitignored working artifacts under var/:
//
//	var/rules/6-042/6-042.pdf   downloaded source (CC BY-NC-SA, NOT committed)
//	var/rules/6-042/6-042.txt   pdftotext -layout full text (NOT committed)
//	var/rules/6-042/chapters/   one .txt per chapter (NOT committed)
//
//	./bin/rules/book.go                    # download (if missing) + pdftotext + split
//	./bin/rules/book.go --split-only       # reuse an existing text, just split
//	./bin/rules/book.go --force            # re-download even if the PDF exists
//	./bin/rules/book.go --dry-run          # print the plan, write nothing
//
// The committed catalog (etc/rules/6-042/*.yml) is our own compressed rules —
// the licensed book text never enters the repository or the GitHub mirror.
// NOTE: never run `gofmt -w` on this file — it breaks the shebang.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/rules"
	"github.com/eSlider/2dph/pkg/utils"
)

const (
	defaultURL = "https://ocw.mit.edu/courses/6-042j-mathematics-for-computer-science-spring-2015/mit6_042js15_textbook.pdf"
	relBase    = "var/rules/6-042"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	fs := flag.NewFlagSet("rules-book", flag.ContinueOnError)
	url := fs.String("url", defaultURL, "source PDF URL")
	pdf := fs.String("pdf", "", "PDF path (default <root>/"+relBase+"/6-042.pdf)")
	text := fs.String("text", "", "layout text path (default <root>/"+relBase+"/6-042.txt)")
	out := fs.String("out", "", "chapter output dir (default <root>/"+relBase+"/chapters)")
	force := fs.Bool("force", false, "re-download the PDF even if it exists")
	splitOnly := fs.Bool("split-only", false, "skip download/pdftotext, split the existing text")
	dryRun := fs.Bool("dry-run", false, "print the plan and summary, write nothing")
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "usage: book.go [--url URL] [--pdf p] [--text p] [--out dir] [--force] [--split-only] [--dry-run]\n")
	}
	if err := fs.Parse(args); err != nil {
		return 2
	}

	root := utils.Root()
	pdfPath := orDefault(*pdf, filepath.Join(root, relBase, "6-042.pdf"))
	textPath := orDefault(*text, filepath.Join(root, relBase, "6-042.txt"))
	outDir := orDefault(*out, filepath.Join(root, relBase, "chapters"))

	if !*splitOnly {
		if *dryRun {
			fmt.Printf("plan: download %s -> %s\n", *url, pdfPath)
			fmt.Printf("plan: pdftotext -layout %s %s\n", pdfPath, textPath)
		} else {
			if err := os.MkdirAll(filepath.Dir(pdfPath), 0o755); err != nil {
				fmt.Fprintf(os.Stderr, "book: %v\n", err)
				return 1
			}
			if *force || !exists(pdfPath) {
				if err := download(*url, pdfPath); err != nil {
					fmt.Fprintf(os.Stderr, "book: download: %v\n", err)
					return 1
				}
			}
			if err := pdftotext(pdfPath, textPath); err != nil {
				fmt.Fprintf(os.Stderr, "book: %v\n", err)
				return 1
			}
		}
	}

	raw, err := os.ReadFile(textPath)
	if err != nil {
		if *dryRun {
			return 0 // nothing extracted yet — plan only
		}
		fmt.Fprintf(os.Stderr, "book: read %s: %v\n", textPath, err)
		return 1
	}

	chs := rules.SplitChapters(string(raw))
	fmt.Printf("split: %d chapters from %s\n", len(chs), textPath)
	if *dryRun {
		for _, ch := range chs {
			fmt.Printf("  %2d  %-45s lines %d..%d\n", ch.Number, ch.Title, ch.Start, ch.End)
		}
		return 0
	}
	if len(chs) == 0 {
		fmt.Fprintln(os.Stderr, "book: no chapters detected — check the PDF/text format")
		return 1
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		fmt.Fprintf(os.Stderr, "book: %v\n", err)
		return 1
	}
	for _, ch := range chs {
		name := fmt.Sprintf("%02d-%s.txt", ch.Number, slug(ch.Title))
		if err := os.WriteFile(filepath.Join(outDir, name), []byte(ch.Body), 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "book: write %s: %v\n", name, err)
			return 1
		}
		fmt.Printf("  %2d  %-45s -> %s (%d lines)\n", ch.Number, ch.Title, name, ch.End-ch.Start+1)
	}
	pdfSum, _ := sha256File(pdfPath)
	fmt.Printf("done: %d chapters -> %s\n", len(chs), outDir)
	if pdfSum != "" {
		fmt.Printf("pdf sha256: %s\n", pdfSum)
	}
	return 0
}

func orDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// download streams url to dest atomically (tmp file + rename), so an aborted
// run never leaves a truncated PDF in place.
func download(url, dest string) error {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	tmp := dest + ".part"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	if _, err := io.Copy(f, resp.Body); err != nil {
		f.Close()
		os.Remove(tmp)
		return err
	}
	if err := f.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, dest)
}

// pdftotext shells out to poppler's pdftotext (-layout preserves the book's
// two-column reading order, which the chapter splitter relies on).
func pdftotext(pdf, text string) error {
	bin, err := exec.LookPath("pdftotext")
	if err != nil {
		return fmt.Errorf("pdftotext not found in PATH (install poppler-utils)")
	}
	out, err := exec.Command(bin, "-layout", pdf, text).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pdftotext: %v (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

var slugRe = regexp.MustCompile(`[^a-z0-9]+`)

func slug(s string) string {
	s = strings.ToLower(s)
	s = slugRe.ReplaceAllString(s, "-")
	return strings.Trim(s, "-")
}

func sha256File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
