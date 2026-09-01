//go:build cgo && system_ladybug

package brain

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	lbug "github.com/LadybugDB/go-ladybug"

	"github.com/eSlider/2dph/internal/brain/rank"
	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/internal/corpus"
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
	if len(ids) != 1 || len(ids[0]) != 32 {
		t.Fatalf("ids=%v (want 32-hex ContentHash id)", ids)
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

	var chats []contract.Leaf
	if err := (corpus.Chats{Dir: chatDir}).Stream(context.Background(), func(l contract.Leaf) error {
		chats = append(chats, l)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(chats) == 0 {
		t.Fatal("chats fixture must load")
	}
	chatN, err := WriteCorpus(conn, chats, nil, WriteOptions{Workers: 2, Batch: 8})
	if err != nil {
		t.Fatal(err)
	}
	if chatN < 1 {
		t.Fatalf("chatN=%d", chatN)
	}
	if _, err := UpsertLeaf(conn, LeafInput{
		Text:   "container 'brain' unique-fact-token is running and declared in compose.yaml",
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

// Контракт P-9.2: observed_at из источника пишется как есть, пусто → now().
// external_id сохраняется в колонке.
func TestContractObservedAtPassthrough(t *testing.T) {
	dir := t.TempDir()
	db, conn, err := OpenWritable(filepath.Join(dir, "kb.lbug"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer conn.Close()
	if err := InitSchema(conn); err != nil {
		t.Fatal(err)
	}

	// leaf с observed_at/external_id из источника
	id, err := UpsertLeaf(conn, LeafInput{
		Text: "alice contract leaf", Source: "test-contract.md",
		ExternalID: "msg-42", ObservedAt: "2026-08-31T09:15:00Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	// leaf без observed_at → штамп now()
	if _, err := UpsertLeaf(conn, LeafInput{
		Text: "bob plain leaf", Source: "test-contract.md", ExternalID: "msg-43",
	}); err != nil {
		t.Fatal(err)
	}

	got := map[string]string{}
	res, err := conn.Query("MATCH (l:Leaf) RETURN l.id, l.observed_at, l.external_id")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			t.Fatal(err)
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 3 {
			t.Fatal("leaf row")
		}
		got[fmt.Sprint(vals[0])] = fmt.Sprint(vals[1]) + "|" + fmt.Sprint(vals[2])
	}
	wantObs := "2026-08-31T09:15:00Z|msg-42"
	if got[id] != wantObs {
		t.Fatalf("alice leaf observed_at|external_id = %q, want %q", got[id], wantObs)
	}
	// bob: observed_at заполнен now(), external_id на месте
	for lid, v := range got {
		if lid == id {
			continue
		}
		if v == "|msg-43" || v == "<nil>|msg-43" {
			t.Fatalf("bob leaf observed_at must fall back to now(), got %q", v)
		}
		if !strings.Contains(v, "msg-43") {
			t.Fatalf("bob leaf external_id missing: %q", v)
		}
	}
}

// /ingest принимает external_id/observed_at из запроса (контракт P-9.2).
func TestParseIngestLeafsContractFields(t *testing.T) {
	leafs, err := parseIngestLeafs([]byte(`{"text":"hi","source":"mail","external_id":"msg-7","observed_at":"2026-08-31T09:15:00Z"}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(leafs) != 1 {
		t.Fatalf("leafs=%d", len(leafs))
	}
	if leafs[0].ExternalID != "msg-7" || leafs[0].ObservedAt != "2026-08-31T09:15:00Z" {
		t.Fatalf("contract fields lost: %+v", leafs[0])
	}
}

// openTestDB открывает свежую БД со схемой в t.TempDir().
func openTestDB(t *testing.T) (*lbug.Database, *lbug.Connection) {
	t.Helper()
	dir := t.TempDir()
	db, conn, err := OpenWritable(filepath.Join(dir, "kb.lbug"))
	if err != nil {
		t.Fatal(err)
	}
	if err := InitSchema(conn); err != nil {
		t.Fatal(err)
	}
	return db, conn
}

func dbLeafCount(t *testing.T, conn *lbug.Connection) int {
	t.Helper()
	res, err := conn.Query("MATCH (l:Leaf) RETURN count(l)")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Close()
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			t.Fatal(err)
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 1 {
			t.Fatal("count row")
		}
		n, err := strconv.Atoi(fmt.Sprint(vals[0]))
		if err != nil {
			t.Fatalf("count value %q: %v", vals[0], err)
		}
		return n
	}
	return 0
}

// issue #237: resume-набор (Existing) собирается один раз на старте и
// переиспользуется между чанками — второй чанк с дубликатами дописывает ровно
// недостающее, без пересборки набора на чанк.
func TestWriteCorpusResumeExistingSetAcrossChunks(t *testing.T) {
	db, conn := openTestDB(t)
	defer db.Close()
	defer conn.Close()

	leafs := mkTestLeafs(6, "r")
	n1, err := WriteCorpus(conn, leafs[:4], nil, WriteOptions{Workers: 2, Batch: 2})
	if err != nil {
		t.Fatal(err)
	}
	if n1 != 4 {
		t.Fatalf("chunk1 wrote %d, want 4", n1)
	}
	// Existing собран один раз, до чанка 2 (как в index-драйвере на старте)
	existing, err := ExistingLeafIDSet(conn)
	if err != nil {
		t.Fatal(err)
	}
	if len(existing) != 4 {
		t.Fatalf("ExistingLeafIDSet size = %d, want 4", len(existing))
	}
	// чанк 2: leafs 2,3 — дубликаты чанка 1, leafs 4,5 — новые
	n2, err := WriteCorpus(conn, leafs[2:], nil, WriteOptions{Workers: 2, Batch: 2, Skip: true, Existing: existing})
	if err != nil {
		t.Fatal(err)
	}
	if n2 != 2 {
		t.Fatalf("chunk2 wrote %d, want 2", n2)
	}
	if got := dbLeafCount(t, conn); got != 6 {
		t.Fatalf("db leaf count = %d, want 6", got)
	}
}

// issue #237: WriteCorpus без Existing, но со Skip — совместимость: набор
// собирается внутри (старый путь single-shot вызова).
func TestWriteCorpusResumeCollectsSetInternally(t *testing.T) {
	db, conn := openTestDB(t)
	defer db.Close()
	defer conn.Close()

	leafs := mkTestLeafs(3, "c")
	if _, err := WriteCorpus(conn, leafs, nil, WriteOptions{Workers: 2, Batch: 2}); err != nil {
		t.Fatal(err)
	}
	// повторная запись тех же leafs с Skip и Existing=nil → всё отфильтровано
	n, err := WriteCorpus(conn, leafs, nil, WriteOptions{Workers: 2, Batch: 2, Skip: true})
	if err != nil {
		t.Fatal(err)
	}
	if n != 0 {
		t.Fatalf("resume wrote %d, want 0", n)
	}
	if got := dbLeafCount(t, conn); got != 3 {
		t.Fatalf("db leaf count = %d, want 3", got)
	}
}

// issue #237: сквозной прогресс — done не сбрасывается на чанк, total общий.
func TestWriteCorpusChunkedProgressCumulative(t *testing.T) {
	db, conn := openTestDB(t)
	defer db.Close()
	defer conn.Close()

	r := NewProgressReporter(io.Discard, time.Hour)
	leafs := mkTestLeafs(5, "p")
	// чанк 1: base=0, total=5; чанк 2: base=3, total=5 (как WriteCorpusChunked)
	if _, err := WriteCorpus(conn, leafs[:3], nil, WriteOptions{
		Workers: 2, Batch: 1, Progress: r, ProgressDone: 0, ProgressTotal: 5,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCorpus(conn, leafs[3:], nil, WriteOptions{
		Workers: 2, Batch: 1, Progress: r, ProgressDone: 3, ProgressTotal: 5,
	}); err != nil {
		t.Fatal(err)
	}
	if got := r.done.Load(); got != 5 {
		t.Errorf("done = %d, want 5 (сквозной счётчик)", got)
	}
	if got := r.total.Load(); got != 5 {
		t.Errorf("total = %d, want 5", got)
	}
}

// issue #237: запись чанками даёт тот же результат, что и запись целиком
// (детерминированные id = ContentHash, число и состав leafs совпадают).
func TestWriteCorpusChunkedEqualsWhole(t *testing.T) {
	db, conn := openTestDB(t)
	defer db.Close()
	defer conn.Close()

	leafs := mkTestLeafs(10, "w")
	if _, err := WriteCorpus(conn, leafs, nil, WriteOptions{Workers: 2, Batch: 3}); err != nil {
		t.Fatal(err)
	}
	want := dbLeafCount(t, conn)
	if want != 10 {
		t.Fatalf("whole write count = %d, want 10", want)
	}

	db2, conn2 := openTestDB(t)
	defer db2.Close()
	defer conn2.Close()
	stats := CorpusStats{Total: len(leafs), BySource: map[string]int{"test": len(leafs)}}
	n, err := WriteCorpusChunked(context.Background(), []contract.Source{fakeSource{name: "test", leafs: leafs}}, 4, 0, stats,
		func(chunk []contract.Leaf, base, total int) (int, error) {
			return WriteCorpus(conn2, chunk, nil, WriteOptions{
				Workers: 2, Batch: 2, ProgressDone: base, ProgressTotal: total,
			})
		})
	if err != nil {
		t.Fatal(err)
	}
	if n != 10 {
		t.Fatalf("chunked write = %d, want 10", n)
	}
	if got := dbLeafCount(t, conn2); got != want {
		t.Fatalf("chunked count = %d, want %d", got, want)
	}
}
