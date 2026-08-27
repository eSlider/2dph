//go:build mail_convert_mbox

// usr/bin/env go run -tags=mail_convert_mbox "$0" "$@"; exit
//
// bin/mail/convert-mbox.go - split mbox mailboxes into one .eml per message.
//
//	./bin/mail/convert-mbox.go --in DIR --out var/corpus/mail/archive --dry-run
//
// Walks DIR for mbox files (skipping Thunderbird metadata and policy folders
// like Drafts/Templates/Trash/Junk/Spam/Unsent), splits each on mbox "From "
// separators, and writes each message to
// <out>/<source>/<rel-dir>/<sha256:16>/<sha256:16>.eml so mailconv.FromEML can
// ingest them. The splitter lives in internal/mailconv (mailconv.SplitMboxDir);
// this tool is a thin CLI wrapper over it.
// <source> is derived from a --source tag or the top-level dir under --in.
// Re-runs are idempotent: already-written messages are skipped (never duplicated).
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/eSlider/2dph/internal/mailconv"
)

func main() {
	var in, out, source string
	var dry bool
	flag.StringVar(&in, "in", "", "input root to scan for mbox files")
	flag.StringVar(&out, "out", "var/corpus/mail/archive", "output root")
	flag.StringVar(&source, "source", "", "force source label (else top-level dir name)")
	flag.BoolVar(&dry, "dry-run", false, "count only, no writes")
	flag.Parse()
	if in == "" {
		fmt.Fprintln(os.Stderr, "convert-mbox: --in DIR required")
		os.Exit(2)
	}
	written, skipped, err := mailconv.SplitMboxDir(in, out, source, dry)
	if err != nil {
		fmt.Fprintf(os.Stderr, "convert-mbox: %v\n", err)
		os.Exit(1)
	}
	mode := "written"
	if dry {
		mode = "dry-run"
	}
	fmt.Printf("convert-mbox: %d messages (%s), %d skipped (already present)\n", written, mode, skipped)
}
