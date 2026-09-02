//go:build cgo && system_ladybug

// extractRows source-vector test (issue #248 B3): ANN build/upsert/ensure
// берут вектора из embedding-колонки когда она есть, а для column-less
// leafs (свежий rebuild при ANN-on пишет БД без колонки) — эмбедят l.text
// моделью. Модель подменяется стабом (как embedQueryFn) — офлайн, без
// HF-кэша: колоночные leafs не трогают модель вовсе.
package brain

import (
	"testing"

	"github.com/eSlider/2dph/internal/brain/ann"
)

// TestExtractRowsEmbedsColumnlessFromText — три leaf: два с колонкой, один
// без; extractRows возвращает все три, column-less эмбеддится из текста ровно
// один раз (стаб), колоночные идут как есть.
func TestExtractRowsEmbedsColumnlessFromText(t *testing.T) {
	dim := EmbedDim
	colVec := make([]float64, dim)
	for i := range colVec {
		colVec[i] = 0.01
	}
	colVec[0] = 0.9
	withCol := []LeafInput{
		{Text: "column leaf alpha", Source: "ann-col.md", Root: "info", Type: "reference", Embedding: colVec},
		{Text: "column leaf beta", Source: "ann-col.md", Root: "info", Type: "reference", Embedding: colVec},
	}
	noCol := LeafInput{Text: "text only leaf gamma", Source: "ann-col.md", Root: "info", Type: "reference"}
	all := append(withCol, noCol)

	dbpath := annFixtureDBLeafs(t, all)

	var stubbed []string
	prev := annExtractEmbed
	annExtractEmbed = func(text string) ([]float64, error) {
		stubbed = append(stubbed, text)
		vec := make([]float64, dim)
		vec[0] = 0.5
		vec[1] = 0.25
		return vec, nil
	}
	t.Cleanup(func() { annExtractEmbed = prev })

	openFixtureRead(t, dbpath)
	rows, err := extractRows(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != len(all) {
		t.Fatalf("extractRows returned %d rows, want %d (column-less тоже индексируется)", len(rows), len(all))
	}
	if len(stubbed) != 1 {
		t.Fatalf("model embed called %d times, want 1 (только column-less leaf)", len(stubbed))
	}
	if stubbed[0] != noCol.Text {
		t.Errorf("embedded text = %q, want %q", stubbed[0], noCol.Text)
	}
	// column-less row несёт стабовый вектор; колоночные — свои
	noColID := noCol.ContentHash()
	var gotNoCol *ann.Row
	for i := range rows {
		if rows[i].ID == noColID {
			gotNoCol = &rows[i]
		}
	}
	if gotNoCol == nil {
		t.Fatalf("column-less leaf %s not in extractRows", noColID)
	}
	if len(gotNoCol.Vec) != dim || gotNoCol.Vec[0] != 0.5 || gotNoCol.Vec[1] != 0.25 {
		t.Errorf("column-less vector = %v (len %d), want stub [0.5 0.25 ...]", gotNoCol.Vec, len(gotNoCol.Vec))
	}
	withColID := withCol[0].ContentHash()
	for i := range rows {
		if rows[i].ID == withColID {
			if got := rows[i].Vec[0]; got != 0.9 {
				t.Errorf("column leaf vector[0] = %v, want 0.9 (колонка не пере-эмбеддится)", got)
			}
		}
	}
}

// TestExtractRowsLegacyDBNoModel — legacy БД (все leafs с колонкой) не
// требует модель: стаб не вызывается, ошибок нет (быстрый путь без модели).
func TestExtractRowsLegacyDBNoModel(t *testing.T) {
	vec := make([]float64, EmbedDim)
	vec[3] = 1
	dbpath := annFixtureDBLeafs(t, []LeafInput{
		{Text: "legacy one", Source: "legacy.md", Root: "info", Type: "reference", Embedding: vec},
		{Text: "legacy two", Source: "legacy.md", Root: "info", Type: "reference", Embedding: vec},
	})

	prev := annExtractEmbed
	calls := 0
	annExtractEmbed = func(text string) ([]float64, error) {
		calls++
		return nil, nil
	}
	t.Cleanup(func() { annExtractEmbed = prev })

	openFixtureRead(t, dbpath)
	rows, err := extractRows(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("extractRows returned %d rows, want 2", len(rows))
	}
	if calls != 0 {
		t.Fatalf("model embed called %d times on legacy DB, want 0", calls)
	}
}
