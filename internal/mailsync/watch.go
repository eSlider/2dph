package mailsync

// Watch loop for generic IMAP accounts: sync new .eml → convert to markdown
// → push un-ingested leafs into a running brain via POST /ingest (visible
// immediately, no rebuild/restart) → IMAP IDLE until something changes.
//
//	./bin/mail/watch.go --source imap --env .secrets/mail.env
//	./bin/mail/watch.go --source imap --once          # single cycle
//	./bin/mail/watch.go --source imap --brain http://127.0.0.1:8630
//
// Ingestion state lives in <out>/.watch-state.json so restarts never double-
// ingest. A failed brain POST leaves the leaf unmarked and it is retried on
// the next cycle.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/mailconv"
	"github.com/eSlider/2dph/internal/markdown"
)

// WatchConfig is the watch-loop superset of CLIConfig.
type WatchConfig struct {
	CLI       CLIConfig
	Brain     string        // brain base URL; default http://127.0.0.1:8630
	OCR       bool          // pass-through to import conversion
	Once      bool          // one cycle then exit
	IdleWait  time.Duration // max IDLE quiet window before rescan; default 20m
	StatePath string        // ingestion state; default <out>/.watch-state.json
}

const watchStateFile = ".watch-state.json"

// ParseWatchCLI parses watch flags on top of the sync flags.
func ParseWatchCLI(args []string) (WatchConfig, int, error) {
	cli, code, err := ParseCLI(args)
	if err != nil || code != 0 {
		return WatchConfig{}, code, err
	}
	wc := WatchConfig{
		CLI:      cli,
		Brain:    cli.Brain,
		OCR:      cli.OCR,
		IdleWait: 20 * time.Minute,
	}
	if wc.Brain == "" {
		wc.Brain = envLookup(cli.Env, "BRAIN_URL", "WATCH_BRAIN")
	}
	if wc.Brain == "" {
		wc.Brain = "http://127.0.0.1:8630"
	}
	if !wc.CLI.Local && wc.CLI.Sync.IMAP == nil && wc.CLI.Sync.OO == nil && wc.CLI.Sync.Gmail == nil && wc.CLI.Sync.M365 == nil {
		return WatchConfig{}, 2, fmt.Errorf("watch needs a source: --source imap (with IMAP_* in %s)", cli.Env)
	}
	if wc.CLI.Sync.Out == "" {
		return WatchConfig{}, 2, fmt.Errorf("watch: out dir unresolved")
	}
	wc.StatePath = filepath.Join(wc.CLI.Sync.Out, watchStateFile)
	return wc, 0, nil
}

// envLookup checks process env first, then KEY=VALUE lines of the env file.
func envLookup(envPath string, keys ...string) string {
	for _, k := range keys {
		if v := os.Getenv(k); v != "" {
			return v
		}
	}
	b, err := os.ReadFile(envPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		for _, want := range keys {
			if strings.TrimSpace(k) == want {
				return strings.Trim(strings.TrimSpace(v), "\"'")
			}
		}
	}
	return ""
}

// WatchMain is the CLI entry for bin/mail/watch.go; returns exit code.
func WatchMain(args []string) int {
	cfg, code, err := ParseWatchCLI(args)
	if err != nil {
		fmt.Fprintln(os.Stderr, "mail/watch:", err)
		return code
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	for {
		n, err := runCycle(ctx, cfg)
		if err != nil {
			fmt.Fprintf(os.Stderr, "mail/watch: cycle: %v\n", err)
			// Back off so a bad password / down brain doesn't spin hot.
			select {
			case <-time.After(30 * time.Second):
			case <-ctx.Done():
				return 0
			}
		}
		if cfg.Once {
			if n >= 0 {
				fmt.Printf("mail/watch: cycle ingested=%d\n", n)
			}
			return 0
		}
		fmt.Printf("mail/watch: cycle done (ingested=%d), waiting for changes\n", n)
		waitForChange(ctx, cfg)
	}
}

// waitForChange blocks on IMAP IDLE across all folders (or plain sleep when
// no IMAP source is configured).
func waitForChange(ctx context.Context, cfg WatchConfig) {
	if icfg := cfg.CLI.Sync.IMAP; icfg != nil {
		folders := icfg.Folders
		if len(folders) == 0 {
			discovered, err := IMAPFolders(*icfg)
			if err == nil {
				folders = discovered
			}
		}
		_ = IMAPIdle(*icfg, folders, cfg.IdleWait)
		return
	}
	select {
	case <-time.After(cfg.IdleWait):
	case <-ctx.Done():
	}
}

// runCycle executes sync → convert → push. Returns ingested leaf count (-1
// if the sync stage failed hard).
func runCycle(ctx context.Context, cfg WatchConfig) (int, error) {
	if !cfg.CLI.Local {
		stats, err := Run(ctx, cfg.CLI.Sync)
		if err != nil {
			return -1, fmt.Errorf("sync: %w", err)
		}
		fmt.Printf("mail/watch: sync checked=%d new=%d skipped=%d failed=%d\n",
			stats.Checked, stats.New, stats.Skipped, stats.Failed)
	}

	if ok, skip, fail, cerr := mailconv.FromEML(cfg.CLI.Sync.Out, cfg.OCR, false, false); cerr != nil || fail > 0 {
		fmt.Fprintf(os.Stderr, "mail/watch: convert ok=%d skip=%d fail=%d err=%v\n", ok, skip, fail, cerr)
	} else if ok > 0 {
		fmt.Printf("mail/watch: converted %d new message.md (skip=%d)\n", ok, skip)
	}

	return PushNewLeafs(cfg.CLI.Sync.Out, cfg.StatePath, cfg.Brain)
}

// loadWatchState reads the ingested-paths set.
func loadWatchState(path string) (map[string]bool, error) {
	state := map[string]bool{}
	b, err := os.ReadFile(path)
	if err != nil {
		return state, nil // fresh start
	}
	if err := json.Unmarshal(b, &state); err != nil {
		return nil, fmt.Errorf("state %s: %w", path, err)
	}
	return state, nil
}

func saveWatchState(path string, state map[string]bool) error {
	b, err := json.MarshalIndent(state, "", " ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

type ingestLeaf struct {
	Text      string `json:"text"`
	Source    string `json:"source"`
	Root      string `json:"root,omitempty"`
	Type      string `json:"type,omitempty"`
	ValidFrom string `json:"valid_from,omitempty"`
	How       string `json:"how,omitempty"`
}

// PushNewLeafs walks root for message.md (+ sibling attachment .md), pushes
// every file not yet in the state set to POST <brain>/ingest, and marks it
// done on success. Returns the number of newly ingested leafs.
func PushNewLeafs(root, statePath, brain string) (int, error) {
	state, err := loadWatchState(statePath)
	if err != nil {
		return 0, err
	}

	var mds []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if filepath.Base(p) == "message.md" {
			mds = append(mds, p)
		}
		return nil
	})

	ingested := 0
	client := &http.Client{Timeout: 120 * time.Second}
	dirty := false

	for _, md := range mds {
		if state[md] {
			continue
		}
		raw, err := os.ReadFile(md)
		if err != nil {
			continue
		}
		meta, _ := markdown.ExtractFrontmatter(string(raw))
		date := meta["date"]
		if len(date) >= 10 {
			date = date[:10]
		}
		id := filepath.Base(filepath.Dir(md))

		files := []string{md}
		if entries, err := os.ReadDir(filepath.Join(filepath.Dir(md), "attachments")); err == nil {
			for _, e := range entries {
				if strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
					files = append(files, filepath.Join(filepath.Dir(md), "attachments", e.Name()))
				}
			}
		}

		var leafs []ingestLeaf
		for _, f := range files {
			fraw, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			src := fmt.Sprintf("ooMail:%s:%s", id, filepath.Base(f))
			for _, lf := range markdown.ToAll(string(fraw), f, "ooMail") {
				leafs = append(leafs, ingestLeaf{
					Text:      lf.Text,
					Source:    src,
					Root:      "info",
					Type:      lf.Type,
					ValidFrom: date,
					How:       "mail/import",
				})
			}
		}
		if len(leafs) == 0 {
			continue
		}

		payload, err := json.Marshal(map[string]any{"leafs": leafs})
		if err != nil {
			return ingested, err
		}
		resp, err := client.Post(strings.TrimSuffix(brain, "/")+"/ingest", "application/json", bytes.NewReader(payload))
		if err != nil {
			return ingested, fmt.Errorf("brain %s: %w", brain, err)
		}
		_ = resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			return ingested, fmt.Errorf("brain ingest %s: HTTP %d", id, resp.StatusCode)
		}

		for _, f := range files {
			state[f] = true
			dirty = true
		}
		state[md] = true
		ingested += len(leafs)
		if err := saveWatchState(statePath, state); err != nil {
			return ingested, err
		}
	}
	if dirty {
		_ = saveWatchState(statePath, state)
	}
	return ingested, nil
}
