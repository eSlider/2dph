//go:build cgo && system_ladybug

// Tombstone wiring tests (#4): ensure reconciles leafs that no longer exist in
// the DB out of the index (Delete), and rebuilds once the tombstone ratio
// crosses 10%. Offline, synthetic fixtures — no model, no network.
package brain

import (
	"path/filepath"
	"testing"

	"github.com/eSlider/2dph/internal/brain/ann"
)

// deleteFixtureLeafs removes the given leafs from the fixture DB and returns
// their content-hash ids (identical to Leaf.id, see UpsertLeaf).
func deleteFixtureLeafs(t *testing.T, dbpath string, leafs []LeafInput) []string {
	t.Helper()
	db, conn, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer conn.Close()
	ids := make([]string, 0, len(leafs))
	for _, lf := range leafs {
		id := lf.ContentHash()
		if err := execParams(conn, "MATCH (l:Leaf {id:$id}) DELETE l", map[string]any{"id": id}); err != nil {
			t.Fatalf("delete leaf %s: %v", id, err)
		}
		ids = append(ids, id)
	}
	return ids
}

// TestAnnEnsureReconcilesDeletedLeafs: after leafs disappear from the DB the
// next ensure tombstones them in the index (no rebuild — the coverage is
// fine), so Search stops ranking them and stats expose the dead fraction.
func TestAnnEnsureReconcilesDeletedLeafs(t *testing.T) {
	const n, nClusters, deadN = 200, 10, 20
	leafs := annFixtureLeafs(n, nClusters)
	dbpath := annFixtureDBLeafs(t, leafs)
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	setAnnCfg(t, true, idxPath)

	captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("initial ensure = %d, want 0", code)
		}
	})

	deadIDs := deleteFixtureLeafs(t, dbpath, leafs[:deadN])
	dead := make(map[string]bool, len(deadIDs))
	for _, id := range deadIDs {
		dead[id] = true
	}

	out := captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("reconcile ensure = %d, want 0", code)
		}
	})
	rep := parseEnsureReport(t, out)
	if rep.Rebuild {
		t.Fatalf("ensure rebuilt for %d/%d deleted leafs: %+v", deadN, n, rep)
	}
	if rep.Reconciled != deadN {
		t.Fatalf("reconciled = %d, want %d", rep.Reconciled, deadN)
	}
	if rep.Indexed != n-deadN {
		t.Fatalf("indexed = %d, want %d", rep.Indexed, n-deadN)
	}
	if rep.Tombstones != deadN {
		t.Fatalf("tombstones = %d, want %d", rep.Tombstones, deadN)
	}

	// The on-disk index keeps the tombstones (below the compaction
	// threshold) but never ranks them.
	idx, err := ann.Open(idxPath, ann.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if idx.Len() != n-deadN || idx.Tombstones() != deadN {
		t.Fatalf("reopened len/tombstones = %d/%d, want %d/%d",
			idx.Len(), idx.Tombstones(), n-deadN, deadN)
	}
	for id := range dead {
		if idx.Lookup(id) {
			t.Fatalf("deleted leaf %s still live in the index", id)
		}
	}
	hits := idx.Search(toFloat32(leafs[0].Embedding, EmbedDim), 30)
	if len(hits) == 0 {
		t.Fatal("search after reconcile returned nothing")
	}
	for _, h := range hits {
		if dead[h.ID] {
			t.Fatalf("deleted leaf %s ranked after reconcile", h.ID)
		}
	}
}

// TestAnnEnsureRebuildsAboveTombstoneRatio: once >10% of the index rows are
// deleted leafs the wave rebuilds instead of upserting, landing a snapshot
// with zero tombstones and only live vectors.
func TestAnnEnsureRebuildsAboveTombstoneRatio(t *testing.T) {
	const n, nClusters, deadN = 200, 10, 120 // 60% dead
	leafs := annFixtureLeafs(n, nClusters)
	dbpath := annFixtureDBLeafs(t, leafs)
	idxPath := filepath.Join(filepath.Dir(dbpath), "vector.ann")
	setAnnCfg(t, true, idxPath)

	captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("initial ensure = %d, want 0", code)
		}
	})
	deleteFixtureLeafs(t, dbpath, leafs[:deadN])

	out := captureToolStdout(t, func() {
		if code := runAnnEnsure([]string{"--db", dbpath, "--index", idxPath, "--json"}); code != 0 {
			t.Fatalf("tombstone ensure = %d, want 0", code)
		}
	})
	rep := parseEnsureReport(t, out)
	if rep.Action != "build" || !rep.Rebuild {
		t.Fatalf("tombstone-heavy ensure = %+v, want action=build rebuild=true", rep)
	}
	if rep.Indexed != n-deadN {
		t.Fatalf("indexed = %d, want %d live", rep.Indexed, n-deadN)
	}
	if rep.Tombstones != 0 {
		t.Fatalf("tombstones after rebuild = %d, want 0", rep.Tombstones)
	}

	idx, err := ann.Open(idxPath, ann.Defaults())
	if err != nil {
		t.Fatal(err)
	}
	defer idx.Close()
	if idx.Len() != n-deadN || idx.Tombstones() != 0 {
		t.Fatalf("rebuilt len/tombstones = %d/%d, want %d/0", idx.Len(), idx.Tombstones(), n-deadN)
	}
}
