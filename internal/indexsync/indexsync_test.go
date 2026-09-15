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

	docHive := t.TempDir()
	for _, p := range []struct{ src, ch string }{{"portals", "o2"}, {"portals", "dkv"}} {
		d := filepath.Join(docHive, "source="+p.src, "channel="+p.ch, "dt=2026-09-13")
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		file := filepath.Join(d, "data_0.parquet")
		if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		now := time.Now()
		if err := os.Chtimes(file, now, now); err != nil {
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
		DocumentsHive: docHive,
		DB:            db,
		MailSource:    "gator",
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
	if len(rep.DocPartitions) != 2 || rep.DocPartitions[0] != "portals/dkv" || rep.DocPartitions[1] != "portals/o2" {
		t.Fatalf("doc partitions = %v, want [portals/dkv portals/o2]", rep.DocPartitions)
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
		"mail-leaf --document --commit --skip --force --hive-doc " + cfg.DocumentsHive,
		"brain-index --skip",
		"mail-graph --channel gmail --commit --skip-existing --force",
		"mail-graph --channel wheregroup --commit --skip-existing --force",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("calls missing %q:\n%s", want, got)
		}
	}
	// gator mode must stay off the local corpus: no --with-mail.
	if strings.Contains(got, "--with-mail") {
		t.Fatalf("gator mode passed --with-mail:\n%s", got)
	}
	if rep.MailSource != "gator" {
		t.Fatalf("report mail source = %q, want gator", rep.MailSource)
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

// TestCycleSkipsDocumentWhenHiveEmpty: an empty/absent document hive must not
// invoke the doc import (the mail path keeps working).
func TestCycleSkipsDocumentWhenHiveEmpty(t *testing.T) {
	cfg, logPath, _, dc := setup(t)
	cfg.DocumentsHive = t.TempDir() // no source= partitions
	rep, err := Cycle(context.Background(), cfg, dc)
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if len(rep.DocPartitions) != 0 {
		t.Fatalf("doc partitions = %v, want empty", rep.DocPartitions)
	}
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(logs), "--document") {
		t.Fatalf("doc import ran on an empty hive:\n%s", logs)
	}
}

// missingDir returns a path that does not exist (an upstream tree not yet
// produced by gator). Must be nested under a real temp dir so cleanup works.
func missingDir(t *testing.T) string {
	t.Helper()
	return filepath.Join(t.TempDir(), "not-produced-yet")
}

// TestCycleMailAbsentImportsDocuments: with the mail hive missing (mail not yet
// produced on this host), the document tree must still be imported and the
// cycle must succeed without touching last_error.
func TestCycleMailAbsentImportsDocuments(t *testing.T) {
	cfg, logPath, _, dc := setup(t)
	cfg.Hive = missingDir(t)

	rep, err := Cycle(context.Background(), cfg, dc)
	if err != nil {
		t.Fatalf("cycle with absent mail hive: %v", err)
	}
	if len(rep.Channels) != 0 {
		t.Fatalf("channels = %v, want none", rep.Channels)
	}
	if len(rep.DocPartitions) != 2 {
		t.Fatalf("doc partitions = %v, want 2", rep.DocPartitions)
	}

	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(logs)
	if !strings.Contains(got, "mail-leaf --document --commit --skip --force --hive-doc "+cfg.DocumentsHive) {
		t.Fatalf("doc import did not run on a mail-less cycle:\n%s", got)
	}
	// The mail leaf/graph steps must be skipped entirely, not failed.
	if strings.Contains(got, "mail-leaf --commit") {
		t.Fatalf("mail-leaf ran although the mail hive is absent:\n%s", got)
	}
	if strings.Contains(got, "mail-graph") {
		t.Fatalf("mail-graph ran although the mail hive is absent:\n%s", got)
	}

	v := brain.ViewFreshness(cfg.Root, cfg.DB)
	if v.LastError != "" {
		t.Fatalf("absent mail hive set last_error: %q", v.LastError)
	}
	if v.Stale {
		t.Fatalf("freshness stale after a documents-only cycle: %+v", v)
	}
}

// TestCycleDocumentsAbsentKeepsMail: a missing document hive must not affect
// the mail path or the cycle outcome.
func TestCycleDocumentsAbsentKeepsMail(t *testing.T) {
	cfg, logPath, _, dc := setup(t)
	cfg.DocumentsHive = missingDir(t)

	rep, err := Cycle(context.Background(), cfg, dc)
	if err != nil {
		t.Fatalf("cycle with absent documents hive: %v", err)
	}
	if rep.Imported != 2 || len(rep.DocPartitions) != 0 {
		t.Fatalf("report = %+v, want 2 mail imports and no docs", rep)
	}
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(logs)
	if !strings.Contains(got, "mail-leaf --commit --skip --force") {
		t.Fatalf("mail leaf did not run:\n%s", got)
	}
	if !strings.Contains(got, "mail-graph --channel gmail") {
		t.Fatalf("mail graph did not run:\n%s", got)
	}
	if strings.Contains(got, "--document") {
		t.Fatalf("doc import ran although the document hive is absent:\n%s", got)
	}
	if v := brain.ViewFreshness(cfg.Root, cfg.DB); v.LastError != "" || v.Stale {
		t.Fatalf("freshness bad after mail-only cycle: %+v", v)
	}
}

// TestCycleBothHivesAbsentIsNoop: nothing produced yet => clean no-op, no
// brain bounce, no binaries, and a stale last_error is cleared.
func TestCycleBothHivesAbsentIsNoop(t *testing.T) {
	cfg, logPath, fd, dc := setup(t)
	cfg.Hive = missingDir(t)
	cfg.DocumentsHive = missingDir(t)
	saveError(cfg, "previous cycle failed")

	rep, err := Cycle(context.Background(), cfg, dc)
	if err != nil {
		t.Fatalf("both-absent cycle: %v", err)
	}
	if !rep.Skipped || rep.Err != "" {
		t.Fatalf("report = %+v, want a clean skip", rep)
	}
	if fd.stopped != 0 || fd.started != 0 {
		t.Fatalf("brain bounced on a no-op cycle (stop=%d start=%d)", fd.stopped, fd.started)
	}
	if logs, _ := os.ReadFile(logPath); len(logs) != 0 {
		t.Fatalf("binaries ran on a no-op cycle:\n%s", logs)
	}
	if v := brain.ViewFreshness(cfg.Root, cfg.DB); v.LastError != "" || v.Stale {
		t.Fatalf("no-op did not clear freshness: %+v", v)
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

// TestCycleAnnMissingPreventsSkip: with ANN wired (--ann/ANN_BIN) but no index
// snapshot yet, an otherwise-fresh kb must NOT be skipped — the cycle has to
// run `ann ensure` to build the index. Once the snapshot exists, the normal
// freshness skip resumes.
func TestCycleAnnMissingPreventsSkip(t *testing.T) {
	cfg, logPath, fd, dc := setup(t)
	if _, err := Cycle(context.Background(), cfg, dc); err != nil {
		t.Fatal(err)
	}
	// Wire ANN after the first cycle: no var/state/vector.ann exists yet.
	cfg.AnnBin = writeScript(t, t.TempDir(), "brain-ann", logPath, "", false)

	before := fd.stopped
	rep, err := Cycle(context.Background(), cfg, dc)
	if err != nil {
		t.Fatal(err)
	}
	if rep.Skipped {
		t.Fatalf("cycle skipped while ANN index missing: %+v", rep)
	}
	if !rep.ANN {
		t.Fatalf("ann ensure did not run: %+v", rep)
	}
	if fd.stopped == before {
		t.Fatalf("brain not bounced for the ANN build")
	}
	logs, _ := os.ReadFile(logPath)
	if !strings.Contains(string(logs), "brain-ann ensure") {
		t.Fatalf("ann ensure missing from calls:\n%s", logs)
	}

	// Index snapshot now exists: the next cycle may skip again.
	if err := os.MkdirAll(filepath.Dir(cfg.annIndexPath()), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg.annIndexPath(), []byte("idx"), 0o644); err != nil {
		t.Fatal(err)
	}
	rep, err = Cycle(context.Background(), cfg, dc)
	if err != nil {
		t.Fatal(err)
	}
	if !rep.Skipped {
		t.Fatalf("cycle not skipped after ANN index appeared: %+v", rep)
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

// seedCorpusMail creates one message.md under the local 2dph M365 corpus so
// corpus mode has a freshness signal and something to index.
func seedCorpusMail(t *testing.T, root, id string) {
	t.Helper()
	d := filepath.Join(root, "var", "corpus", "mail", "m365", id)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "message.md"), []byte("# hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestCycleCorpusModeIndexesLocalMailAndSkipsGator: corpus mode (T10-A) calls
// brain-index --with-mail (and --since when configured), imports documents, and
// never runs the gator mail-leaf/mail-graph steps.
func TestCycleCorpusModeIndexesLocalMailAndSkipsGator(t *testing.T) {
	cfg, logPath, _, dc := setup(t)
	cfg.MailSource = "corpus"
	cfg.Since = "2026-01-01"
	seedCorpusMail(t, cfg.Root, "42")

	rep, err := Cycle(context.Background(), cfg, dc)
	if err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if rep.MailSource != "corpus" || !rep.CorpusMail || !rep.Indexed {
		t.Fatalf("report = %+v", rep)
	}
	if len(rep.Channels) != 0 {
		t.Fatalf("corpus mode discovered gator channels: %v", rep.Channels)
	}
	if len(rep.DocPartitions) != 2 {
		t.Fatalf("doc partitions = %v, want 2", rep.DocPartitions)
	}

	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(logs)
	if !strings.Contains(got, "brain-index --skip --db "+cfg.DB+" --with-mail") {
		t.Fatalf("brain-index did not get --with-mail:\n%s", got)
	}
	if !strings.Contains(got, "--since 2026-01-01") {
		t.Fatalf("brain-index did not get --since:\n%s", got)
	}
	if !strings.Contains(got, "mail-leaf --document --commit --skip --force --hive-doc "+cfg.DocumentsHive) {
		t.Fatalf("document import did not run in corpus mode:\n%s", got)
	}
	if strings.Contains(got, "mail-leaf --commit") {
		t.Fatalf("gator mail-leaf ran in corpus mode:\n%s", got)
	}
	if strings.Contains(got, "mail-graph") {
		t.Fatalf("gator mail-graph ran in corpus mode:\n%s", got)
	}
	if v := brain.ViewFreshness(cfg.Root, cfg.DB); v.LastError != "" || v.Stale {
		t.Fatalf("freshness bad after corpus cycle: %+v", v)
	}
}

// TestCycleCorpusModeAbsentMailStillImportsDocuments: a missing/empty local
// corpus must not fail the cycle or block the document tree.
func TestCycleCorpusModeAbsentMailStillImportsDocuments(t *testing.T) {
	cfg, logPath, _, dc := setup(t)
	cfg.MailSource = "corpus"

	rep, err := Cycle(context.Background(), cfg, dc)
	if err != nil {
		t.Fatalf("cycle with absent corpus mail: %v", err)
	}
	if rep.CorpusMail {
		t.Fatalf("corpus mail reported present on an empty corpus: %+v", rep)
	}
	if len(rep.DocPartitions) != 2 {
		t.Fatalf("doc partitions = %v, want 2", rep.DocPartitions)
	}
	logs, err := os.ReadFile(logPath)
	if err != nil {
		t.Fatal(err)
	}
	got := string(logs)
	if !strings.Contains(got, "mail-leaf --document") {
		t.Fatalf("document import did not run:\n%s", got)
	}
	if !strings.Contains(got, "--with-mail") {
		t.Fatalf("brain-index missing --with-mail:\n%s", got)
	}
	if strings.Contains(got, "mail-leaf --commit") || strings.Contains(got, "mail-graph") {
		t.Fatalf("gator mail path ran in corpus mode:\n%s", got)
	}
	if v := brain.ViewFreshness(cfg.Root, cfg.DB); v.LastError != "" || v.Stale {
		t.Fatalf("freshness bad after docs-only corpus cycle: %+v", v)
	}
}

// TestCycleCorpusModeNoUpstreamIsNoop: with neither the local mail corpus nor
// the document hive populated, the cycle is a clean no-op and clears last_error.
func TestCycleCorpusModeNoUpstreamIsNoop(t *testing.T) {
	cfg, logPath, fd, dc := setup(t)
	cfg.MailSource = "corpus"
	cfg.DocumentsHive = missingDir(t)
	saveError(cfg, "previous cycle failed")

	rep, err := Cycle(context.Background(), cfg, dc)
	if err != nil {
		t.Fatalf("corpus no-upstream cycle: %v", err)
	}
	if !rep.Skipped || rep.Err != "" {
		t.Fatalf("report = %+v, want a clean skip", rep)
	}
	if fd.stopped != 0 || fd.started != 0 {
		t.Fatalf("brain bounced on a no-op corpus cycle (stop=%d start=%d)", fd.stopped, fd.started)
	}
	if logs, _ := os.ReadFile(logPath); len(logs) != 0 {
		t.Fatalf("binaries ran on a no-op corpus cycle:\n%s", logs)
	}
	if v := brain.ViewFreshness(cfg.Root, cfg.DB); v.LastError != "" || v.Stale {
		t.Fatalf("no-op did not clear freshness: %+v", v)
	}
}
