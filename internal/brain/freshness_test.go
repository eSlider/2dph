package brain

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestFreshnessRoundTrip(t *testing.T) {
	root := t.TempDir()
	f := Freshness{
		ImportAt:  "2026-09-13T10:00:00Z",
		IndexAt:   "2026-09-13T10:05:00Z",
		KBMtime:   "2026-09-13T10:05:00Z",
		PackMtime: "2026-09-13T09:00:00Z",
		Channels:  []string{"gmail", "wheregroup"},
	}
	if err := SaveFreshness(root, f); err != nil {
		t.Fatal(err)
	}
	got := LoadFreshness(root)
	if got.IndexAt != f.IndexAt || got.ImportAt != f.ImportAt {
		t.Fatalf("load = %+v, want index/import %s/%s", got, f.IndexAt, f.ImportAt)
	}
	if len(got.Channels) != 2 || got.Channels[0] != "gmail" {
		t.Fatalf("channels = %v, want [gmail wheregroup]", got.Channels)
	}
	if got.UpdatedAt == "" {
		t.Fatal("UpdatedAt not stamped")
	}
}

// TestViewFreshnessNoStateIsStale: without a cycle state there is no known
// index timestamp, so the view must report stale (the exact #292 symptom).
func TestViewFreshnessNoStateIsStale(t *testing.T) {
	v := ViewFreshness(t.TempDir(), "")
	if !v.Stale {
		t.Fatal("missing state must be stale")
	}
	if v.StaleAfter != DefaultStaleAfter.String() {
		t.Fatalf("stale_after = %q, want %q", v.StaleAfter, DefaultStaleAfter)
	}
}

func TestViewFreshnessFreshIndexNotStale(t *testing.T) {
	root := t.TempDir()
	kb := filepath.Join(root, "var", "kb.lbug")
	if err := os.MkdirAll(filepath.Dir(kb), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kb, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if err := os.Chtimes(kb, now, now); err != nil {
		t.Fatal(err)
	}
	f := Freshness{
		IndexAt:    now.Format(time.RFC3339),
		PackMtime:  now.Add(-time.Hour).Format(time.RFC3339),
		StaleAfter: "2h",
	}
	if err := SaveFreshness(root, f); err != nil {
		t.Fatal(err)
	}
	v := ViewFreshness(root, kb)
	if v.Stale {
		t.Fatalf("fresh index reported stale: %+v", v)
	}
	if v.KBMtime == "" {
		t.Fatal("kb_mtime not populated")
	}
}

// TestViewFreshnessPackNewerThanKB: gator produced a newer pack than the
// indexed kb → the projection is behind upstream → stale.
func TestViewFreshnessPackNewerThanKB(t *testing.T) {
	root := t.TempDir()
	kb := filepath.Join(root, "var", "kb.lbug")
	if err := os.MkdirAll(filepath.Dir(kb), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(kb, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	old := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(kb, old, old); err != nil {
		t.Fatal(err)
	}
	f := Freshness{
		IndexAt:   old.Format(time.RFC3339),
		PackMtime: time.Now().Format(time.RFC3339),
	}
	if err := SaveFreshness(root, f); err != nil {
		t.Fatal(err)
	}
	if v := ViewFreshness(root, kb); !v.Stale {
		t.Fatal("newer gator pack must mark the projection stale")
	}
}

func TestViewFreshnessLastErrorStale(t *testing.T) {
	root := t.TempDir()
	now := time.Now().UTC()
	if err := SaveFreshness(root, Freshness{
		IndexAt:   now.Format(time.RFC3339),
		LastError: "index failed",
	}); err != nil {
		t.Fatal(err)
	}
	if v := ViewFreshness(root, ""); !v.Stale || v.LastError == "" {
		t.Fatalf("last_error must surface and mark stale: %+v", v)
	}
}
