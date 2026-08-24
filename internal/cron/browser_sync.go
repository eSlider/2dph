// Package cron implements periodic extraction → brain ingest daemons.
//
// The browser-sync daemon keeps the brain fresh from the browser corpus:
// it probes Thorium CDP via the agent-browser CLI, reads the last-known
// extracted data under var/corpus/{gmail,linkedin,djinni}/*.json, converts
// each entry into a brain leaf and pushes it to <brain>/ingest. A down
// Thorium is tolerated: the run skips extraction and still ingests the
// existing corpus (data already captured on the last successful run).
package cron

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/eSlider/2dph/pkg/utils"
)

// ErrThoriumUnavailable is returned by Extract when the Thorium CDP endpoint
// cannot be reached (agent-browser CLI failed or the browser is down). RunOnce
// treats it as a soft signal: skip live extraction, keep pushing the corpus.
var ErrThoriumUnavailable = errors.New("thorium unavailable")

// Leaf is the brain ingest payload (POST /ingest).
type Leaf struct {
	Text       string `json:"text"`
	Source     string `json:"source"`
	Root       string `json:"root,omitempty"`
	Confidence string `json:"confidence,omitempty"`
	Type       string `json:"type,omitempty"`
}

// Config is the browser-sync runtime configuration. Empty fields resolve to
// defaults via WithDefaults.
type Config struct {
	// Root is the repo root; corpus and log paths default under it.
	Root string
	// Corpus is the extraction output dir; default <root>/var/corpus.
	Corpus string
	// LogPath is the daemon log file; default <root>/var/log/browser-sync.log.
	LogPath string
	// Brain is the brain base URL; default http://127.0.0.1:8630.
	Brain string
	// AgentBin is the agent-browser CLI; default "agent-browser".
	AgentBin string
	// CDPPort is the Thorium CDP port; default "9222".
	CDPPort string
	// SkipExtract disables the Thorium probe (tests, headless cron). When set,
	// the run only pushes the existing corpus.
	SkipExtract bool
	// Timeout bounds a single /ingest POST. The brain embeds every leaf, so a
	// full corpus needs minutes; default 10m.
	Timeout time.Duration
}

// WithDefaults returns a copy of c with every empty field filled from the
// standard 2dph layout.
func (c Config) WithDefaults() Config {
	if c.Root == "" {
		c.Root = utils.Root()
	}
	if c.Corpus == "" {
		c.Corpus = filepath.Join(c.Root, "var", "corpus")
	}
	if c.LogPath == "" {
		c.LogPath = filepath.Join(c.Root, "var", "log", "browser-sync.log")
	}
	if c.Brain == "" {
		c.Brain = "http://127.0.0.1:8630"
	}
	if c.AgentBin == "" {
		c.AgentBin = "agent-browser"
	}
	if c.CDPPort == "" {
		c.CDPPort = "9222"
	}
	if c.Timeout <= 0 {
		c.Timeout = 10 * time.Minute
	}
	return c
}

// Report is the result of one run cycle, for logging and stats.
type Report struct {
	Extracted bool   // live Thorium extraction ran successfully
	Skipped   bool   // Thorium unavailable, pushed the last-known corpus
	Leafs     int    // leafs loaded from the corpus
	Ingested  int    // leafs accepted by the brain /ingest
	Err       string // per-cycle error, empty on success
}

// Summary renders the report as a single human-readable log line.
func (r Report) Summary() string {
	status := "ok"
	if r.Err != "" {
		status = "error: " + r.Err
	}
	note := ""
	if r.Skipped {
		note = " thorium unavailable, pushed last-known corpus"
	}
	return fmt.Sprintf("browser-sync: extracted=%v leafs=%d ingested=%d%s status=%s",
		r.Extracted, r.Leafs, r.Ingested, note, status)
}

// runner abstracts exec.CommandContext so tests can stub the CLI without a
// browser. defaultRunner shells out to the agent-browser binary.
type runner func(ctx context.Context, name string, args ...string) ([]byte, error)

var execRun runner = defaultRunner

func defaultRunner(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).Output()
}

// Extract probes Thorium CDP through the agent-browser CLI. A failed probe
// (browser down, CLI missing) wraps ErrThoriumUnavailable.
func Extract(ctx context.Context, cfg Config) error {
	c := cfg.WithDefaults()
	if _, err := execRun(ctx, c.AgentBin, "connect", c.CDPPort); err != nil {
		return fmt.Errorf("%w: %s connect %s: %v", ErrThoriumUnavailable, c.AgentBin, c.CDPPort, err)
	}
	return nil
}

// RunOnce executes a single cycle: probe Thorium (unless skipped), load the
// corpus, convert to leafs and push them to the brain. A down Thorium is not
// fatal: extraction is skipped and the existing corpus is still ingested.
func RunOnce(ctx context.Context, cfg Config) (Report, error) {
	c := cfg.WithDefaults()
	rep := Report{}

	if !c.SkipExtract {
		if err := Extract(ctx, c); err != nil {
			if errors.Is(err, ErrThoriumUnavailable) {
				rep.Skipped = true
			} else {
				rep.Err = err.Error()
				return rep, fmt.Errorf("extract: %w", err)
			}
		} else {
			rep.Extracted = true
		}
	}

	leafs, err := LoadCorpus(c.Corpus)
	if err != nil {
		rep.Err = err.Error()
		return rep, fmt.Errorf("corpus %s: %w", c.Corpus, err)
	}
	rep.Leafs = len(leafs)
	if len(leafs) == 0 {
		return rep, nil
	}

	n, err := Push(ctx, c.Brain, c.Timeout, leafs)
	rep.Ingested = n
	if err != nil {
		rep.Err = err.Error()
		return rep, err
	}
	return rep, nil
}
