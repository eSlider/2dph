package source

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// page models one Fetch response of a sequential backend: when the driver asks
// with cursor `from`, the source yields `ids` and advances to `next`.
type page struct {
	from Cursor
	ids  []string
	next Cursor
}

// fakeSrc is a deterministic Source over a fixed page list. Fetch with an
// unknown cursor (already advanced past all pages) returns no data.
type fakeSrc struct {
	name  string
	pages []page
}

func (f *fakeSrc) Name() string { return f.name }

func (f *fakeSrc) Fetch(_ context.Context, cursor Cursor) ([]Blob, Cursor, error) {
	for _, p := range f.pages {
		if p.from == cursor {
			blobs := make([]Blob, len(p.ids))
			for i, id := range p.ids {
				blobs[i] = Blob{ID: id}
			}
			return blobs, p.next, nil
		}
	}
	return nil, "", nil
}

func ctx() context.Context { return context.Background() }

func handleCollect(handled *[]string) func(context.Context, Blob) error {
	return func(_ context.Context, b Blob) error {
		*handled = append(*handled, b.ID)
		return nil
	}
}

// Acceptance #1: повторный Fetch без новых данных возвращает 0 blob.
func TestSyncIdempotentRepeat(t *testing.T) {
	src := &fakeSrc{name: "fake", pages: []page{{from: "", ids: []string{"a", "b"}, next: "c1"}}}
	state := filepath.Join(t.TempDir(), "fake.json")

	var handled []string
	st1, err := Sync(ctx(), src, handleCollect(&handled), Options{StatePath: state})
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if st1.New != 2 || st1.Skipped != 0 {
		t.Fatalf("first stats = %+v, want New=2 Skipped=0", st1)
	}

	// Second run over the same source + state: cursor advanced past the batch,
	// so nothing new is emitted.
	st2, err := Sync(ctx(), src, handleCollect(&handled), Options{StatePath: state})
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if st2.New != 0 {
		t.Fatalf("repeat produced %d new blobs, want 0 (st=%+v)", st2.New, st2)
	}
	if got := len(handled); got != 2 {
		t.Fatalf("handled %d items, want exactly 2 (each once)", got)
	}
}

// Acceptance #2: сбой середины батча возобновляется с чекпойнта, уже
// обработанные элементы не переобрабатываются.
func TestSyncMidBatchFailureResumesFromCheckpoint(t *testing.T) {
	src := &fakeSrc{name: "fake", pages: []page{{from: "", ids: []string{"a", "b", "c"}, next: "c1"}}}
	state := filepath.Join(t.TempDir(), "fake.json")

	calls := map[string]int{}
	failOnce := true // a transient failure: only the first encounter of b fails
	handle := func(_ context.Context, b Blob) error {
		calls[b.ID]++
		if b.ID == "b" && failOnce {
			failOnce = false
			return errors.New("boom")
		}
		return nil
	}

	if _, err := Sync(ctx(), src, handle, Options{StatePath: state}); err == nil {
		t.Fatal("expected mid-batch failure")
	}
	if calls["a"] != 1 || calls["b"] != 1 || calls["c"] != 0 {
		t.Fatalf("calls after failure = %v, want a=1 b=1 c=0", calls)
	}

	// Resume: a must NOT be re-processed, b and c complete.
	st, err := Sync(ctx(), src, handle, Options{StatePath: state})
	if err != nil {
		t.Fatalf("resume sync: %v", err)
	}
	if calls["a"] != 1 {
		t.Fatalf("a re-processed after resume: %v", calls)
	}
	if calls["b"] != 2 {
		t.Fatalf("b not reprocessed after resume: %v", calls)
	}
	if calls["c"] != 1 {
		t.Fatalf("c not processed after resume: %v", calls)
	}
	if st.New != 2 || st.Skipped != 1 {
		t.Fatalf("resume stats = %+v, want New=2 Skipped=1", st)
	}
}

// Acceptance #3: чекпойнт пишется атомарно — корректный JSON, никаких
// временных файлов не остаётся.
func TestCheckpointWrittenAtomically(t *testing.T) {
	dir := t.TempDir()
	state := filepath.Join(dir, "fake.json")
	src := &fakeSrc{name: "fake", pages: []page{{from: "", ids: []string{"a", "b"}, next: "c1"}}}

	var handled []string
	if _, err := Sync(ctx(), src, handleCollect(&handled), Options{StatePath: state}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	b, err := os.ReadFile(state)
	if err != nil {
		t.Fatalf("read checkpoint: %v", err)
	}
	var cp checkpoint
	if err := json.Unmarshal(b, &cp); err != nil {
		t.Fatalf("checkpoint is not valid JSON: %v\n%s", err, b)
	}
	if cp.Cursor != "c1" {
		t.Fatalf("cursor = %q, want c1", cp.Cursor)
	}
	sort.Strings(cp.Seen)
	wantSeen := []string{hashID("a"), hashID("b")}
	sort.Strings(wantSeen)
	if !reflect.DeepEqual(cp.Seen, wantSeen) {
		t.Fatalf("seen = %v, want %v (sha256 ids)", cp.Seen, wantSeen)
	}

	// No temp files left behind by the atomic write.
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Fatalf("temp file left behind: %s", e.Name())
		}
	}
}
