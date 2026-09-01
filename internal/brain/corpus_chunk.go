// Потоковая/чанкованная запись корпуса (issue #237): корпус не накапливается
// в одном срезе — CountCorpus считает leafs, WriteCorpusChunked стримит
// источники и пишет чанками по size. cgo-free: БД-запись (WriteCorpus)
// приходит извне через write, поэтому логика тестируется без Ladybug.
package brain

import (
	"context"
	"errors"
	"fmt"

	"github.com/eSlider/2dph/internal/contract"
)

// CorpusStats — итоги pass 1 (подсчёт): сколько leafs даёт каждый источник.
type CorpusStats struct {
	Total    int            // всего leafs по всем источникам
	BySource map[string]int // leafs по имени источника
}

// errLimitReached — внутренний стоп-сигнал: лимит leafs исчерпан, стрим
// можно оборвать (штатное завершение, не ошибка).
var errLimitReached = errors.New("corpus: limit reached")

// CountCorpus стримит источники в порядке списка и считает leafs, не
// накапливая их. Pass 1 для dry-run и для сквозного total/по-источникам.
func CountCorpus(ctx context.Context, sources []contract.Source) (CorpusStats, error) {
	stats := CorpusStats{BySource: map[string]int{}}
	for _, s := range sources {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		before := stats.Total
		if err := s.Stream(ctx, func(contract.Leaf) error {
			stats.Total++
			return nil
		}); err != nil {
			return stats, fmt.Errorf("corpus %s: %w", s.Name(), err)
		}
		stats.BySource[s.Name()] = stats.Total - before
	}
	return stats, nil
}

// WriteCorpusChunked стримит источники и пишет leafs чанками по size
// (<=0 → 2048), не удерживая корпус целиком: чанк заполняется из стрима,
// отдаётся write, буфер освобождается. Порядок источников и leafs
// сохраняется. limit>0 обрывает стрим после limit leafs (семантика --limit,
// применяется до чанков). stats — результат CountCorpus (pass 1): Total
// идёт в сквозной прогресс, BySource — в отчёт.
//
// write получает (чанк, base=уже записанные до чанка leafs, total=весь
// корпус) и возвращает число записанных; base/total нужны WriteCorpus для
// сквозного прогресса (done не сбрасывается на чанк).
func WriteCorpusChunked(ctx context.Context, sources []contract.Source, size, limit int, stats CorpusStats, write func(chunk []contract.Leaf, base, total int) (int, error)) (int, error) {
	if size <= 0 {
		size = 2048
	}
	written := 0
	emitted := 0
	buf := make([]contract.Leaf, 0, size)
	flush := func() error {
		if len(buf) == 0 {
			return nil
		}
		n, err := write(buf, written, stats.Total)
		if err != nil {
			return err
		}
		written += n
		buf = buf[:0]
		return nil
	}
	emit := func(l contract.Leaf) error {
		if limit > 0 && emitted >= limit {
			return errLimitReached
		}
		emitted++
		buf = append(buf, l)
		if len(buf) >= size {
			return flush()
		}
		return nil
	}
	for _, s := range sources {
		if err := ctx.Err(); err != nil {
			return written, err
		}
		if err := s.Stream(ctx, emit); err != nil {
			if errors.Is(err, errLimitReached) {
				break
			}
			return written, fmt.Errorf("corpus %s: %w", s.Name(), err)
		}
	}
	if err := flush(); err != nil {
		return written, err
	}
	return written, nil
}
