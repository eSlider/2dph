package duckdb

// Интеграция с реальным DuckDB (CGO): создание parquet-фикстуры и чтение
// через QueryRows. Структура колонок повторяет read-схему kind=mail (gator
// query.Mail, #89): LIST(STRUCT(name,email)), VARCHAR[], TIMESTAMP —
// duckdb-go должен вернуть JSON-совместимые значения, которые переживают
// encoding/json roundtrip (коннектор D-1.3 #260 читает parquet именно так).

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/duckdb/duckdb-go/v2"
)

func TestQueryRowsParquetMailShape(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	p := filepath.Join(dir, "data_0.parquet")

	// Fixture готовится на одном соединении (in-memory БД живёт на нём);
	// QueryRows ниже открывает свою БД и читает готовый parquet-файл.
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)

	if _, err := db.ExecContext(ctx, `CREATE TABLE m (
		message_id VARCHAR, folder VARCHAR,
		"from" STRUCT(name VARCHAR, email VARCHAR)[],
		cc STRUCT(name VARCHAR, email VARCHAR)[],
		irt VARCHAR[], "date" TIMESTAMP, body VARCHAR, content_hash VARCHAR)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.ExecContext(ctx, `INSERT INTO m VALUES
		('m1@example.com', 'INBOX', [{'name':'Alice','email':'alice@example.com'}], NULL,
		 ['p0@example.com'], TIMESTAMP '2026-09-01 08:00:00', 'hello', 'abcdef1234567890abcdef1234567890')`); err != nil {
		t.Fatal(err)
	}
	copySQL := "COPY (SELECT message_id, folder, \"from\", cc, irt, \"date\", body, content_hash FROM m) TO '" +
		strings.ReplaceAll(p, "'", "''") + "' (FORMAT PARQUET)"
	if _, err := db.ExecContext(ctx, copySQL); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("parquet fixture not written: %v", err)
	}

	rows, err := QueryRows(ctx, "SELECT * FROM read_parquet('"+strings.ReplaceAll(p, "'", "''")+"')")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("rows = %d, want 1", len(rows))
	}
	r := rows[0]

	// JSON roundtrip: duckdb-значения не должны ломать encoding/json.
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("json.Marshal(row): %v", err)
	}
	if !strings.Contains(string(b), `"email":"alice@example.com"`) {
		t.Fatalf("row JSON = %s, want nested from address", b)
	}
	if r["message_id"] != "m1@example.com" {
		t.Fatalf("message_id = %v", r["message_id"])
	}
	if r["cc"] != nil {
		t.Fatalf("cc = %v, want nil", r["cc"])
	}
	// TIMESTAMP колонка: маппер должен принять и time.Time, и RFC3339-строку.
	if _, isTime := r["date"].(time.Time); !isTime {
		if s, ok := r["date"].(string); !ok || !strings.Contains(s, "2026-09-01") {
			t.Fatalf("date = %#v (%T), want time.Time or RFC3339 string", r["date"], r["date"])
		}
	}
}

func TestExecRejectsBrokenSQL(t *testing.T) {
	if err := Exec(context.Background(), "NOT SQL"); err == nil {
		t.Fatal("broken SQL must error")
	}
}
