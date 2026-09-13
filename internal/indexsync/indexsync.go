// Package indexsync implements the compose-managed periodic cycle that closes
// the chain gator sync→etl→pack → 2dph import(graph) → index(kb.lbug)
// (issue #292).
//
// One cycle:
//  1. discover the gator kind=mail channels from the parquet hive;
//  2. quiesce the compose brain container (Ladybug is single-writer);
//  3. incremental index of the corpus into kb.lbug (--skip; builds missing
//     indexes only);
//  4. import every gator channel into the Message/Person graph (idempotent
//     MERGE — a re-run creates zero duplicates);
//  5. maintain the ANN vector index (when enabled);
//  6. restart the brain and wait for it to become healthy;
//  7. persist a freshness state file for /stats and scripts/stack/status.
//
// The cycle is skipped when the kb is already newer than the newest gator pack
// and the last run did not error, so the brain is not bounced for nothing.
package indexsync

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/dockerctl"
	"github.com/eSlider/2dph/internal/mailgraph"
	"github.com/eSlider/2dph/pkg/utils"
)

// Config is the cycle runtime configuration. Empty fields resolve to defaults.
type Config struct {
	Root          string
	Hive          string
	DB            string
	MailGraphBin  string
	BrainIndexBin string
	AnnBin        string
	Interval      time.Duration
	StaleAfter    time.Duration
	Quiesce       bool
	Rebuild       bool
	Project       string
	Service       string
	Channels      []string
}

// WithDefaults fills the standard 2dph layout (no host-absolute paths).
func (c Config) WithDefaults() Config {
	if c.Root == "" {
		c.Root = utils.Root()
	}
	if c.DB == "" {
		c.DB = filepath.Join(c.Root, "var", "kb.lbug")
	}
	if c.MailGraphBin == "" {
		c.MailGraphBin = "mail-graph"
	}
	if c.BrainIndexBin == "" {
		c.BrainIndexBin = "brain-index"
	}
	if c.Interval <= 0 {
		c.Interval = time.Hour
	}
	if c.StaleAfter <= 0 {
		c.StaleAfter = brain.DefaultStaleAfter
	}
	if c.Project == "" {
		c.Project = "2dph"
	}
	if c.Service == "" {
		c.Service = "brain"
	}
	return c
}

// Report is the outcome of one cycle.
type Report struct {
	StartedAt time.Time
	Skipped   bool
	Reason    string
	Channels  []string
	Imported  int
	Indexed   bool
	ANN       bool
	Err       string
}

// Summary renders one human-readable log line.
func (r Report) Summary() string {
	switch {
	case r.Err != "":
		return fmt.Sprintf("index-sync: error=%s channels=%v imported=%d indexed=%v", r.Err, r.Channels, r.Imported, r.Indexed)
	case r.Skipped:
		return fmt.Sprintf("index-sync: skip (%s)", r.Reason)
	default:
		return fmt.Sprintf("index-sync: ok channels=%v imported=%d indexed=%v ann=%v", r.Channels, r.Imported, r.Indexed, r.ANN)
	}
}

// runner is the exec seam (same pattern as internal/cron): a package var so
// tests drive the real binaries through temp scripts instead of stubbing our
// own interfaces.
var runner = func(ctx context.Context, name string, args ...string) ([]byte, error) {
	return exec.CommandContext(ctx, name, args...).CombinedOutput()
}

// Cycle runs one import+index cycle. The brain is restarted even when a step
// fails; the failure is persisted as freshness.last_error so it stays visible.
func Cycle(ctx context.Context, cfg Config, dc *dockerctl.Client) (rep Report, err error) {
	cfg = cfg.WithDefaults()
	rep.StartedAt = time.Now().UTC()

	channels := cfg.Channels
	if len(channels) == 0 {
		channels, err = mailgraph.Channels(cfg.Hive)
		if err != nil {
			rep.Err = err.Error()
			saveError(cfg, rep.Err)
			return rep, err
		}
	}
	rep.Channels = channels

	packNano, packErr := mailgraph.PackMtime(cfg.Hive)
	if packErr != nil {
		packNano = 0
	}
	if skip := shouldSkip(cfg, packNano); skip != "" {
		rep.Skipped = true
		rep.Reason = skip
		return rep, nil
	}

	// Quiesce: Ladybug is single-writer, so the brain must release kb.lbug.
	var release func()
	if cfg.Quiesce && dc != nil {
		release, err = quiesceBrain(ctx, cfg, dc)
		if err != nil {
			rep.Err = err.Error()
			saveError(cfg, rep.Err)
			return rep, err
		}
		defer release()
	}

	// Corpus index first: --rebuild deletes the db, so the graph import must
	// follow it; --skip preserves an existing graph and only adds new leafs.
	idxArgs := []string{"--with-mail"}
	if cfg.Rebuild {
		idxArgs = append(idxArgs, "--rebuild")
	} else {
		idxArgs = append(idxArgs, "--skip")
	}
	if cfg.DB != "" {
		idxArgs = append(idxArgs, "--db", cfg.DB)
	}
	if out, err := runner(ctx, cfg.BrainIndexBin, idxArgs...); err != nil {
		rep.Err = fmt.Sprintf("index: %v (%s)", err, lastLine(out))
		saveError(cfg, rep.Err)
		return rep, fmt.Errorf("%s", rep.Err)
	}
	rep.Indexed = true

	// Graph import: idempotent MERGE per channel.
	for _, ch := range channels {
		args := []string{"--channel", ch, "--commit", "--skip-existing", "--force"}
		if cfg.Hive != "" {
			args = append(args, "--hive", cfg.Hive)
		}
		if cfg.DB != "" {
			args = append(args, "--db", cfg.DB)
		}
		if out, err := runner(ctx, cfg.MailGraphBin, args...); err != nil {
			rep.Err = fmt.Sprintf("graph %s: %v (%s)", ch, err, lastLine(out))
			saveError(cfg, rep.Err)
			return rep, fmt.Errorf("%s", rep.Err)
		}
		rep.Imported++
	}

	if cfg.AnnBin != "" {
		if out, err := runner(ctx, cfg.AnnBin, "ensure"); err != nil {
			rep.Err = fmt.Sprintf("ann: %v (%s)", err, lastLine(out))
			saveError(cfg, rep.Err)
			return rep, fmt.Errorf("%s", rep.Err)
		}
		rep.ANN = true
	}

	saveSuccess(cfg, channels, packNano)
	return rep, nil
}

// Run loops Cycle every Interval until ctx is cancelled. The callback (may be
// nil) receives every cycle report for logging.
func Run(ctx context.Context, cfg Config, dc *dockerctl.Client, onCycle func(Report)) error {
	cfg = cfg.WithDefaults()
	for {
		rep, err := Cycle(ctx, cfg, dc)
		if onCycle != nil {
			onCycle(rep)
		}
		_ = err // reported via onCycle; loop keeps going
		if ctx.Err() != nil {
			return ctx.Err()
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(cfg.Interval):
		}
	}
}

// shouldSkip returns a non-empty reason when the projection is already at
// least as fresh as the newest gator pack and the last cycle did not error.
func shouldSkip(cfg Config, packNano int64) string {
	if cfg.Rebuild || packNano == 0 {
		return ""
	}
	state := brain.LoadFreshness(cfg.Root)
	if state.LastError != "" || state.IndexAt == "" {
		return ""
	}
	info, err := os.Stat(cfg.DB)
	if err != nil {
		return ""
	}
	if info.ModTime().UnixNano() >= packNano {
		return "kb newer than gator pack"
	}
	return ""
}

// quiesceBrain finds the compose brain container, stops it, and returns a
// release func that starts it again and waits for health.
func quiesceBrain(ctx context.Context, cfg Config, dc *dockerctl.Client) (func(), error) {
	labels := map[string]string{
		"com.docker.compose.project": cfg.Project,
		"com.docker.compose.service": cfg.Service,
	}
	id, err := dc.FindOne(ctx, labels)
	if err != nil {
		return nil, fmt.Errorf("quiesce: %w", err)
	}
	if err := dc.Stop(ctx, id, 30); err != nil {
		return nil, fmt.Errorf("quiesce: %w", err)
	}
	release := func() {
		rctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		if err := dc.Start(rctx, id); err != nil {
			fmt.Fprintf(os.Stderr, "index-sync: start brain: %v\n", err)
			return
		}
		if err := dc.WaitHealthy(rctx, id, 90*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "index-sync: %v\n", err)
		}
	}
	return release, nil
}

func saveSuccess(cfg Config, channels []string, packNano int64) {
	now := time.Now().UTC().Format(time.RFC3339)
	f := brain.LoadFreshness(cfg.Root)
	f.ImportAt = now
	f.IndexAt = now
	f.Channels = channels
	f.StaleAfter = cfg.StaleAfter.String()
	f.LastError = ""
	if packNano > 0 {
		f.PackMtime = time.Unix(0, packNano).UTC().Format(time.RFC3339)
	}
	if info, err := os.Stat(cfg.DB); err == nil {
		f.KBMtime = info.ModTime().UTC().Format(time.RFC3339)
	}
	_ = brain.SaveFreshness(cfg.Root, f)
}

func saveError(cfg Config, msg string) {
	f := brain.LoadFreshness(cfg.Root)
	f.LastError = msg
	f.StaleAfter = cfg.StaleAfter.String()
	_ = brain.SaveFreshness(cfg.Root, f)
}

func lastLine(b []byte) string {
	s := string(b)
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '\n' {
			if i == len(s)-1 {
				s = s[:i]
				continue
			}
			return s[i+1:]
		}
	}
	return s
}
