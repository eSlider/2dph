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

// Options controls the polling loop. Zero value uses defaults.
type Options struct {
	Dirs     []string
	Interval time.Duration
	// IndexCmd is the index command template. %s is replaced by the repo
	// root (from KB_ROOT). Defaults to `<root>/bin/brain/index.go --with-mail`.
	IndexCmd string
}

// Run blocks forever polling Dirs (defaults: KB_WATCH_DIRS or /corpus) every
// Interval (default 30s) and re-indexing when files change. KB_ROOT names the
// repo root used to locate bin/brain/index.go.
func Run(args []string) {
	opts := fromEnv(args)
	root, _ := os.Getwd()
	if r := os.Getenv("KB_ROOT"); r != "" {
		root = r
	}
	log.Printf("watch: dirs=%v interval=%s root=%s", opts.Dirs, opts.Interval, root)
	var last string
	for {
		if flag := Stamp(opts.Dirs); flag != "" && flag != last {
			last = flag
			reindex(opts.IndexCmd, root)
		}
		time.Sleep(opts.Interval)
	}
}

func fromEnv(args []string) Options {
	opts := Options{Interval: 30 * time.Second}
	if raw := os.Getenv("KB_WATCH_INTERVAL"); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil && n > 0 {
			opts.Interval = time.Duration(n) * time.Second
		}
	}
	defDirs := "/corpus"
	if raw := os.Getenv("KB_WATCH_DIRS"); raw != "" {
		defDirs = raw
	}
	if len(args) > 0 {
		opts.Dirs = args
	} else {
		for _, d := range strings.Split(defDirs, " ") {
			if d != "" {
				opts.Dirs = append(opts.Dirs, d)
			}
		}
	}
	opts.IndexCmd = "<root>/bin/brain/index.go --with-mail"
	return opts
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
