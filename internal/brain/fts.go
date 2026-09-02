//go:build cgo && system_ladybug

// Фаза FTS-индексации (issue #244): автоматизация двухфазного workaround-а
// из docs/brain/rebuild.md в один вызов. CREATE_FTS_INDEX на полном корпусе
// падает с дефолтным пулом 1GB (buffer pool full) и оставляет частичную
// внутреннюю таблицу <tableID>_<indexName>_appears_info (0_id_appears_info),
// недостижимую через DROP TABLE/DROP_FTS_INDEX (каталог скрывает внутренние
// таблицы) — retry навсегда заблокирован, пока БД не пересоздана. Поэтому
// пул для фазы FTS подбирается автоматически по размеру текста корпуса.
package brain

import (
	"fmt"
	"strconv"

	lbug "github.com/LadybugDB/go-ladybug"
)

// FTSBufferPool возвращает буфер-пул Ladybug, достаточный для фазы
// CREATE_FTS_INDEX над корпусом из totalChars символов текста.
//
// Калибровка (issue #244, замеры порогов buffer pool full):
//   - реальные mail-лифы: 10.3MB текста → 64MB падает / 128MB проходит (×12.4);
//   - синтетический корпус 10.8MB → 192MB падает / 256MB проходит (×23.7);
//   - продакшн 105k лифов (280MB текста): 1GB падает / 10GB проходит (×2-36).
//
// Коэффициент ×32 покрывает худший замер с запасом 1.35× и даёт ~9GB для
// продакшн-корпуса — совпадает с проверенными 10GB. FTS держит в пуле
// appears_info (строка на токен) + dirty-страницы листов, это C-потолок
// Ladybug, не Go-heap. Пул — page cache, а не преаллокация (RSS растёт
// только под реальный working set), поэтому завышение безопасно.
func FTSBufferPool(totalChars int64) uint64 {
	need := totalChars * 32
	if need < 1<<30 {
		need = 1 << 30
	}
	return uint64(need)
}

// BuildIndexes переоткрывает БД с автоподобранным под корпус пулом и строит
// отсутствующие индексы (FTS). write-фаза (запись leafs) остаётся на
// конфигурированном KB_BUFFER_POOL (default 1GB); фаза индексов уходит на
// автоподобранный пул в отдельном хэндле (после закрытия write-хэндла —
// корректный результат и при удержанной C-памяти процесса).
//
// Явный KB_BUFFER_POOL действует как нижняя граница: pool = max(заданный,
// FTSBufferPool(totalChars)).
func BuildIndexes(path string, totalChars int64) error {
	pool := FTSBufferPool(totalChars)
	if v := brainCfg().BufferPool; v > 0 && uint64(v) > pool {
		pool = uint64(v)
	}
	db, conn, err := openWritable(path, pool)
	if err != nil {
		return err
	}
	defer db.Close()
	defer conn.Close()
	if err := EnsureIndexes(conn); err != nil {
		// После падения CREATE_FTS_INDEX остаётся орфан-таблица
		// 0_id_appears_info; она недостижима через SQL (внутренний каталог),
		// поэтому retry на этой БД невозможен — честно говорим recovery.
		return fmt.Errorf("FTS index build: %w (partial FTS tables like 0_id_appears_info are not droppable via SQL and block retry; delete %s and re-run --rebuild, or restore a backup)", err, path)
	}
	return nil
}

// CorpusTextChars возвращает суммарное число символов текста всех leafs
// (точная база для FTSBufferPool, покрывает и resume-корпус).
func CorpusTextChars(conn *lbug.Connection) (int64, error) {
	res, err := conn.Query("MATCH (l:Leaf) RETURN sum(size(l.text))")
	if err != nil {
		return 0, err
	}
	defer res.Close()
	if !res.HasNext() {
		return 0, nil
	}
	row, err := res.Next()
	if err != nil {
		return 0, err
	}
	vals, err := row.GetAsSlice()
	if err != nil || len(vals) < 1 || vals[0] == nil {
		return 0, nil
	}
	return strconv.ParseInt(fmt.Sprint(vals[0]), 10, 64)
}
