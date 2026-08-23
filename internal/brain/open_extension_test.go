//go:build cgo && system_ladybug

package brain

import (
	"os"
	"path/filepath"
	"testing"
)

// TestOpenWithSandboxInstallsExtensions is the regression for #147: the GitHub
// Actions write-path job failed with
//
//	LOAD EXTENSION FTS: Binder exception: Extension: fts is an official
//	extension and has not been installed.
//
// on a fresh runner, while every local host passed. Local hosts have the FTS
// extension already cached under the home dir, so a bare LOAD EXTENSION FTS
// (without INSTALL) works. A clean runner has no such cache, so the serve
// read handle opened by openWithSandbox died at extension load. openWithSandbox
// must INSTALL the extension before LOADing it, exactly like OpenWritable does.
//
// This test isolates HOME to a temp dir so the extension cache is empty on any
// host, deterministically reproducing the runner condition. It must pass after
// the fix even though a bare LOAD would fail here.
func TestOpenWithSandboxInstallsExtensions(t *testing.T) {
	restoreCfg := soakSetup(t)
	defer restoreCfg()

	dbpath := dbPath()
	if err := os.MkdirAll(filepath.Dir(dbpath), 0o755); err != nil {
		t.Fatal(err)
	}

	// Seed with OpenWritable (INSTALL + LOAD) under the real HOME so the KB is
	// valid; the extension cache is then isolated from the openWithSandbox call.
	wdb, wconn, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatalf("seed OpenWritable: %v", err)
	}
	if err := InitSchema(wconn); err != nil {
		t.Fatal(err)
	}
	vec := make([]float64, EmbedDim)
	vec[0] = 0.5
	if _, err := AddLeafs(wconn, []LeafInput{
		{Text: "extension proof leaf", Source: "ext-test", Root: "info", Type: "reference", Embedding: vec},
	}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureIndexes(wconn); err != nil {
		t.Fatal(err)
	}
	wconn.Close()
	wdb.Close()

	// Isolate HOME to an empty temp dir AFTER seeding: openWithSandbox must be
	// able to load FTS/VECTOR from a cold extension cache (INSTALL then LOAD).
	t.Setenv("HOME", t.TempDir())

	if err := openWithSandbox(brainCfg().Eps); err != nil {
		t.Fatalf("openWithSandbox on clean extension cache: %v", err)
	}
	defer func() {
		brainMu.Lock()
		closeBrainLocked()
		brainMu.Unlock()
	}()

	h := HTTP{}
	if _, statErr := h.Stats(t.Context()); statErr != nil {
		t.Fatalf("stats after openWithSandbox: %v", statErr)
	}
}
