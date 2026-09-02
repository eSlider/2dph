//go:build cgo && system_ladybug

package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/internal/contract"
)

// TestFTSBufferPool — формула автоподбора пула для FTS-фазы (issue #244).
// Калибровка на замерах: 10.3MB реального текста → порог 64-128MB;
// 10.8MB синтетики → 192-256MB; продакшн 280MB текста → 1GB падает, 10GB
// проходит. ×32 покрывает худший замер и даёт ~9GB для продакшн-корпуса.
func TestFTSBufferPool(t *testing.T) {
	if got := FTSBufferPool(0); got != 1<<30 {
		t.Fatalf("FTSBufferPool(0) = %d, want 1GB floor", got)
	}
	// 10.3MB текста (калибровочный корпус): должен быть ≥ 128MB (порог)
	got := FTSBufferPool(10_300_000)
	if got < 128<<20 {
		t.Fatalf("FTSBufferPool(10.3MB) = %d, want >= 128MB (measured threshold)", got)
	}
	// 10.8MB синтетики: порог 256MB — покрыть с запасом
	got = FTSBufferPool(10_800_000)
	if got < 256<<20 {
		t.Fatalf("FTSBufferPool(10.8MB) = %d, want >= 256MB (measured threshold)", got)
	}
	// продакшн 280MB текста → ~9GB (совпадает с проверенными 10GB)
	got = FTSBufferPool(280_300_000)
	if got < 5<<30 {
		t.Fatalf("FTSBufferPool(280MB) = %d, want >= 5GB", got)
	}
}

// TestBuildIndexesAutoPoolSucceedsWhereSmallPoolFails — регрессия #244:
// старый путь (EnsureIndexes на write-хэндле с write-пулом) падает на
// корпусе ~10MB текста; BuildIndexes с автоподобранным пулом проходит.
func TestBuildIndexesAutoPoolSucceedsWhereSmallPoolFails(t *testing.T) {
	cfg := config.Defaults()
	cfg.BufferPool = 192 << 20 // write-пул: хватает на запись 3600×3KB, ниже FTS-порога 256MB
	Configure(&cfg)

	dir := t.TempDir()
	dbpath := filepath.Join(dir, "kb.lbug")
	db, conn, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitSchema(conn); err != nil {
		t.Fatal(err)
	}
	if _, err := WriteCorpus(conn, corpusLeafs(3600, 3<<10), nil, WriteOptions{Workers: 2, Batch: 64}); err != nil {
		t.Fatal(err)
	}
	db.Close()
	conn.Close()

	// старый путь на write-пуле падает (документируем ожидание) — на КОПИИ
	// БД: неудачный EnsureIndexes оставляет орфан, который заблокировал бы
	// BuildIndexes на оригинале.
	oldDB := filepath.Join(dir, "oldpath.lbug")
	if err := copyFile(dbpath, oldDB); err != nil {
		t.Fatal(err)
	}
	db2, conn2, err := OpenWritable(oldDB)
	if err != nil {
		t.Fatal(err)
	}
	oldPathErr := EnsureIndexes(conn2)
	db2.Close()
	conn2.Close()
	if oldPathErr == nil {
		t.Log("note: EnsureIndexes passed even at 192MB pool (corpus below threshold); BuildIndexes must still pass")
	}

	// новый путь: автопул → успех в том же прогоне
	if err := BuildIndexes(dbpath, 0); err != nil {
		t.Fatalf("BuildIndexes: %v", err)
	}
	if !ftsSearchable(t, dbpath, "leafword") {
		t.Fatal("FTS index must be searchable after BuildIndexes")
	}
}

// TestEnsureIndexesFailureMentionsOrphan — при падении CREATE_FTS_INDEX
// (маленький пул) ошибка явно называет орфан-таблицу и recovery, а не
// вводит в заблуждение фразой про delete kb.lbug.
func TestEnsureIndexesFailureMentionsOrphan(t *testing.T) {
	cfg := config.Defaults()
	cfg.BufferPool = 192 << 20
	Configure(&cfg)

	dir := t.TempDir()
	dbpath := filepath.Join(dir, "kb.lbug")
	db, conn, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatal(err)
	}
	if err := InitSchema(conn); err != nil {
		t.Fatal(err)
	}
	// корпус ~10.8MB текста: FTS на 192MB пуле падает с buffer pool full
	if _, err := WriteCorpus(conn, corpusLeafs(3600, 3<<10), nil, WriteOptions{Workers: 2, Batch: 64}); err != nil {
		t.Fatal(err)
	}
	err = EnsureIndexes(conn)
	if err == nil {
		db.Close()
		conn.Close()
		t.Skip("192MB pool did not fail CREATE_FTS_INDEX on this corpus; nothing to assert")
	}
	msg := err.Error()
	for _, want := range []string{"CREATE_FTS_INDEX", "appears_info", "rebuilt"} {
		if !strings.Contains(msg, want) {
			t.Errorf("EnsureIndexes error %q does not mention %q", msg, want)
		}
	}
	conn.Close()
	db.Close()

	// орфан блокирует retry даже с адекватным пулом (документированное
	// C-ограничение: внутренние таблицы недостижимы через SQL) — ошибка
	// BuildIndexes обязана это называть, а не молчать.
	err = BuildIndexes(dbpath, 0)
	if err == nil {
		t.Fatal("BuildIndexes must report the orphan that blocks retry")
	}
	if !strings.Contains(err.Error(), "appears_info") && !strings.Contains(err.Error(), "0_id_") {
		t.Errorf("BuildIndexes error %q must name the orphan table", err.Error())
	}
}

// corpusLeafs генерирует n лифов с текстом примерно textLen байт.
// Словарь и размеры повторяют калибровочный корпус (wthr3, issue #244):
// 3600 лифов × 3KB — FTS-порог 192MB падает / 256MB проходит.
func corpusLeafs(n, textLen int) []contract.Leaf {
	words := []string{"alpha", "beta", "gamma", "delta", "epsilon", "zeta", "eta", "theta",
		"iota", "kappa", "lambda", "mu", "nu", "xi", "omicron", "pi", "rho", "sigma", "tau",
		"upsilon", "phi", "chi", "psi", "omega"}
	leafs := make([]contract.Leaf, 0, n)
	for i := 0; i < n; i++ {
		var b strings.Builder
		b.Grow(textLen)
		b.WriteString(fmt.Sprintf("leaf number %d leafword ", i))
		for b.Len() < textLen {
			b.WriteString(words[i%len(words)])
			b.WriteByte(' ')
		}
		leafs = append(leafs, contract.Leaf{
			Source: "test-fts.md", ExternalID: fmt.Sprintf("t%d", i), Kind: "reference",
			Text: b.String(), Root: "info", Confidence: "confirmed",
		})
	}
	return leafs
}

// ftsSearchable открывает БД read-only и ищет токен через QUERY_FTS_INDEX.
func ftsSearchable(t *testing.T, dbpath, token string) bool {
	t.Helper()
	cfg := config.Defaults()
	cfg.BufferPool = 1 << 30
	Configure(&cfg)
	d, c, err := OpenWritable(dbpath)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer d.Close()
	defer c.Close()
	stmt, err := c.Prepare("CALL QUERY_FTS_INDEX('Leaf', 'id', $q) RETURN * LIMIT 1")
	if err != nil {
		return false
	}
	defer stmt.Close()
	res, err := c.Execute(stmt, map[string]any{"q": token})
	if err != nil {
		return false
	}
	defer res.Close()
	return res.HasNext()
}

// copyFile копирует файл БД (после закрытия хэндла wal уже свернут).
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}
