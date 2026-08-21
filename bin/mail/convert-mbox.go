//go:build mail_convert_mbox

// usr/bin/env go run -tags=mail_convert_mbox "$0" "$@"; exit
//
// bin/mail/convert-mbox.go - split mbox mailboxes into one .eml per message.
//
//	./bin/mail/convert-mbox.go --in DIR --out var/mail/archive --dry-run
//
// Walks DIR for mbox files (skipping Thunderbird metadata like *.msf/.dat/.js),
// splits each on mbox "From " separators, and writes each message to
// <out>/<source>/<folder>/<NNN>/<NNN>.eml so mailconv.FromEML can ingest them.
// <source> is derived from a --source tag or the top-level dir under --in.
//
package main

import (
	"bufio"
	"bytes"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// reSkipName matches Thunderbird folders we must not import: drafts,
// templates, trash, spam/junk, unsent queue (#79 exclusion policy).
var reSkipName = regexp.MustCompile(`(?i)^(drafts?|templates?|trash|junk|spam(assassin)?|unsent( messages)?)(\.sbd)?$`)

// reEnvelope matches the sender + ctime-date tail of a real mbox separator.
var reEnvelope = regexp.MustCompile(`^\S+ [A-Z][a-z]{2} [A-Z][a-z]{2} `)

// isSep reports whether a line is a real mbox separator. Gmail/Thunderbird
// mbox uses a bare "From " (empty tail) or "From sender ctime-date"; lines like
// "From FCC: ..." (a Thunderbird fcc marker) and "From: ..." headers must not
// split.
func isSep(line []byte) bool {
	s := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
	if !strings.HasPrefix(s, "From ") {
		return false
	}
	rest := s[len("From "):]
	return rest == "" || reEnvelope.MatchString(rest)
}

var msgSeq int64

func main() {
	var in, out, source string
	var dry bool
	flag.StringVar(&in, "in", "", "input root to scan for mbox files")
	flag.StringVar(&out, "out", "var/mail/archive", "output root")
	flag.StringVar(&source, "source", "", "force source label (else top-level dir name)")
	flag.BoolVar(&dry, "dry-run", false, "count only, no writes")
	flag.Parse()
	if in == "" {
		fmt.Fprintln(os.Stderr, "convert-mbox: --in DIR required")
		os.Exit(2)
	}
	total := 0
	err := filepath.Walk(in, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		ext := strings.ToLower(filepath.Ext(p))
		if ext == ".msf" || ext == ".dat" || ext == ".js" || ext == ".json" || ext == ".sqlite" || ext == ".sqlite-wal" {
			return nil
		}
		if !looksMbox(p) {
			return nil
		}
		rel, _ := filepath.Rel(in, p)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		for _, part := range parts {
			if reSkipName.MatchString(part) {
				return nil
			}
		}
		src := source
		if src == "" && len(parts) > 0 {
			src = parts[0]
		}
		dir := filepath.Dir(rel)
		n, err := splitMbox(p, out, src, dir, dry)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  [err] %s: %v\n", p, err)
			return nil
		}
		total += n
		fmt.Printf("  [%s] %d msgs  %s\n", map[bool]string{true: "dry", false: "ok "}[dry], n, rel)
		return nil
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("convert-mbox: %d messages (%s)\n", total, map[bool]string{true: "dry-run", false: "written"}[dry])
	return
}

// looksMbox reports whether the file starts with the mbox separator format.
func looksMbox(p string) bool {
	f, err := os.Open(p)
	if err != nil {
		return false
	}
	defer f.Close()
	r := bufio.NewReader(io.LimitReader(f, 8<<20))
	// Scan the first few lines for the "From " separator. Use ReadBytes so the
	// trailing \r is kept (ReadLine strips it, breaking bare "From " separators).
	for i := 0; i < 6; i++ {
		line, err := r.ReadBytes('\n')
		if err != nil && len(line) == 0 {
			break
		}
		if isSep(line) {
			return true
		}
	}
	return false
}

func splitMbox(p, out, src, dir string, dry bool) (int, error) {
	f, err := os.Open(p)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	// Preserve the mbox's directory tree as nested dirs (sanitize each segment,
	// not the flattened path) so distinct folders never collide.
	base := []string{out, sanitize(src)}
	for _, seg := range strings.Split(filepath.ToSlash(dir), "/") {
		if seg != "" && seg != "." {
			base = append(base, sanitize(seg))
		}
	}
	baseDir := filepath.Join(base...)
	br := bufio.NewReaderSize(f, 1<<20)
	var (
		cur   bytes.Buffer
		start bool
		count int
	)
	flush := func() error {
		if cur.Len() == 0 {
			return nil
		}
		count++
		if dry {
			cur.Reset()
			return nil
		}
		msgSeq++
		id := fmt.Sprintf("%07d", msgSeq)
		md := filepath.Join(baseDir, id)
		if err := os.MkdirAll(md, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(md, id+".eml"), cur.Bytes(), 0o644); err != nil {
			return err
		}
		cur.Reset()
		return nil
	}
	for {
		line, err := br.ReadBytes('\n')
		if len(line) > 0 {
			if isSep(line) {
				// A separator begins a new message; it is a delimiter, not part
				// of the RFC 822 body, so flush the previous message and skip it.
				if start {
					if err := flush(); err != nil {
						return count, err
					}
				}
				start = true
				if err == io.EOF {
					break
				}
				continue
			}
			if start {
				cur.Write(line)
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return count, err
		}
	}
	if err := flush(); err != nil {
		return count, err
	}
	return count, nil
}

func sanitize(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_",
		"<", "_", ">", "_", "|", "_", " ", "_", "[", "", "]", "", ".", "_")
	return r.Replace(s)
}

var _ = sort.Strings
