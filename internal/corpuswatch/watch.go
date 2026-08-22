// Package watch polls corpus directories for changes and re-runs brain/index.
//
// Port of the former bin/kb-watch bash script to an importable, testable Go
// package. Polls file mtimes (no inotify deps); cheap and reliable.
package corpuswatch

import (
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Options controls the polling loop. Zero value uses defaults. Values come
// from the typed config (internal/config), not environment reads.
type Options struct {
	Dirs     []string
	Interval time.Duration
	// Root is the repo root used to resolve the IndexCmd template (<root>).
	Root string
	// IndexCmd is the index command template; <root> is replaced by Root.
	// Defaults to `<root>/bin/brain/index.go --with-mail`.
	IndexCmd string
}

// normalize applies defaults: args override Dirs, empty Dirs defaults to
// /corpus, a zero Interval to 30s, empty Root to the working dir, and an empty
// IndexCmd to the standard brain/index command.
func normalize(opts Options, args []string) Options {
	if opts.Interval <= 0 {
		opts.Interval = 30 * time.Second
	}
	if len(args) > 0 {
		opts.Dirs = args
	}
	if len(opts.Dirs) == 0 {
		opts.Dirs = []string{"/corpus"}
	}
	if opts.Root == "" {
		opts.Root, _ = os.Getwd()
	}
	if opts.IndexCmd == "" {
		opts.IndexCmd = "<root>/bin/brain/index.go --with-mail"
	}
	return opts
}

// Run blocks forever polling Dirs every Interval and re-indexing when files
// change.
func Run(args []string, opts Options) {
	opts = normalize(opts, args)
	log.Printf("watch: dirs=%v interval=%s root=%s", opts.Dirs, opts.Interval, opts.Root)
	var last string
	for {
		if flag := Stamp(opts.Dirs); flag != "" && flag != last {
			last = flag
			reindex(opts.IndexCmd, opts.Root)
		}
		time.Sleep(opts.Interval)
	}
}

// Stamp returns a rolling fingerprint (newest mtime under dirs) that changes
// whenever any corpus file is touched. Empty when no files found.
func Stamp(dirs []string) string {
	var newest time.Time
	for _, dir := range dirs {
		_ = filepath.WalkDir(dir, func(path string, _ os.DirEntry, err error) error {
			if err != nil {
				return nil
			}
			if info, e := os.Stat(path); e == nil && info.ModTime().After(newest) {
				newest = info.ModTime()
			}
			return nil
		})
	}
	if newest.IsZero() {
		return ""
	}
	return strconv.FormatInt(newest.UnixNano(), 10)
}

func reindex(template, root string) {
	cmd := strings.ReplaceAll(template, "<root>", root)
	parts := strings.Fields(cmd)
	c := exec.Command(parts[0], parts[1:]...)
	out, err := c.CombinedOutput()
	if err != nil {
		log.Printf("watch: index failed: %v\n%s", err, out)
	} else {
		log.Printf("watch: re-indexed")
	}
}
