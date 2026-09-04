//go:build cgo && system_ladybug

package brain

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"

	lbug "github.com/LadybugDB/go-ladybug"

	"github.com/eSlider/2dph/internal/brain/rank"
	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/pkg/cli"
)

// MainAuditContract — read-only аудит соответствия leafs контракту записи
// (P-9.2, docs/brain/contract.md). Не пишет в БД: только агрегаты одним
// проходом по Leaf, чтобы работать против живой kb.lbug (313k leafs).
func MainAuditContract(args []string) int {
	opt, err := rank.ParseJSONFlag("brain-audit-contract", args)
	if err != nil {
		return cli.Fail(err)
	}
	// Открываем отдельным read-only хендлом (ReadOnly=true): живая kb.lbug
	// держится сервисом RW, второй RW-open невозможен (status 1), а RO-open
	// работает. FTS/VECTOR не нужны — только агрегаты по Leaf.
	conn, closeConn, err := openReadOnly()
	if err != nil {
		fmt.Fprintf(os.Stderr, "open brain (read-only): %v\n", err)
		return 1
	}
	defer closeConn()
	rep, err := auditContract(conn)
	if err != nil {
		fmt.Fprintf(os.Stderr, "brain/audit-contract: %v\n", err)
		return 1
	}
	if opt.JSONOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		return b2i(enc.Encode(rep))
	}
	fmt.Printf("contract audit (read-only), db=%s\n", rep["db"])
	fmt.Printf("  total: %d\n", rep["total"])
	missing := rep["missing"].(map[string]int)
	for _, k := range []string{"observed_at", "external_id", "valid_from", "how", "loc", "kind"} {
		fmt.Printf("  missing_%s: %d\n", k, missing[k])
	}
	fmt.Println("  by_corpus (топ-20):")
	by := rep["by_corpus"].(map[string]int)
	keys := make([]string, 0, len(by))
	for k := range by {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return by[keys[i]] > by[keys[j]] })
	shown := 0
	rest := 0
	for _, k := range keys {
		if shown < 20 {
			fmt.Printf("    %-12s %d\n", k, by[k])
			shown++
		} else {
			rest += by[k]
		}
	}
	if rest > 0 {
		fmt.Printf("    %-12s %d\n", "(остальные)", rest)
	}
	return 0
}

// openReadOnly открывает kb.lbug с ReadOnly=true (без FTS/VECTOR — аудиту
// не нужны) и возвращает закрывающую функцию. Не трогает глобальный
// serve-хендл db/conn.
func openReadOnly() (*lbug.Connection, func(), error) {
	cfg := lbug.DefaultSystemConfig()
	cfg.ReadOnly = true
	cfg.MaxNumThreads = 8
	db, err := lbug.OpenDatabase(dbPath(), cfg)
	if err != nil {
		return nil, nil, err
	}
	conn, err := lbug.OpenConnection(db)
	if err != nil {
		db.Close()
		return nil, nil, err
	}
	return conn, func() {
		conn.Close()
		db.Close()
	}, nil
}

// corpusOf больше не нужен (P-9.3): source пишется как имя корпуса
// (mail/git/chats/docs/facts), аудит группирует по нему напрямую. Живая
// kb.lbug до пересборки всё ещё содержит старые evidence-указатели в source —
// до миграции такие строки показываются как есть.
func auditContract(conn *lbug.Connection) (map[string]any, error) {
	// Живая БД может ещё не иметь колонки external_id (миграция применяется
	// при старте сервиса). Пробуем с ней; при ошибке биндера падаем на
	// запрос без неё и честно считаем все leafs "без external_id".
	rows, err := queryContractRows(conn)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	missing := map[string]int{"observed_at": 0, "external_id": 0, "valid_from": 0, "how": 0, "loc": 0, "kind": 0}
	byCorpus := map[string]int{}
	total := 0
	for rows.HasNext() {
		row, err := rows.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 7 {
			continue
		}
		source := fmt.Sprint(vals[0])
		total++
		byCorpus[source]++
		if empty(vals[1]) {
			missing["observed_at"]++
		}
		if empty(vals[6]) {
			missing["external_id"]++
		}
		if empty(vals[2]) {
			missing["valid_from"]++
		}
		if empty(vals[3]) {
			missing["how"]++
		}
		if empty(vals[4]) {
			missing["loc"]++
		}
		if empty(vals[5]) {
			missing["kind"]++
		}
	}
	return map[string]any{
		"total": total, "missing": missing, "by_corpus": byCorpus, "db": dbPath(),
		"contract_version": contract.ReadContractVersion,
	}, nil
}

// queryContractRows выполняет аудит-запрос, с fallback на БД без external_id.
func queryContractRows(conn *lbug.Connection) (*lbug.QueryResult, error) {
	res, err := conn.Query(
		"MATCH (l:Leaf) RETURN l.source, l.observed_at, l.valid_from, l.how, l.loc, l.type, l.external_id")
	if err == nil {
		return res, nil
	}
	qClose(res)
	res, err = conn.Query(
		"MATCH (l:Leaf) RETURN l.source, l.observed_at, l.valid_from, l.how, l.loc, l.type, ''")
	if err != nil {
		return nil, err
	}
	return res, nil
}

// empty — Ladybug отдаёт отсутствующее значение как "" или "<nil>".
func empty(v any) bool {
	s := fmt.Sprint(v)
	return s == "" || s == "<nil>"
}
