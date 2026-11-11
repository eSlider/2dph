package ann

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// filterRows returns n rows: n-1 info rows in one tight cluster around e0 plus
// one facts row in a second cluster around e1, so "root == facts" is a rare
// slice exactly like the live corpus (39 facts in 43k leafs).
func filterRows(n int) ([]Row, []float32) {
	rows := make([]Row, 0, n)
	for i := 0; i < n-1; i++ {
		v := make([]float32, 256)
		v[0] = 1
		v[8+(i%16)] = float32(i%7) * 0.001
		rows = append(rows, Row{ID: "info-" + itoa(i), Vec: v, Root: "info",
			Source: "docs/info-" + itoa(i) + ".md", ValidFrom: "", ValidTo: ""})
	}
	fact := make([]float32, 256)
	fact[1] = 1
	rows = append(rows, Row{ID: "fact-1", Vec: fact, Root: "facts",
		Source:    "office-assistant/AGENTS.md x 2dph/AGENTS.md",
		ValidFrom: "2025-01-01", ValidTo: "2025-12-31"})

	q := make([]float32, 256)
	q[0] = 1
	return rows, q
}

func rootIs(root string) func(Row) bool {
	return func(r Row) bool { return r.Root == root }
}

// TestSearchFilteredHitsRareRoot — an unfiltered top-1 is an info leaf; the
// filtered search returns the facts leaf that the candidate window would drop
// (the defect of issue #3, at the index level).
func TestSearchFilteredHitsRareRoot(t *testing.T) {
	rows, q := filterRows(200)
	ix := New(Params{RngSeed: 3, NList: 4, NProbe: 4})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	top := ix.Search(q, 1)
	if len(top) != 1 || top[0].ID != "info-0" {
		t.Fatalf("unfiltered top-1 = %v, want the info leaf (fixture sanity)", top)
	}
	got := ix.SearchFiltered(float64Vec(q), 1, rootIs("facts"))
	if len(got) != 1 || got[0].ID != "fact-1" {
		t.Fatalf("filtered top-1 = %v, want fact-1", got)
	}
}

// TestSearchFilteredEscalatesProbeSet — with NProbe=1 only one cell is probed
// by default; a rare root in a far cell must widen the probe set until it is
// covered (the in-index form of the exact-scan fallback). pred sees every row
// exactly once, so the call count proves the escalation reached all 200 rows.
func TestSearchFilteredEscalatesProbeSet(t *testing.T) {
	rows, q := filterRows(200)
	ix := New(Params{RngSeed: 3, NList: 4, NProbe: 1})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	// Sanity: NProbe=1 really restricts the unfiltered search to one cell.
	if got := ix.Search(q, 200); len(got) >= len(rows) {
		t.Fatalf("unfiltered search returned all %d rows, NProbe=1 not applied", len(got))
	}
	seen := 0
	got := ix.SearchFiltered(float64Vec(q), 1, func(r Row) bool {
		seen++
		return r.Root == "facts"
	})
	if len(got) != 1 || got[0].ID != "fact-1" {
		t.Fatalf("filtered top-1 = %v, want fact-1 (probe escalation)", got)
	}
	if seen != len(rows) {
		t.Fatalf("pred saw %d rows, want all %d (probe set must widen)", seen, len(rows))
	}
}

// TestSearchFilteredNoMatchTerminates — a filter that matches nothing must end
// with an empty result once every cell has been probed, not spin.
func TestSearchFilteredNoMatchTerminates(t *testing.T) {
	rows, q := filterRows(120)
	ix := New(Params{RngSeed: 5, NList: 4, NProbe: 1})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	got := ix.SearchFiltered(float64Vec(q), 5, rootIs("nothing"))
	if len(got) != 0 {
		t.Fatalf("filtered search for an absent root = %v, want empty", got)
	}
}

// TestSearchUnfilteredUnaffected — nil pred mirrors Search64, so the #204
// unfiltered recall gate keeps measuring the same candidate set.
func TestSearchUnfilteredUnaffected(t *testing.T) {
	rows, q := filterRows(120)
	ix := New(Params{RngSeed: 7, NList: 4, NProbe: 2})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	want := ix.Search64(float64Vec(q), 10)
	got := ix.Search(q, 10)
	if len(got) != len(want) {
		t.Fatalf("Search = %d hits, Search64 = %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("hit %d: Search %+v != Search64 %+v", i, got[i], want[i])
		}
	}
}

// TestSnapshotV2KeepsFilterMetadata — the filter fields survive save/open, so
// a restarted serve filters without a graph point lookup.
func TestSnapshotV2KeepsFilterMetadata(t *testing.T) {
	rows, q := filterRows(150)
	ix := New(Params{RngSeed: 11, NList: 4, NProbe: 4})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	path := filepath.Join(t.TempDir(), "vector.ann")
	if err := ix.SaveTo(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	ix2, err := Open(path, Params{RngSeed: 11, NList: 4, NProbe: 4})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer ix2.Close()
	got := ix2.SearchFiltered(float64Vec(q), 1, func(r Row) bool {
		return r.Root == "facts" && r.Source == "office-assistant/AGENTS.md x 2dph/AGENTS.md" &&
			r.ValidFrom == "2025-01-01" && r.ValidTo == "2025-12-31"
	})
	if len(got) != 1 || got[0].ID != "fact-1" {
		t.Fatalf("metadata lost across the snapshot: %v", got)
	}
	if err := ix2.Upsert([]Row{{ID: "facts-2", Vec: q, Root: "facts", Source: "2dph/AGENTS.md"}}); err != nil {
		t.Fatalf("upsert: %v", err)
	}
	if got := ix2.SearchFiltered(float64Vec(q), 2, rootIs("facts")); len(got) != 2 {
		t.Fatalf("upserted row not filtered in: %v", got)
	}
}

// TestSnapshotV1Rejected — the format bump (issue #3): a v1 snapshot carries no
// filter metadata. Reading it as v2 would mis-place every row boundary, so Open
// must fail with ErrStaleFormat and let the caller rebuild or fall back.
func TestSnapshotV1Rejected(t *testing.T) {
	path := filepath.Join(t.TempDir(), "vector.ann")
	var buf bytes.Buffer
	buf.WriteString(snapshotMagicV1)
	hdr := [3]int32{int32(Defaults().Dim), 1, int32(Defaults().NProbe)}
	if err := binary.Write(&buf, binary.LittleEndian, &hdr); err != nil {
		t.Fatal(err)
	}
	buf.Write(make([]byte, int(hdr[0])*4)) // one centroid
	buf.Write(make([]byte, 4))             // one empty cell
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Open(path, Defaults())
	if err == nil {
		t.Fatal("v1 snapshot must be rejected")
	}
	if !errors.Is(err, ErrStaleFormat) {
		t.Fatalf("err = %v, want ErrStaleFormat", err)
	}
}

// TestWALV1RowsReplayWithoutMetadata — WAL lines written before the format bump
// stay readable (JSON, optional fields): they replay as legacy rows with an
// empty root, matching no root filter and staying as-of eligible (D24).
func TestWALV1RowsReplayWithoutMetadata(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vector.ann")
	rows, _ := filterRows(20)
	ix := New(Params{RngSeed: 13, NList: 4, NProbe: 4})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	if err := ix.SaveTo(path); err != nil {
		t.Fatalf("save: %v", err)
	}
	vec := make([]float32, 256)
	vec[2] = 1
	line, err := json.Marshal(map[string]any{"ID": "legacy-1", "Vec": vec}) // v1 shape
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path+".wal", append(line, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	ix2, err := Open(path, Params{RngSeed: 13, NList: 4, NProbe: 4})
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer ix2.Close()
	if !ix2.Lookup("legacy-1") {
		t.Fatal("v1 WAL row not replayed")
	}
	if got := ix2.SearchFiltered(float64Vec(vec), 1, func(r Row) bool {
		return r.ID == "legacy-1" && r.Root == "" && r.Source == ""
	}); len(got) != 1 {
		t.Fatal("v1 WAL row must replay with empty metadata")
	}
}
