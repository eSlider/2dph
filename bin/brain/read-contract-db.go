//usr/bin/env bash -c 'exec "${0%/*}/../cgo/zig" go run -tags=system_ladybug,brain_readcontract_db "$0" "$@"' "$0" "$@"; exit
//go:build cgo && system_ladybug && brain_readcontract_db
//
// bin/brain/read-contract-db.go - read-only гейт контракта чтения (P-9.4)
// против живой kb.lbug. По образцу audit-contract.go: открывает БД с
// ReadOnly=true (второй RW-open невозможен при живом сервисе, RO работает),
// агрегаты одним проходом. Проверяет данные, из которых строятся ответы
// search/get/stats/audit, по нормативным схемам internal/contract.
//
//	bin/cgo/zig go run -tags=system_ladybug,brain_readcontract_db bin/brain/read-contract-db.go
//	bin/cgo/zig go run -tags=system_ladybug,brain_readcontract_db bin/brain/read-contract-db.go --json
//	KB_ROOT=/path/to/2dph ... (иначе путь ищется вверх от cwd: var/kb.lbug)
//
// Формат ответов (JSON) проверяет HTTP-гейт bin/brain/read-contract.go
// против живого сервиса; здесь — инварианты данных и домены полей.
//
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	lbug "github.com/LadybugDB/go-ladybug"

	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/pkg/cli"
	"github.com/integrii/flaggy"
)

func main() {
	os.Exit(runDBGate(os.Args[1:]))
}

type dbReport struct {
	Tool    string    `json:"tool"`
	Mode    string    `json:"mode"`
	Version string    `json:"contract_version"`
	DB      string    `json:"db"`
	Checks  []dbCheck `json:"checks"`
}

type dbCheck struct {
	Endpoint string `json:"endpoint"`
	Pass     bool   `json:"pass"`
	Info     string `json:"info,omitempty"`
	Sample   int    `json:"sample,omitempty"`
}

func runDBGate(args []string) int {
	var jsonOut bool
	p := flaggy.NewParser("brain-read-contract-db")
	p.Description = "read-only gate over live kb.lbug (P-9.4)"
	p.Bool(&jsonOut, "", "json", "machine-readable report")
	if err := cli.Parse(p, args); err != nil {
		fmt.Fprintf(os.Stderr, "brain/read-contract-db: %v\n", err)
		return 2
	}
	conn, closeConn, err := openReadOnly()
	if err != nil {
		fmt.Fprintf(os.Stderr, "open brain (read-only): %v\n", err)
		return 1
	}
	defer closeConn()

	rep := dbReport{Tool: "brain/read-contract-db", Mode: "db", Version: contract.ReadContractVersion, DB: dbPath()}
	rep.Checks = append(rep.Checks, checkStats(conn), checkAudit(conn), checkSample(conn))

	if jsonOut {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		enc.SetEscapeHTML(false)
		_ = enc.Encode(rep)
	} else {
		fmt.Printf("read contract gate: %s (contract_version=%s)\n", rep.Mode, rep.Version)
		for _, c := range rep.Checks {
			mark := "PASS"
			if !c.Pass {
				mark = "FAIL"
			}
			extra := ""
			if c.Sample > 0 {
				extra = fmt.Sprintf(" (sample=%d)", c.Sample)
			}
			fmt.Printf("  %-6s %-8s %s%s\n", mark, c.Endpoint, c.Info, extra)
		}
	}
	for _, c := range rep.Checks {
		if !c.Pass {
			return 1
		}
	}
	fmt.Println("gate: PASS")
	return 0
}

// openReadOnly открывает kb.lbug с ReadOnly=true — работает параллельно с
// живым RW-сервисом (Ladybug: второй RW-open невозможен, RO-open разрешён).
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

func dbPath() string {
	if v := os.Getenv("KB_ROOT"); v != "" {
		return filepath.Join(v, "var", "kb.lbug")
	}
	if wd, err := os.Getwd(); err == nil {
		if root := findRepoRoot(wd); root != "" {
			return filepath.Join(root, "var", "kb.lbug")
		}
	}
	return filepath.Join(".", "var", "kb.lbug")
}

func findRepoRoot(dir string) string {
	for i := 0; i < 8; i++ {
		if st, err := os.Stat(filepath.Join(dir, "var")); err == nil && st.IsDir() {
			return dir
		}
		if st, err := os.Stat(filepath.Join(dir, ".git")); err == nil && st.IsDir() {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}

func empty(v any) bool {
	s := fmt.Sprint(v)
	return s == "" || s == "<nil>"
}

// checkStats воспроизводит форму ответа /stats (total, by_root) и проверяет
// её по схеме контракта.
func checkStats(conn *lbug.Connection) dbCheck {
	res, err := conn.Query("MATCH (l:Leaf) RETURN l.root, count(*)")
	if err != nil {
		return dbCheck{Endpoint: "stats", Info: err.Error()}
	}
	defer res.Close()
	byRoot := map[string]int{}
	total := 0
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return dbCheck{Endpoint: "stats", Info: err.Error()}
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 2 {
			continue
		}
		root := fmt.Sprint(vals[0])
		n := 0
		switch v := vals[1].(type) {
		case int64:
			n = int(v)
		case float64:
			n = int(v)
		}
		byRoot[root] = n
		total += n
	}
	// Тот же JSON, что отдаёт продьюсер — валидатор проверяет и теги, и домены.
	body, _ := json.Marshal(map[string]any{
		"contract_version": contract.ReadContractVersion,
		"total":            total,
		"by_root":          byRoot,
		"db":               dbPath(),
	})
	if _, err := contract.ValidateStatsResponse(body); err != nil {
		return dbCheck{Endpoint: "stats", Info: err.Error()}
	}
	return dbCheck{Endpoint: "stats", Pass: true, Info: fmt.Sprintf("total=%d roots=%d", total, len(byRoot))}
}

// checkAudit воспроизводит форму ответа /audit (by_confidence).
func checkAudit(conn *lbug.Connection) dbCheck {
	res, err := conn.Query("MATCH (l:Leaf) RETURN l.root, l.confidence, count(*)")
	if err != nil {
		return dbCheck{Endpoint: "audit", Info: err.Error()}
	}
	defer res.Close()
	var rows []contract.AuditRow
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return dbCheck{Endpoint: "audit", Info: err.Error()}
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 3 {
			continue
		}
		rows = append(rows, contract.AuditRow{
			Root:       fmt.Sprint(vals[0]),
			Confidence: fmt.Sprint(vals[1]),
			Count:      asInt(vals[2]),
		})
	}
	body, _ := json.Marshal(map[string]any{
		"contract_version": contract.ReadContractVersion,
		"status":           "ok",
		"by_confidence":    rows,
	})
	if _, err := contract.ValidateAuditResponse(body); err != nil {
		return dbCheck{Endpoint: "audit", Info: err.Error()}
	}
	return dbCheck{Endpoint: "audit", Pass: true, Info: fmt.Sprintf("rows=%d", len(rows))}
}

// checkSample проверяет выборку leafs в форме ответов search/get: id, root,
// confidence, source, type, valid_from/valid_to.
func checkSample(conn *lbug.Connection) dbCheck {
	res, err := conn.Query(
		"MATCH (l:Leaf) RETURN l.id, l.text, l.root, l.confidence, l.source, l.type, l.valid_from, l.valid_to")
	if err != nil {
		return dbCheck{Endpoint: "sample", Info: err.Error()}
	}
	defer res.Close()
	const sampleN = 200
	hits := make([]contract.SearchHit, 0, sampleN)
	gets := make([]contract.GetResponse, 0, sampleN)
	n := 0
	for res.HasNext() && n < sampleN {
		row, err := res.Next()
		if err != nil {
			return dbCheck{Endpoint: "sample", Info: err.Error()}
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 8 {
			continue
		}
		hits = append(hits, contract.SearchHit{
			ID:         fmt.Sprint(vals[0]),
			Text:       fmt.Sprint(vals[1]),
			Root:       fmt.Sprint(vals[2]),
			Confidence: nullStr(vals[3]),
			ValidFrom:  nullStr(vals[6]),
			ValidTo:    nullStr(vals[7]),
		})
		gets = append(gets, contract.GetResponse{
			ID:         fmt.Sprint(vals[0]),
			Root:       fmt.Sprint(vals[2]),
			Confidence: nullStr(vals[3]),
			Source:     fmt.Sprint(vals[4]),
			Type:       fmt.Sprint(vals[5]),
			ValidFrom:  nullStr(vals[6]),
			ValidTo:    nullStr(vals[7]),
		})
		n++
	}
	// search-форма: confidence в hits опциональна — проверяем id/root/даты.
	sBody, _ := json.Marshal(map[string]any{
		"contract_version": contract.ReadContractVersion,
		"query":            "sample",
		"root_filter":      "",
		"count":            len(hits),
		"results":          hits,
	})
	if _, err := contract.ValidateSearchResponse(sBody); err != nil {
		return dbCheck{Endpoint: "sample", Info: err.Error()}
	}
	// get-форма: confidence/source/type обязательны.
	for i, g := range gets {
		gBody, _ := json.Marshal(map[string]any{
			"contract_version": contract.ReadContractVersion,
			"id":               g.ID,
			"root":             g.Root,
			"confidence":       g.Confidence,
			"source":           g.Source,
			"type":             g.Type,
			"valid_from":       g.ValidFrom,
			"valid_to":         g.ValidTo,
		})
		if _, err := contract.ValidateGetResponse(gBody); err != nil {
			return dbCheck{Endpoint: "sample", Info: fmt.Sprintf("get row %d: %v", i, err)}
		}
	}
	return dbCheck{Endpoint: "sample", Pass: true, Info: "search/get fields ok", Sample: n}
}

func nullStr(v any) string {
	if v == nil {
		return ""
	}
	s := fmt.Sprint(v)
	if s == "<nil>" {
		return ""
	}
	return s
}

func asInt(v any) int {
	switch n := v.(type) {
	case int64:
		return int(n)
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}
