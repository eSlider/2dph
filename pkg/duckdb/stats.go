// Package duckdb runs in-process DuckDB for columnar aggregates.
// Graph facts stay in Ladybug. Web-search KV cache stays modernc sqlite.
package duckdb

import (
	"database/sql"
	"fmt"

	_ "github.com/duckdb/duckdb-go/v2"
)

type Stats struct {
	N   int     `json:"n"`
	Min float64 `json:"min"`
	P50 float64 `json:"p50"`
	P95 float64 `json:"p95"`
	Max float64 `json:"max"`
	Avg float64 `json:"avg"`
}

func Quantiles(samples []float64) (Stats, error) {
	if len(samples) == 0 {
		return Stats{}, fmt.Errorf("duckdb: empty samples")
	}
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return Stats{}, err
	}
	defer db.Close()
	var s Stats
	err = db.QueryRow(`
SELECT count(v), min(v), quantile_cont(v, 0.5), quantile_cont(v, 0.95), max(v), avg(v)
FROM (SELECT unnest(?) AS v)`, samples).Scan(
		&s.N, &s.Min, &s.P50, &s.P95, &s.Max, &s.Avg)
	return s, err
}

func CountJSONL(path string) (int64, error) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return 0, err
	}
	defer db.Close()
	var n int64
	err = db.QueryRow(`SELECT count(*) FROM read_json_auto(?)`, path).Scan(&n)
	return n, err
}
