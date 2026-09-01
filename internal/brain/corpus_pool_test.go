package brain

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/contract"
)

func embedStr(i int, text string) string { return text }

func TestParallelEmbedAllPreservesOrder(t *testing.T) {
	items := make([]poolItem, 50)
	for i := range items {
		items[i] = poolItem{i: i, text: fmt.Sprintf("text-%d", i)}
	}
	embed := func(s string) ([]float64, error) {
		return []float64{float64(len(s))}, nil
	}
	res, err := parallelEmbed(context.Background(), items, embed, 8, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(res) != len(items) {
		t.Fatalf("got %d results, want %d", len(res), len(items))
	}
	for i := range res {
		if res[i].i != i {
			t.Fatalf("result[%d].i = %d (order broken)", i, res[i].i)
		}
		if res[i].err != nil || len(res[i].emb) != 1 || res[i].emb[0] != float64(len(items[i].text)) {
			t.Fatalf("result[%d] = %+v", i, res[i])
		}
	}
}

func TestParallelEmbedHonorsWorkers(t *testing.T) {
	var (
		mu        sync.Mutex
		cur, maxc int
		want      = 4
		iters     = int64(0)
	)
	items := make([]poolItem, 200)
	for i := range items {
		items[i] = poolItem{i: i, text: "x"}
	}
	embed := func(string) ([]float64, error) {
		mu.Lock()
		cur++
		if cur > maxc {
			maxc = cur
		}
		atomic.AddInt64(&iters, 1)
		mu.Unlock()
		time.Sleep(time.Millisecond)
		mu.Lock()
		cur--
		mu.Unlock()
		return []float64{1}, nil
	}
	if _, err := parallelEmbed(context.Background(), items, embed, want, nil); err != nil {
		t.Fatalf("err: %v", err)
	}
	if maxc > want {
		t.Errorf("max concurrency %d > workers %d", maxc, want)
	}
	if iters != int64(len(items)) {
		t.Errorf("embedded %d items, want %d", iters, len(items))
	}
}

func TestParallelEmbedContextCancel(t *testing.T) {
	items := make([]poolItem, 5000)
	for i := range items {
		items[i] = poolItem{i: i, text: "y"}
	}
	ctx, cancel := context.WithCancel(context.Background())
	embed := func(string) ([]float64, error) {
		time.Sleep(2 * time.Millisecond)
		return []float64{1}, nil
	}
	go func() {
		time.Sleep(5 * time.Millisecond)
		cancel()
	}()
	_, err := parallelEmbed(ctx, items, embed, 8, nil)
	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestParallelEmbedPropagatesError(t *testing.T) {
	items := []poolItem{{i: 0, text: "a"}, {i: 1, text: "b"}, {i: 2, text: "c"}}
	boom := errors.New("boom")
	embed := func(s string) ([]float64, error) {
		if s == "b" {
			return nil, boom
		}
		return []float64{1}, nil
	}
	res, err := parallelEmbed(context.Background(), items, embed, 2, nil)
	if err != nil {
		t.Fatalf("embed errors are collected per-result, got top-level err %v", err)
	}
	if !errors.Is(res[1].err, boom) {
		t.Errorf("result[1].err = %v, want boom", res[1].err)
	}
	if res[0].err != nil || res[2].err != nil {
		t.Errorf("results 0/2 should have no error: %+v", res)
	}
}

func TestParallelEmbedProgress(t *testing.T) {
	items := make([]poolItem, 20)
	for i := range items {
		items[i] = poolItem{i: i, text: "t"}
	}
	var lastDone int64
	embed := func(string) ([]float64, error) { return []float64{1}, nil }
	progress := func(done, total int) {
		if total != len(items) {
			t.Errorf("progress total = %d, want %d", total, len(items))
		}
		atomic.StoreInt64(&lastDone, int64(done))
	}
	if _, err := parallelEmbed(context.Background(), items, embed, 4, progress); err != nil {
		t.Fatalf("err: %v", err)
	}
	if lastDone != int64(len(items)) {
		t.Errorf("progress last done = %d, want %d", lastDone, len(items))
	}
}

func TestParallelEmbedEmpty(t *testing.T) {
	res, err := parallelEmbed(context.Background(), nil, func(string) ([]float64, error) { return nil, nil }, 4, nil)
	if err != nil || len(res) != 0 {
		t.Fatalf("empty: res=%v err=%v", res, err)
	}
}

func TestFilterExistingLeafs(t *testing.T) {
	leafs := []contract.Leaf{
		{Source: "docs", ExternalID: "a.md", Kind: "reference", Text: "h1\n\nt1"},
		{Source: "docs", ExternalID: "b.md", Kind: "reference", Text: "h2\n\nt2"},
		{Source: "docs", ExternalID: "c.md", Kind: "reference", Text: "h3\n\nt3"},
	}
	// Present: id of leaf 0 only. Also present with a source that differs in
	// case so normalization matters.
	existing := map[string]bool{
		leafs[0].ContentHash(): true,
		leafs[1].ContentHash(): true,
	}
	got := filterExistingLeafs(leafs, existing)
	if len(got) != 1 || got[0].ExternalID != "c.md" {
		t.Fatalf("filterExistingLeafs kept %+v, want only c.md", got)
	}
	// Empty existing -> keep all.
	if all := filterExistingLeafs(leafs, map[string]bool{}); len(all) != 3 {
		t.Fatalf("empty existing should keep all, got %d", len(all))
	}
	// All present -> keep none.
	allSet := map[string]bool{}
	for _, lf := range leafs {
		allSet[lf.ContentHash()] = true
	}
	if none := filterExistingLeafs(leafs, allSet); len(none) != 0 {
		t.Fatalf("all present should keep none, got %d", len(none))
	}
}

// P-9.3 #5.3: filterExistingLeafs нормализует текст так же, как writer —
// leaf с CRLF/хвостовыми пробелами совпадает по id с уже записанным.
func TestFilterExistingLeafsNormalizesText(t *testing.T) {
	base := contract.Leaf{Source: "mail", ExternalID: "msg-1", Kind: "mail", Text: "subject\n\nbody"}
	withCRLF := contract.Leaf{Source: "mail", ExternalID: "msg-1", Kind: "mail", Text: "subject\r\n\r\nbody  "}
	if base.ContentHash() != withCRLF.ContentHash() {
		t.Fatal("normalization must unify text before ContentHash")
	}
	got := filterExistingLeafs([]contract.Leaf{withCRLF}, map[string]bool{base.ContentHash(): true})
	if len(got) != 0 {
		t.Fatalf("normalized duplicate must be filtered, kept %+v", got)
	}
}

func TestChunkBounds(t *testing.T) {
	cases := []struct {
		n, size int
		want    [][2]int
	}{
		{0, 64, nil},
		{5, 64, [][2]int{{0, 5}}},
		{100, 64, [][2]int{{0, 64}, {64, 100}}},
		{128, 64, [][2]int{{0, 64}, {64, 128}}},
		{130, 64, [][2]int{{0, 64}, {64, 128}, {128, 130}}},
		{10, 0, [][2]int{{0, 10}}}, // default batch when size<=0
	}
	for _, c := range cases {
		got := chunkBounds(c.n, c.size)
		if fmt.Sprint(got) != fmt.Sprint(c.want) {
			t.Errorf("chunkBounds(%d,%d) = %v, want %v", c.n, c.size, got, c.want)
		}
	}
}
