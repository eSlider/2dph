package ann

import (
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
)

// TestANNDeleteHidesTombstonedRows is the recall gate of #4: a deleted leaf
// must leave Search immediately. Ranking a tombstone also burns a top-k slot
// (leafHitByID then drops it silently), so with k above the live count every
// returned id must be live and the list must still be full.
func TestANNDeleteHidesTombstonedRows(t *testing.T) {
	rows := syntheticRows(200, 10, 61)
	ix := New(Params{RngSeed: 3})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}

	dead := make([]string, 0, 100)
	live := make(map[string]bool, 100)
	for i, r := range rows {
		if i < 100 {
			dead = append(dead, r.ID)
			continue
		}
		live[r.ID] = true
	}
	n, err := ix.Delete(dead...)
	if err != nil {
		t.Fatalf("delete: %v", err)
	}
	if n != len(dead) {
		t.Fatalf("deleted = %d, want %d", n, len(dead))
	}
	if ix.Len() != 100 {
		t.Fatalf("len = %d, want 100 live", ix.Len())
	}
	if ix.Tombstones() != 100 {
		t.Fatalf("tombstones = %d, want 100", ix.Tombstones())
	}
	st := ix.Stats()
	if st.Live != 100 || st.Tombstones != 100 {
		t.Fatalf("stats = %+v, want live=100 tombstones=100", st)
	}
	if math.Abs(st.Ratio-0.5) > 1e-9 {
		t.Fatalf("ratio = %v, want 0.5", st.Ratio)
	}
	if ix.Lookup(dead[0]) {
		t.Fatalf("deleted id %s still reports as indexed", dead[0])
	}

	// A full-cluster query still yields a full, live-only top-k.
	q := queryNear(rows, 10, 0, rand.New(rand.NewSource(7)))
	got := ix.Search(q, 60)
	if len(got) != 60 {
		t.Fatalf("hits = %d, want 60 (tombstones must not consume slots)", len(got))
	}
	for _, h := range got {
		if !live[h.ID] {
			t.Fatalf("tombstoned id %s ranked", h.ID)
		}
	}

	// Deleting again (or an unknown id) is a no-op: reconcile may feed the
	// same obsolete ids every wave.
	if n2, err := ix.Delete(dead[0], "no-such-leaf"); err != nil || n2 != 0 {
		t.Fatalf("re-delete = (%d,%v), want (0,nil)", n2, err)
	}
	if ix.Len() != 100 || ix.Tombstones() != 100 {
		t.Fatalf("re-delete changed stats: %+v", ix.Stats())
	}
}

// TestANNSaveCompactsTombstones: Save writes the snapshot without tombstoned
// rows, drops them from the in-memory cells, rebuilds idCell and truncates the
// WAL — a reopened index has zero tombstones and no dead ids.
func TestANNSaveCompactsTombstones(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vector.ann")

	rows := syntheticRows(120, 6, 62)
	ix := New(Params{RngSeed: 4})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := ix.SaveTo(path); err != nil {
		t.Fatalf("save: %v", err)
	}

	dead := make([]string, 0, 40)
	for i, r := range rows {
		if i%3 == 0 {
			dead = append(dead, r.ID)
		}
	}
	if _, err := ix.Delete(dead...); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if ix.Dirty() == 0 {
		t.Fatal("dirty = 0 after delete, want WAL entries")
	}
	if err := ix.Save(); err != nil {
		t.Fatalf("compact save: %v", err)
	}
	if ix.Tombstones() != 0 {
		t.Fatalf("tombstones after save = %d, want 0 (compacted)", ix.Tombstones())
	}
	if ix.Len() != len(rows)-len(dead) {
		t.Fatalf("len after save = %d, want %d", ix.Len(), len(rows)-len(dead))
	}
	if ix.Dirty() != 0 {
		t.Fatalf("dirty after save = %d, want 0 (WAL reset)", ix.Dirty())
	}
	if _, err := os.Stat(path + ".wal"); !os.IsNotExist(err) {
		t.Fatalf("WAL still present after save: %v", err)
	}
	if ix.Lookup(dead[0]) {
		t.Fatalf("compacted id %s still indexed", dead[0])
	}

	ix2, err := Open(path, Params{RngSeed: 4})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer ix2.Close()
	if ix2.Len() != len(rows)-len(dead) || ix2.Tombstones() != 0 {
		t.Fatalf("reopened len/tombstones = %d/%d, want %d/0",
			ix2.Len(), ix2.Tombstones(), len(rows)-len(dead))
	}
	for _, id := range dead {
		if ix2.Lookup(id) {
			t.Fatalf("dead id %s survived compaction", id)
		}
	}
	q := queryNear(rows, 6, 0, rand.New(rand.NewSource(8)))
	for _, h := range ix2.Search(q, 30) {
		if ix2.Lookup(h.ID) == false {
			t.Fatalf("search returned non-indexed id %s", h.ID)
		}
	}
}

// TestANNDeleteSurvivesReopen: Delete is a WAL record ({"ID":..,"del":true}),
// so a restart between delete and the next Save keeps the ids dead — the
// index does not resurrect deleted leafs for one more wave.
func TestANNDeleteSurvivesReopen(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vector.ann")

	rows := syntheticRows(60, 6, 63)
	ix := New(Params{RngSeed: 5})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := ix.SaveTo(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	dead := []string{rows[0].ID, rows[1].ID, rows[2].ID}
	if _, err := ix.Delete(dead...); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := ix.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	ix2, err := Open(path, Params{RngSeed: 5})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer ix2.Close()
	if ix2.Len() != len(rows)-len(dead) {
		t.Fatalf("replayed len = %d, want %d", ix2.Len(), len(rows)-len(dead))
	}
	if ix2.Tombstones() != len(dead) {
		t.Fatalf("replayed tombstones = %d, want %d", ix2.Tombstones(), len(dead))
	}
	for _, id := range dead {
		if ix2.Lookup(id) {
			t.Fatalf("deleted id %s came back after replay", id)
		}
	}

	// Re-adding a tombstoned id (same content hash ⇒ same vector) revives it
	// instead of leaving two rows for one id.
	if err := ix2.Upsert([]Row{rows[0]}); err != nil {
		t.Fatalf("revive upsert: %v", err)
	}
	if !ix2.Lookup(rows[0].ID) || ix2.Tombstones() != len(dead)-1 {
		t.Fatalf("revive = lookup %v tombstones %d, want true/%d",
			ix2.Lookup(rows[0].ID), ix2.Tombstones(), len(dead)-1)
	}
	if ix2.Len() != len(rows)-len(dead)+1 {
		t.Fatalf("len after revive = %d, want %d", ix2.Len(), len(rows)-len(dead)+1)
	}
}

// TestANNBuildDropsTombstones: a full Build replaces the index, so a stale
// tombstone set must not survive it.
func TestANNBuildDropsTombstones(t *testing.T) {
	rows := syntheticRows(40, 4, 64)
	ix := New(Params{RngSeed: 6})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	if _, err := ix.Delete(rows[0].ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if err := ix.Build(rows); err != nil {
		t.Fatalf("rebuild: %v", err)
	}
	if ix.Tombstones() != 0 || ix.Len() != len(rows) {
		t.Fatalf("after rebuild len/tombstones = %d/%d, want %d/0", ix.Len(), ix.Tombstones(), len(rows))
	}
}
