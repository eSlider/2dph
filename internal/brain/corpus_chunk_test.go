package brain

import (
	"context"
	"fmt"
	"io"
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/contract"
)

// fakeSource — детерминированный источник для тестов чанкования (cgo-free).
type fakeSource struct {
	name  string
	leafs []contract.Leaf
}

func (f fakeSource) Name() string { return f.name }

func (f fakeSource) Stream(ctx context.Context, emit func(contract.Leaf) error) error {
	for _, l := range f.leafs {
		if err := emit(l); err != nil {
			return err
		}
	}
	return ctx.Err()
}

func mkTestLeafs(n int, prefix string) []contract.Leaf {
	out := make([]contract.Leaf, 0, n)
	for i := 0; i < n; i++ {
		out = append(out, contract.Leaf{
			Source: "test", ExternalID: fmt.Sprintf("%s-%d", prefix, i),
			Kind: "reference", Text: fmt.Sprintf("%s leaf %d", prefix, i),
		})
	}
	return out
}

func TestCountCorpus(t *testing.T) {
	sources := []contract.Source{
		fakeSource{name: "docs", leafs: mkTestLeafs(10, "d")},
		fakeSource{name: "mail", leafs: mkTestLeafs(5, "m")},
	}
	stats, err := CountCorpus(context.Background(), sources, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 15 {
		t.Errorf("Total = %d, want 15", stats.Total)
	}
	if stats.BySource["docs"] != 10 || stats.BySource["mail"] != 5 {
		t.Errorf("BySource = %v, want docs=10 mail=5", stats.BySource)
	}
}

func TestCountCorpusEmpty(t *testing.T) {
	stats, err := CountCorpus(context.Background(), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 0 || len(stats.BySource) != 0 {
		t.Errorf("empty corpus: stats = %+v", stats)
	}
}

// TestWriteCorpusChunkedOrder — потоковое разбиение на чанки: порядок
// источников и leafs сохраняется, чанки режутся по size, base накапливается.
func TestWriteCorpusChunkedOrder(t *testing.T) {
	sources := []contract.Source{
		fakeSource{name: "docs", leafs: mkTestLeafs(25, "d")},
		fakeSource{name: "mail", leafs: mkTestLeafs(25, "m")},
	}
	stats, err := CountCorpus(context.Background(), sources, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	var bases []int
	written, err := WriteCorpusChunked(context.Background(), sources, 20, 0, stats,
		func(chunk []contract.Leaf, base, total int) (int, error) {
			bases = append(bases, base)
			if total != 50 {
				t.Errorf("chunk total = %d, want 50", total)
			}
			for _, l := range chunk {
				got = append(got, l.ExternalID)
			}
			return len(chunk), nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if written != 50 {
		t.Errorf("written = %d, want 50", written)
	}
	if fmt.Sprint(bases) != fmt.Sprint([]int{0, 20, 40}) {
		t.Errorf("bases = %v, want [0 20 40]", bases)
	}
	if len(got) != 50 {
		t.Fatalf("got %d leafs, want 50", len(got))
	}
	// порядок: d-0..d-24 затем m-0..m-24
	for i := 0; i < 25; i++ {
		if got[i] != fmt.Sprintf("d-%d", i) {
			t.Errorf("got[%d] = %s, want d-%d", i, got[i], i)
		}
		if got[25+i] != fmt.Sprintf("m-%d", i) {
			t.Errorf("got[%d] = %s, want m-%d", 25+i, got[25+i], i)
		}
	}
}

// TestWriteCorpusChunkedLimit — --limit: write получает не больше limit leafs
// (лимит применяется до чанков, как сейчас через opt.Limit).
func TestWriteCorpusChunkedLimit(t *testing.T) {
	sources := []contract.Source{fakeSource{name: "docs", leafs: mkTestLeafs(100, "d")}}
	stats, err := CountCorpus(context.Background(), sources, 0)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	var chunkSizes []int
	written, err := WriteCorpusChunked(context.Background(), sources, 10, 25, stats,
		func(chunk []contract.Leaf, base, total int) (int, error) {
			chunkSizes = append(chunkSizes, len(chunk))
			for _, l := range chunk {
				got = append(got, l.ExternalID)
			}
			return len(chunk), nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if written != 25 {
		t.Errorf("written = %d, want 25", written)
	}
	if fmt.Sprint(chunkSizes) != fmt.Sprint([]int{10, 10, 5}) {
		t.Errorf("chunkSizes = %v, want [10 10 5]", chunkSizes)
	}
	if got[0] != "d-0" || got[24] != "d-24" {
		t.Errorf("limit нарушает порядок: got[0]=%s got[24]=%s", got[0], got[24])
	}
}

// TestWriteCorpusChunkedExistingAcrossChunks — resume: один existing-set,
// собранный на старте, фильтрует каждый чанк; дубликаты из предыдущих чанков
// не пишутся повторно (dedup через границу чанков), порядок сохраняется.
func TestWriteCorpusChunkedExistingAcrossChunks(t *testing.T) {
	leafs := mkTestLeafs(10, "d")
	// дубликаты leafs 2 и 5 (тот же ContentHash) в конце
	leafs = append(leafs, leafs[2], leafs[5])
	sources := []contract.Source{fakeSource{name: "docs", leafs: leafs}}
	stats, err := CountCorpus(context.Background(), sources, 0)
	if err != nil {
		t.Fatal(err)
	}
	// existing собран один раз (как ExistingLeafIDSet на старте), до чанков
	existing := map[string]bool{leafs[0].ContentHash(): true, leafs[1].ContentHash(): true}
	var got []string
	written, err := WriteCorpusChunked(context.Background(), sources, 4, 0, stats,
		func(chunk []contract.Leaf, base, total int) (int, error) {
			kept := filterExistingLeafs(chunk, existing)
			// resume-семантика: записанное пополняет existing
			for _, l := range kept {
				existing[l.ContentHash()] = true
			}
			for _, l := range kept {
				got = append(got, l.ExternalID)
			}
			return len(kept), nil
		})
	if err != nil {
		t.Fatal(err)
	}
	// 12 leafs всего, 2 уже в existing, 2 дубликата внутри корпуса → 8 новых
	if written != 8 {
		t.Errorf("written = %d, want 8", written)
	}
	want := []string{"d-2", "d-3", "d-4", "d-5", "d-6", "d-7", "d-8", "d-9"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("kept order = %v, want %v", got, want)
	}
}

// TestWriteCorpusChunkedEmpty — пустой корпус: write не вызывается.
func TestWriteCorpusChunkedEmpty(t *testing.T) {
	stats, err := CountCorpus(context.Background(), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	written, err := WriteCorpusChunked(context.Background(), nil, 64, 0, stats,
		func(chunk []contract.Leaf, base, total int) (int, error) {
			calls++
			return len(chunk), nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if written != 0 || calls != 0 {
		t.Errorf("empty: written=%d calls=%d, want 0/0", written, calls)
	}
}

// TestWriteCorpusChunkedDefaultSize — size<=0 → чанк по умолчанию 2048.
func TestWriteCorpusChunkedDefaultSize(t *testing.T) {
	sources := []contract.Source{fakeSource{name: "docs", leafs: mkTestLeafs(3000, "d")}}
	stats, err := CountCorpus(context.Background(), sources, 0)
	if err != nil {
		t.Fatal(err)
	}
	var sizes []int
	if _, err := WriteCorpusChunked(context.Background(), sources, 0, 0, stats,
		func(chunk []contract.Leaf, base, total int) (int, error) {
			sizes = append(sizes, len(chunk))
			return len(chunk), nil
		}); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(sizes) != fmt.Sprint([]int{2048, 952}) {
		t.Errorf("sizes = %v, want [2048 952]", sizes)
	}
}

// TestProgressReportOffset — сквозной прогресс: done сдвигается на base
// (уже записанные до чанка leafs), total остаётся общим по всем чанкам.
func TestProgressReportOffset(t *testing.T) {
	r := NewProgressReporter(io.Discard, time.Hour)
	// чанк 1: base=0, 2 записано из 5
	progressReport(r, 0, 5, 2)
	if got := r.done.Load(); got != 2 {
		t.Errorf("done = %d, want 2", got)
	}
	if got := r.total.Load(); got != 5 {
		t.Errorf("total = %d, want 5", got)
	}
	// чанк 2: base=2, старт (0 в чанке), затем 3 записано
	progressReport(r, 2, 5, 0)
	if got := r.done.Load(); got != 2 {
		t.Errorf("done after start = %d, want 2", got)
	}
	progressReport(r, 2, 5, 3)
	if got := r.done.Load(); got != 5 {
		t.Errorf("done = %d, want 5 (сквозной счётчик)", got)
	}
	if got := r.total.Load(); got != 5 {
		t.Errorf("total = %d, want 5", got)
	}
}
