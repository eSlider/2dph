//go:build cgo && system_ladybug

package brain

import (
	"context"
	"fmt"
	"strings"

	"github.com/eSlider/2dph/internal/contract"

	lbug "github.com/LadybugDB/go-ladybug"
)

// WriteOptions controls corpus writing (worker concurrency, batch size, resume).
type WriteOptions struct {
	Limit    int               // max leafs to embed/write (0 = all)
	Workers  int               // parallel embedding workers (0 = 4)
	Batch    int               // leafs per transaction (0 = 64)
	Skip     bool              // skip leafs whose id already exists (resume)
	Existing map[string]bool   // resume-set (issue #237): собран вызывающим один раз на старте; nil → собрать здесь (ExistingLeafIDSet)
	Progress *ProgressReporter // optional progress/ETA monitor

	// Сквозной прогресс чанкованной записи (issue #237): ProgressDone — сколько
	// leafs записано до этого чанка, ProgressTotal — всего по всем чанкам
	// (0 → len(leafs)). done в Report/Finish не сбрасывается на чанк.
	ProgressDone  int
	ProgressTotal int
}

// WriteCorpus embeds (in parallel) and upserts corpus leafs in batches, linking
// FROM_FILE to Loc. Embedding errors abort; per-batch writes use one
// transaction.
//
// P-9.3: leafs приходят от адаптеров корпуса (internal/corpus, contract.Source)
// уже с source=корпус и external_id=устойчивый ref; текст нормализуется здесь
// единообразно (contract.NormalizeText), id = contract.ContentHash()[:32].
//
// issue #237: память ограничена чанком вызывающего (WriteCorpusChunked) —
// внутри держатся только leafs чанка, items и results эмбеддингов; срез на
// весь корпус не строится.
func WriteCorpus(conn *lbug.Connection, leafs []contract.Leaf, model *StaticModel, opt WriteOptions) (int, error) {
	if opt.Workers <= 0 {
		opt.Workers = 4
	}
	if opt.Batch <= 0 {
		opt.Batch = 64
	}
	if opt.Limit > 0 && len(leafs) > opt.Limit {
		leafs = leafs[:opt.Limit]
	}

	// Единая нормализация перед хэшем и записью (P-9.3 #5.3): хэш считается
	// от того же текста, который ляжет в БД. Skip-путь нормализует
	// filterExistingLeafs (при сравнении id); остальное — in-place: срез
	// принадлежит вызывающему (чанк) и после записи не нужен.
	if opt.Skip {
		existing := opt.Existing
		if existing == nil {
			var err error
			existing, err = ExistingLeafIDSet(conn)
			if err != nil {
				return 0, err
			}
		}
		leafs = filterExistingLeafs(leafs, existing)
	} else {
		for i := range leafs {
			leafs[i].Text = contract.NormalizeText(leafs[i].Text)
		}
	}

	base := opt.ProgressDone
	total := opt.ProgressTotal
	if total <= 0 {
		total = len(leafs)
	}
	progressReport(opt.Progress, base, total, 0)
	if len(leafs) == 0 {
		if opt.Progress != nil {
			opt.Progress.Finish(base, total)
		}
		return 0, nil
	}

	items := make([]poolItem, len(leafs))
	for i, lf := range leafs {
		items[i] = poolItem{i: i, text: lf.Text}
	}
	embed := func(text string) ([]float64, error) {
		if model == nil || text == "" {
			return nil, nil
		}
		return model.Embed(text)
	}
	var progress func(int, int)
	if opt.Progress != nil {
		progress = func(done int, _ int) { progressReport(opt.Progress, base, total, done) }
	}
	results, err := parallelEmbed(context.Background(), items, embed, opt.Workers, progress)
	if err != nil {
		return 0, err
	}

	inputs := make([]LeafInput, 0, len(results))
	for i, r := range results {
		if r.err != nil {
			return 0, fmt.Errorf("embed %d: %w", i, r.err)
		}
		lf := leafs[r.i]
		inputs = append(inputs, LeafInput{
			Text: strings.ToValidUTF8(items[r.i].text, "\uFFFD"), Root: lf.Root,
			Confidence: lf.Confidence, Source: lf.Source, SourceRev: "working-tree",
			How: lf.How, Loc: lf.Loc, Type: lf.Kind, Embedding: r.emb,
			ExternalID: lf.ExternalID, ObservedAt: lf.ObservedAt,
		})
	}

	n := 0
	for _, b := range chunkBounds(len(inputs), opt.Batch) {
		ids, err := AddLeafs(conn, inputs[b[0]:b[1]])
		if err != nil {
			return n, err
		}
		for j, id := range ids {
			in := inputs[b[0]+j]
			if _, err := LinkFromFile(conn, id, in.Loc, in.Source, ""); err != nil {
				return n, err
			}
			n++
		}
		progressReport(opt.Progress, base, total, n)
	}
	if opt.Progress != nil {
		opt.Progress.Finish(base+n, total)
	}
	return n, nil
}

// ExistingLeafIDSet returns the set of all current leaf ids (for resume).
// issue #237: собирается один раз на старте (до чанков) и передаётся в
// WriteCorpus через WriteOptions.Existing, а не пересобирается на чанк.
func ExistingLeafIDSet(conn *lbug.Connection) (map[string]bool, error) {
	res, err := conn.Query("MATCH (l:Leaf) RETURN l.id")
	if err != nil {
		return nil, err
	}
	defer res.Close()
	set := map[string]bool{}
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 1 {
			continue
		}
		set[fmt.Sprint(vals[0])] = true
	}
	return set, nil
}
