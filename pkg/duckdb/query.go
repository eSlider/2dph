package duckdb

// Exec и QueryRows — обобщённый доступ к in-process DuckDB поверх
// database/sql: DDL/COPY и произвольный SELECT с JSON-совместимым
// результатом. Образец — gator query.queryRows (#89): сканирование в any +
// нормализация DECIMAL; LIST(STRUCT) duckdb-go отдаёт как []any из
// map[string]any, TIMESTAMP — как time.Time, NULL — nil.

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/duckdb/duckdb-go/v2"
)

// open открывает свежую in-memory БД (одно соединение — in-memory БД
// принадлежит соединению).
func open() (*sql.DB, error) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("duckdb: open: %w", err)
	}
	db.SetMaxOpenConns(1)
	return db, nil
}

// Exec выполняет statement без результата (DDL, COPY, INSERT) на свежей
// in-memory БД. Используется тестами и утилитами для подготовки данных.
func Exec(ctx context.Context, stmt string) error {
	db, err := open()
	if err != nil {
		return err
	}
	defer db.Close()
	if _, err := db.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("duckdb: exec: %w", err)
	}
	return nil
}

// QueryRows выполняет SELECT и возвращает строки как JSON-совместимые map:
// значение каждой колонки приводится к типу, который encoding/json
// сериализует без потерь (gator-паттерн: STRING → string, TIMESTAMP →
// time.Time, LIST(STRUCT) → []any из map[string]any, DECIMAL → float64,
// NULL → nil).
func QueryRows(ctx context.Context, stmt string) ([]map[string]any, error) {
	db, err := open()
	if err != nil {
		return nil, err
	}
	defer db.Close()

	rows, err := db.QueryContext(ctx, stmt)
	if err != nil {
		return nil, fmt.Errorf("duckdb: query: %w", err)
	}
	defer rows.Close()

	cols, err := rows.Columns()
	if err != nil {
		return nil, fmt.Errorf("duckdb: columns: %w", err)
	}
	out := []map[string]any{}
	for rows.Next() {
		vals := make([]any, len(cols))
		ptrs := make([]any, len(cols))
		for i := range vals {
			ptrs[i] = &vals[i]
		}
		if err := rows.Scan(ptrs...); err != nil {
			return nil, fmt.Errorf("duckdb: scan: %w", err)
		}
		row := make(map[string]any, len(cols))
		for i, c := range cols {
			row[c] = normalizeValue(vals[i])
		}
		out = append(out, row)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("duckdb: rows: %w", err)
	}
	return out, nil
}

// normalizeValue приводит значение DuckDB к JSON-совместимому Go-типу:
// DECIMAL сериализуется как объект {Width,Scale,Value} — превращаем в
// float64; остальные типы (time.Time, STRUCT→map, LIST→slice, BLOB→base64)
// encoding/json сериализует корректно.
func normalizeValue(v any) any {
	switch d := v.(type) {
	case duckdb.Decimal:
		return d.Float64()
	default:
		return v
	}
}
