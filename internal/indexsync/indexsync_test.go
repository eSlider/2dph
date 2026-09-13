package indexsync

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/dockerctl"
)

// fakeDocker serves the four Engine endpoints the cycle needs, counting the
// stop/start calls. Real HTTP against a real server (API test), no mock of our
// own interfaces.
type fakeDocker struct {
	stopped, started int
}

func (f *fakeDocker) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/containers/json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{{"Id": "brainc"}})
	})
	mux.HandleFunc("/containers/brainc/stop", func(w http.ResponseWriter, r *http.Request) {
		f.stopped++
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/containers/brainc/start", func(w http.ResponseWriter, r *http.Request) {
		f.started++
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("/containers/brainc/json", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"State": map[string]any{"Running": true, "Health": map[string]any{"Status": "healthy"}},
		})
	})
	return mux
}

// writeScript writes an executable stub "binary" that records its argv to a
// log file and, when touchPath is set, touches it (simulating a db write).
func writeScript(t *testing.T, dir, name, logPath, touchPath string, fail bool) string {
	t.Helper()
	path := filepath.Join(dir, name)
	var b strings.Builder
	b.WriteString("#!/usr/bin/env bash\n")
	b.WriteString("echo \"" + name + " $*\" >> '" + logPath + "'\n")
	if touchPath != "" {
		b.WriteString("touch '" + touchPath + "'\n")
	}
	if fail {
		b.WriteString("echo boom >&2\nexit 1\n")
	}
	if err := os.WriteFile(path, []byte(b.String()), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func setup(t *testing.T) (Config, string, *fakeDocker, *dockerctl.Client) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "var"), 0o755); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(root, "var", "kb.lbug")
	if err := os.WriteFile(db, []byte("db"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-72 * time.Hour)
	if err := os.Chtimes(db, old, old); err != nil {
		t.Fatal(err)
	}

	hive := t.TempDir()
	for _, ch := range []string{"gmail", "wheregroup"} {
		d := filepath.Join(hive, "source=mail", "channel="+ch, "dt=2026-09-13")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		p := filepath.Join(d, "data_0.parquet")
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		if err := os.Chtimes(p, now, now); err != nil {
			t.Fatal(err)
		}
	}

	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "calls.log")
	fd := &fakeDocker{}
	srv := httptest.NewServer(fd.handler())
	t.Cleanup(srv.Close)
	dc, err := dockerctl.NewHTTP(srv.URL, srv.Client())
	if err != nil {
		t.Fatal(err)
	}

	cfg := Config{
		Root:          root,
		Hive:          hive,
		DB:            db,
		MailLeafBin:   writeScript(t, binDir, "mail-leaf", logPath, "", false),
		MailGraphBin:  writeScript(t, binDir, "mail-graph", logPath, "", false),
		BrainIndexBin: writeScript(t, binDir, "brain-index", logPath, db, false),
		Quiesce:       true,
		Project:       "2dph",
		Service:       "brain",
	}
	return cfg, logPath, fd, dc
}

func TestCycleImportsAllChannelsAndQuiesces(t *testing.T) {
	cfg, logPath, fd, dc := setup(t)
	rep, err := Cycle(context.Background(), cfg, dc)
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if rep.Skipped || rep.Imported != 2 || !rep.Indexed {
		t.Fatalf("report = %+v", rep)
	}
	if fd.stopped != 1 || fd.started != 1 {
		t.Fatalf("quiesce stop=%d start=%d, want 1/1", fd.stopped, fd.started)
	}
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(logs)
	for _, want := range []string{
		"mail-leaf --commit --skip --force",
		"--hive " + cfg.Hive,
		"brain-index --skip",
		"mail-graph --channel gmail --commit --skip-existing --force",
		"mail-graph --channel wheregroup --commit --skip-existing --force",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("calls missing %q:\n%s", want, got)
		}
	}

	// Freshness state must be readable and non-stale.
	v := brain.ViewFreshness(cfg.Root, cfg.DB)
	if v.Stale {
		t.Fatalf("freshness stale after a successful cycle: %+v", v)
	}
	if v.ImportAt == "" || v.IndexAt == "" {
		t.Fatalf("freshness times empty: %+v", v)
	}
}

// TestCycleIdempotentSkip: once the kb is newer than the gator pack and the
// last cycle succeeded, a re-run must not bounce the brain again.
func TestCycleIdempotentSkip(t *testing.T) {
	cfg, _, fd, dc := setup(t)
	if _, err := Cycle(context.Background(), cfg, dc); err != nil {
		t.Fatal(err)
	}
	before := fd.stopped
	rep, err := Cycle(context.Background(), cfg, dc)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Skipped {
		t.Fatalf("second cycle not skipped: %+v", rep)
	}
	if fd.stopped != before {
		t.Fatalf("brain bounced on an already-fresh cycle (stop=%d)", fd.stopped)
	}
}

// TestCycleFailureSurfacesAndRestarts: a failing index step records the error
// in freshness and still restarts the brain.
func TestCycleFailureSurfacesAndRestarts(t *testing.T) {
	cfg, _, fd, dc := setup(t)
	cfg.BrainIndexBin = writeScript(t, t.TempDir(), "brain-index", filepath.Join(t.TempDir(), "log"), "", true)
	rep, err := Cycle(context.Background(), cfg, dc)
	if err == nil {
		t.Fatal("want error from failing index")
	}
	if rep.Err == "" || !strings.Contains(rep.Err, "boom") {
		t.Fatalf("report err = %q", rep.Err)
	}
	if fd.started != 1 {
		t.Fatalf("brain not restarted after failure (start=%d)", fd.started)
	}
	v := brain.ViewFreshness(cfg.Root, cfg.DB)
	if v.LastError == "" || !v.Stale {
		t.Fatalf("failure not visible in freshness: %+v", v)
	}
}
