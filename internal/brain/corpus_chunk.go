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
//
// issue #248 (A1): счёт уникальный — pass 1 и pass 2 дедуплицируют одинаково
// (см. leafCounter), поэтому Total (уникальных) честно равен числу записей в
// БД и служит total сквозного прогресса; Streamed показывает «сырой» стрим
// (307k streamed → ~105k unique на полном rebuild) для прозрачности отчёта.
type CorpusStats struct {
	Streamed int            // всего leafs от источников, с дублями (raw стрим)
	Total    int            // уникальных leafs (кросс-чанковый dedup по ContentHash)
	BySource map[string]int // уникальных по имени источника
}

// errLimitReached — внутренний стоп-сигнал: лимит leafs исчерпан, стрим
// можно оборвать (штатное завершение, не ошибка).
var errLimitReached = errors.New("corpus: limit reached")

// leafCounter — счётчик стрима с кросс-чанковой дедупликацией (issue #248,
// A1): dedup-ключ = контрактный ContentHash (source|external_id|kind|text,
// нормализованный внутри Leaf.ContentHash) — тот же id, что ляжет в БД.
// Дубль (тот же ключ в пределах одного прогона — напр. mail из live
// var/corpus/mail и legacy var/mail) не доходит до эмбеддинга/записи.
// seen живёт весь стрим: дубликат во втором чанке/источнике тоже пропускается.
// limit>0 обрывает стрим после limit СТРИМНУТЫХ leafs (семантика --limit),
// дубликаты внутри окна не тратят лимит и не пишутся.
type leafCounter struct {
	limit    int
	seen     map[string]struct{}
	streamed int
	unique   int
}

func newLeafCounter(limit int) *leafCounter {
	return &leafCounter{limit: limit, seen: map[string]struct{}{}}
}

// add учитывает один leaf стрима. ok=false → лимит исчерпан (стрим можно
// оборвать). fresh=true → первое вхождение ContentHash в этом прогоне
// (единственный случай, когда leaf нужно эмбеддить/писать).
func (c *leafCounter) add(l contract.Leaf) (ok, fresh bool) {
	if c.limit > 0 && c.streamed >= c.limit {
		return false, false
	}
	c.streamed++
	key := l.ContentHash()
	if _, dup := c.seen[key]; dup {
		return true, false
	}
	c.seen[key] = struct{}{}
	c.unique++
	return true, true
}

// CountCorpus стримит источники в порядке списка и считает уникальные leafs,
// не накапливая их. Pass 1 для dry-run и для сквозного total/по-источникам.
// Тот же limit, что у WriteCorpusChunked, даёт число, равное записанному
// (dry-run == фактической записи, acceptance #248).
func CountCorpus(ctx context.Context, sources []contract.Source, limit int) (CorpusStats, error) {
	stats := CorpusStats{BySource: map[string]int{}}
	c := newLeafCounter(limit)
	for _, s := range sources {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		if err := s.Stream(ctx, func(l contract.Leaf) error {
			ok, fresh := c.add(l)
			if !ok {
				return errLimitReached
			}
			if fresh {
				stats.BySource[s.Name()]++
			}
			return nil
		}); err != nil {
			if errors.Is(err, errLimitReached) {
				break
			}
			return stats, fmt.Errorf("corpus %s: %w", s.Name(), err)
		}
	}
	stats.Streamed = c.streamed
	stats.Total = c.unique
	return stats, nil
}

// WriteCorpusChunked стримит источники и пишет leafs чанками по size
// (<=0 → 2048), не удерживая корпус целиком: чанк заполняется из стрима,
// отдаётся write, буфер освобождается. Порядок источников и leafs
// сохраняется (по первым вхождениям). limit>0 обрывает стрим после limit
// стримнутых leafs (семантика --limit, применяется до чанков). stats —
// результат CountCorpus (pass 1): Total (уникальных) идёт в сквозной
// прогресс, BySource — в отчёт.
//
// Кросс-чанковый dedup (issue #248, A1) включён ВСЕГДА (не только --skip):
// leaf с ContentHash, уже виденным в этом прогоне, пропускается — до
// эмбеддинга/записи не доходит. Детерминизм финального набора не меняется:
// id = ContentHash, MERGE-семантика БД схлопнула бы дубликаты к тому же id,
// здесь они отсекаются раньше (и не делают C-работу). При --skip поверх
// dedup'а остаётся existing-сет (WriteOptions.Existing / filterExistingLeafs):
// уже существующие в БД и так пропускаются.
//
// write получает (чанк, base=уже записанные до чанка leafs, total=уникальных
// по pass 1) и возвращает число записанных; base/total нужны WriteCorpus для
// сквозного прогресса (done не сбрасывается на чанк).
func WriteCorpusChunked(ctx context.Context, sources []contract.Source, size, limit int, stats CorpusStats, write func(chunk []contract.Leaf, base, total int) (int, error)) (int, error) {
	if size <= 0 {
		size = 2048
	}
	c := newLeafCounter(limit)
	written := 0
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
		ok, fresh := c.add(l)
		if !ok {
			return errLimitReached
		}
		if !fresh {
			return nil // дубль ContentHash в этом прогоне: не эмбеддить, не писать
		}
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
