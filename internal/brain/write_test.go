//go:build cgo && system_ladybug

package brain

import (
	"path/filepath"
	"testing"
)

func TestAddLeafsAndIndexes(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "kb.lbug")
	db, conn, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer conn.Close()
	if err := InitSchema(conn); err != nil {
		t.Fatal(err)
	}
	emb := make([]float64, EmbedDim)
	emb[0] = 0.5
	ids, err := AddLeafs(conn, []LeafInput{{
		Text: "fox leaf for FTS", Source: "test.md", Root: "info",
		Type: "reference", Embedding: emb,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || len(ids[0]) != 24 {
		t.Fatalf("ids=%v", ids)
	}
	if _, err := LinkFromFile(conn, ids[0], "test.md", "repo", ""); err != nil {
		t.Fatal(err)
	}
	if err := EnsureIndexes(conn); err != nil {
		t.Fatal(err)
	}
	// second ensure is idempotent
	if err := EnsureIndexes(conn); err != nil {
		t.Fatal(err)
	}
	ids2, err := AddLeafs(conn, []LeafInput{{
		Text: "second leaf after indexes", Source: "b.md x a.md", Root: "facts",
		Confidence: "confirmed", Type: "fact", Embedding: emb,
	}})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids2) != 1 {
		t.Fatalf("ids2=%v", ids2)
	}
}
