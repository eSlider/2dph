//go:build cgo && system_ladybug

package brain

import (
	"os"
	"path/filepath"
	"testing"

	lbug "github.com/LadybugDB/go-ladybug"

	"github.com/eSlider/2dph/internal/brain/rank"
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

func TestAddIsNonDestructive(t *testing.T) {
	dir := t.TempDir()
	dbpath := filepath.Join(dir, "kb.lbug")
	emb := make([]float64, EmbedDim)
	emb[0] = 0.3

	open := func() (*lbug.Database, *lbug.Connection) {
		t.Helper()
		d, c, err := OpenWritable(dbpath)
		if err != nil {
			t.Fatal(err)
		}
		if err := InitSchema(c); err != nil {
			t.Fatal(err)
		}
		return d, c
	}
	add := func(text, source string) string {
		t.Helper()
		db, conn := open()
		defer db.Close()
		defer conn.Close()
		ids, err := AddLeafs(conn, []LeafInput{{
			Text: text, Source: source, Root: "info", Confidence: "confirmed",
			Type: "reference", Embedding: emb,
		}})
		if err != nil {
			t.Fatal(err)
		}
		if len(ids) != 1 {
			t.Fatalf("ids=%v", ids)
		}
		if err := EnsureIndexes(conn); err != nil {
			t.Fatal(err)
		}
		return ids[0]
	}

	first := add("cli zebra leaf", "cli-test")
	second := add("second moose leaf", "cli-test-2")
	if first == second {
		t.Fatalf("add must produce distinct ids, got %s twice", first)
	}
	db, conn := open()
	defer db.Close()
	defer conn.Close()
	count := func(token string) int {
		t.Helper()
		stmt, err := conn.Prepare(rank.FTSStmt)
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close()
		res, err := conn.Execute(stmt, map[string]any{"q": token, "n": 5})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for res.HasNext() {
			if _, err := res.Next(); err != nil {
				t.Fatal(err)
			}
			n++
		}
		return n
	}
	if n := count("zebra"); n == 0 {
		t.Fatal("first add must be searchable")
	}
	if n := count("moose"); n == 0 {
		t.Fatal("second add must be searchable")
	}
}

func TestFactsAndChatsLandOnRebuild(t *testing.T) {
	dir := t.TempDir()
	chatDir := filepath.Join(dir, "chats")
	if err := os.MkdirAll(chatDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(chatDir, "alice.md"),
		[]byte("# Chat\n\n## Alice and Bob\n\nhello from chats fixture unique-chat-token\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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

	chats, err := LoadCorpusPath(chatDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(chats) == 0 {
		t.Fatal("chats fixture must load")
	}
	chatN, err := WriteCorpus(conn, chats, nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if chatN < 1 {
		t.Fatalf("chatN=%d", chatN)
	}
	if _, err := UpsertLeaf(conn, LeafInput{
		Text: "container 'brain' unique-fact-token is running and declared in compose.yaml",
		Source: "docker ps x compose.yaml", Root: "facts", Confidence: "confirmed",
		Type: "fact", How: "facts/extract",
	}); err != nil {
		t.Fatal(err)
	}
	if err := EnsureIndexes(conn); err != nil {
		t.Fatal(err)
	}

	count := func(token string) int {
		t.Helper()
		stmt, err := conn.Prepare(rank.FTSStmt)
		if err != nil {
			t.Fatal(err)
		}
		defer stmt.Close()
		res, err := conn.Execute(stmt, map[string]any{"q": token, "n": 5})
		if err != nil {
			t.Fatal(err)
		}
		n := 0
		for res.HasNext() {
			if _, err := res.Next(); err != nil {
				t.Fatal(err)
			}
			n++
		}
		return n
	}
	if n := count("unique-chat-token"); n == 0 {
		t.Fatal("chats markdown must be FTS-searchable")
	}
	if n := count("unique-fact-token"); n == 0 {
		t.Fatal("facts must be FTS-searchable")
	}
}
