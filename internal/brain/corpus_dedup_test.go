package brain

// Кросс-чанковый dedup до записи (issue #248, A1): leafs с одинаковым
// контрактным ContentHash (source|external_id|kind|text) в пределах одного
// прогона пишутся один раз — и при fresh rebuild (двойной стрим mail: live
// var/corpus/mail + legacy var/mail), и поверх existing-сета при --skip.
// Dedup-логика cgo-free: тестируется без Ladybug на fakeSource.

import (
	"context"
	"fmt"
	"testing"

	"github.com/eSlider/2dph/internal/contract"
)

// dupLeafs возвращает n уникальных leafs + повторные вхождения leafs[i]
// (тот же ContentHash) в конце — как live+legacy mail корпуса.
func dupLeafs(n int, dups ...int) []contract.Leaf {
	leafs := mkTestLeafs(n, "d")
	for _, i := range dups {
		leafs = append(leafs, leafs[i])
	}
	return leafs
}

func TestCountCorpusDedupUniqueVsStreamed(t *testing.T) {
	sources := []contract.Source{
		fakeSource{name: "mail", leafs: dupLeafs(10, 2, 2, 5, 7, 7)},
	}
	stats, err := CountCorpus(context.Background(), sources, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Streamed != 15 {
		t.Errorf("Streamed = %d, want 15 (все leafs из источников)", stats.Streamed)
	}
	if stats.Total != 10 {
		t.Errorf("Total = %d, want 10 (уникальных ContentHash)", stats.Total)
	}
	if stats.BySource["mail"] != 10 {
		t.Errorf("BySource[mail] = %d, want 10", stats.BySource["mail"])
	}
}

// TestWriteCorpusChunkedDedupAcrossChunks — дубликат во втором чанке не
// доходит до write: каждый уникальный ContentHash пишется один раз, порядок
// первых вхождений сохраняется (детерминизм финального набора, #248).
func TestWriteCorpusChunkedDedupAcrossChunks(t *testing.T) {
	// 10 уникальных + дубли d-2 и d-5 в хвосте; чанк size=4 → дубли попадают
	// в другой чанк, чем оригиналы (d-2 в 1-м чанке, дубль в 3-м).
	leafs := dupLeafs(10, 2, 5)
	sources := []contract.Source{fakeSource{name: "docs", leafs: leafs}}
	stats, err := CountCorpus(context.Background(), sources, 0)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 10 {
		t.Fatalf("pass1 Total = %d, want 10 (unique)", stats.Total)
	}
	var got []string
	var chunkSizes []int
	written, err := WriteCorpusChunked(context.Background(), sources, 4, 0, stats,
		func(chunk []contract.Leaf, base, total int) (int, error) {
			chunkSizes = append(chunkSizes, len(chunk))
			if total != 10 {
				t.Errorf("chunk total = %d, want 10 (уникальных, не streamed)", total)
			}
			for _, l := range chunk {
				got = append(got, l.ExternalID)
			}
			return len(chunk), nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if written != 10 {
		t.Errorf("written = %d, want 10 (дубликаты не пишутся)", written)
	}
	// чанки: d-0..d-3 | d-4..d-7 | d-8,d-9 — дубли d-2/d-5 отброшены dedup'ом
	if fmt.Sprint(chunkSizes) != fmt.Sprint([]int{4, 4, 2}) {
		t.Errorf("chunkSizes = %v, want [4 4 2]", chunkSizes)
	}
	for i := 0; i < 10; i++ {
		if got[i] != fmt.Sprintf("d-%d", i) {
			t.Fatalf("got[%d] = %s, want d-%d (порядок первых вхождений)", i, got[i], i)
		}
	}
}

// TestWriteCorpusChunkedDedupWithExisting — --skip/resume: existing-сет из БД
// остаётся, кросс-чанковый dedup работает поверх (уже записанные в этом
// прогоне тоже пропускаются). Итог = уникальные минус existing.
func TestWriteCorpusChunkedDedupWithExisting(t *testing.T) {
	leafs := dupLeafs(10, 2, 5) // 12 leafs: 10 уникальных + 2 дубля
	sources := []contract.Source{fakeSource{name: "docs", leafs: leafs}}
	stats, err := CountCorpus(context.Background(), sources, 0)
	if err != nil {
		t.Fatal(err)
	}
	existing := map[string]bool{leafs[0].ContentHash(): true, leafs[1].ContentHash(): true}
	var got []string
	written, err := WriteCorpusChunked(context.Background(), sources, 4, 0, stats,
		func(chunk []contract.Leaf, base, total int) (int, error) {
			kept := filterExistingLeafs(chunk, existing)
			for _, l := range kept {
				existing[l.ContentHash()] = true
				got = append(got, l.ExternalID)
			}
			return len(kept), nil
		})
	if err != nil {
		t.Fatal(err)
	}
	// 10 уникальных, 2 уже в existing → 8 новых; дубли d-2/d-5 отсечены dedup'ом
	if written != 8 {
		t.Errorf("written = %d, want 8", written)
	}
	want := []string{"d-2", "d-3", "d-4", "d-5", "d-6", "d-7", "d-8", "d-9"}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("kept order = %v, want %v", got, want)
	}
}

// TestCountCorpusLimitMatchesWriteLimit — --limit считает СТРИМНУТЫЕ leafs
// (обрыв после limit), dedup режет уникальных внутри окна; pass1 (dry-run) и
// pass2 (write) дают одно число при том же limit (acceptance #248: written ==
// unique по dry-run).
func TestCountCorpusLimitMatchesWriteLimit(t *testing.T) {
	// 100 уникальных + 20 дублей в хвосте; limit=50 → в окне 50 стримнутых,
	// дублей в первых 50 нет (дубли в хвосте) → unique=50.
	leafs := mkTestLeafs(100, "d")
	for i := 0; i < 20; i++ {
		leafs = append(leafs, leafs[i])
	}
	sources := []contract.Source{fakeSource{name: "docs", leafs: leafs}}
	stats, err := CountCorpus(context.Background(), sources, 50)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Streamed != 50 {
		t.Errorf("Streamed = %d, want 50 (лимит на стрим)", stats.Streamed)
	}
	if stats.Total != 50 {
		t.Errorf("Total = %d, want 50", stats.Total)
	}
	var got []string
	written, err := WriteCorpusChunked(context.Background(), sources, 10, 50, stats,
		func(chunk []contract.Leaf, base, total int) (int, error) {
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
		t.Errorf("written = %d, want 50 (== unique по dry-run)", written)
	}
	if got[0] != "d-0" || got[49] != "d-49" {
		t.Errorf("limit+dedup нарушает порядок: got[0]=%s got[49]=%s", got[0], got[49])
	}
}

// TestWriteCorpusChunkedDedupLimitWindow — дубль ВНУТРИ окна limit тоже
// пропускается: 3 уникальных + дубль d-0 на 4-й позиции, limit=4 →
// streamed=4, unique=3, written=3.
func TestWriteCorpusChunkedDedupLimitWindow(t *testing.T) {
	leafs := mkTestLeafs(3, "d")
	leafs = append(leafs, leafs[0])
	sources := []contract.Source{fakeSource{name: "docs", leafs: leafs}}
	stats, err := CountCorpus(context.Background(), sources, 4)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Total != 3 {
		t.Errorf("Total = %d, want 3 (уникальных в окне limit)", stats.Total)
	}
	var got []string
	written, err := WriteCorpusChunked(context.Background(), sources, 2, 4, stats,
		func(chunk []contract.Leaf, base, total int) (int, error) {
			for _, l := range chunk {
				got = append(got, l.ExternalID)
			}
			return len(chunk), nil
		})
	if err != nil {
		t.Fatal(err)
	}
	if written != 3 {
		t.Errorf("written = %d, want 3", written)
	}
	if fmt.Sprint(got) != fmt.Sprint([]string{"d-0", "d-1", "d-2"}) {
		t.Errorf("got = %v, want [d-0 d-1 d-2]", got)
	}
}
