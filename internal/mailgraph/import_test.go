//go:build cgo && system_ladybug

package mailgraph

// Интеграционный тест полного цикла D-1.3 (#260): synthetic hive parquet
// (канал wheregroup) → ReadSQL/duckdb → MapRows/ToInputs → brain.UpsertMessages
// в temp Ladybug. Проверяет узлы/рёбра и идемпотентность повторного прогона
// (MERGE, 0 дублей). Запуск (gcc, ladybug из lib-ladybug):
//
//	CC=gcc CGO_CFLAGS=-I<lib-ladybug> CGO_LDFLAGS="-L<lib-ladybug> -llbug" \
//	  go test -tags system_ladybug ./internal/mailgraph/ -run TestImportCycle -count=1
//
// В CI zig-линковка duckdb static lib не встаёт (libstdc++ ABI, как pkg/duckdb
// на Gitea runner), поэтому тест гоняется локально/на live-хосте gcc-сборкой.

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/LadybugDB/go-ladybug"
	_ "github.com/duckdb/duckdb-go/v2"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/pkg/duckdb"
)

// writeMailFixture создаёт hive parquet source=mail/channel=wheregroup с
// тремя письмами: parent (Sent), child (INBOX, REPLY_TO parent, cc), orphan
// (INBOX, REPLY_TO на родителя вне канала). Все колонки read-схемы kind=mail
// присутствуют (включая deleted/observed_at).
func writeMailFixture(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "source=mail", "channel=wheregroup")
	db, err := sql.Open("duckdb", "")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1)
	ctx := context.Background()

	msgs := []struct {
		dt, mid, folder, from, to, cc, irt, subject, date, hash, body string
	}{
		{"2026-09-01", "parent@example.com", "Sent", "alice@example.com", "bob@example.com", "", "", "thread", "2026-09-01 08:00:00", "11111111111111111111111111111111", "thread start"},
		{"2026-09-02", "child@example.com", "INBOX", "bob@example.com", "alice@example.com", "carol@example.com", "parent@example.com", "Re: thread", "2026-09-02 09:00:00", "22222222222222222222222222222222", "reply body"},
		{"2026-09-03", "orphan@example.com", "INBOX", "dave@example.com", "", "", "missing@example.com", "outside", "2026-09-03 10:00:00", "33333333333333333333333333333333", "no parent here"},
	}
	for i, r := range msgs {
		dir := filepath.Join(root, "dt="+r.dt)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		addrList := func(email string) string {
			if email == "" {
				return "NULL"
			}
			return "[{'name':'N','email':'" + email + "'}]"
		}
		irt := "NULL"
		if r.irt != "" {
			irt = "['" + r.irt + "']"
		}
		if _, err := db.ExecContext(ctx, "DROP TABLE IF EXISTS m"); err != nil {
			t.Fatal(err)
		}
		create := fmt.Sprintf(`CREATE TABLE m AS SELECT
			'%s'::VARCHAR AS message_id, '%s'::VARCHAR AS folder,
			%s::STRUCT(name VARCHAR, email VARCHAR)[] AS "from",
			%s::STRUCT(name VARCHAR, email VARCHAR)[] AS "to",
			%s::STRUCT(name VARCHAR, email VARCHAR)[] AS "cc",
			NULL::STRUCT(name VARCHAR, email VARCHAR)[] AS "bcc",
			%s::VARCHAR[] AS in_reply_to,
			'%s'::VARCHAR AS subject,
			TIMESTAMP '%s' AS "date",
			'%s'::VARCHAR AS body,
			'%s'::VARCHAR AS content_hash,
			TIMESTAMP '%s' AS posted_at,
			TIMESTAMP '%s' AS observed_at,
			false AS deleted,
			NULL::STRUCT(filename VARCHAR, size BIGINT)[] AS attachments`,
			r.mid, r.folder, addrList(r.from), addrList(r.to), addrList(r.cc),
			irt, r.subject, r.date, r.body, r.hash, r.date, r.date)
		if _, err := db.ExecContext(ctx, create); err != nil {
			t.Fatalf("create table %d: %v", i, err)
		}
		p := filepath.Join(dir, "data_"+fmt.Sprint(i)+".parquet")
		copySQL := `COPY (SELECT message_id, folder, "from", "to", cc, bcc, in_reply_to, subject, "date", body,
			content_hash, posted_at, observed_at, deleted, attachments FROM m) TO '` +
			strings.ReplaceAll(p, "'", "''") + `' (FORMAT PARQUET)`
		if _, err := db.ExecContext(ctx, copySQL); err != nil {
			t.Fatalf("copy %d: %v", i, err)
		}
	}
	// hive root = родитель source=mail/
	return filepath.Dir(filepath.Dir(root))
}

// graphCount считает узлы/рёбра Ladybug (одна count-колонка).
func graphCount(t *testing.T, conn *lbug.Connection, q string) int {
	t.Helper()
	res, err := conn.Query(q)
	if err != nil {
		t.Fatalf("query %q: %v", q, err)
	}
	defer res.Close()
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			t.Fatalf("row %q: %v", q, err)
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 1 {
			t.Fatalf("count row %q: %v", q, err)
		}
		var n int
		if _, err := fmt.Sscan(fmt.Sprint(vals[0]), &n); err != nil {
			t.Fatalf("count value %q: %v", vals[0], err)
		}
		return n
	}
	return 0
}

// mapAndInputs: read SQL → rows → MessageInput (пропуская битые строки).
func mapAndInputs(t *testing.T, sql string) []brain.MessageInput {
	t.Helper()
	raw, err := duckdb.QueryRows(context.Background(), sql)
	if err != nil {
		t.Fatal(err)
	}
	rows, skipped, firstErr := MapRows(raw)
	if skipped > 0 {
		t.Fatalf("skipped %d rows (%v)", skipped, firstErr)
	}
	ResolveThreads(rows)
	SortByDate(rows)
	return ToInputs(rows)
}

// Полный цикл: parquet → граф; повторный прогон — 0 дублей.
func TestImportCycle(t *testing.T) {
	hive := writeMailFixture(t)
	glob := filepath.Join(hive, "**", "*.parquet")
	readSQL := ReadSQL(glob, "wheregroup")

	// temp Ladybug + схема
	dir := t.TempDir()
	db, conn, err := brain.OpenWritable(filepath.Join(dir, "kb.lbug"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	defer conn.Close()
	if err := brain.InitSchema(conn); err != nil {
		t.Fatal(err)
	}

	importAll := func(skipExisting bool) int {
		inputs := mapAndInputs(t, readSQL)
		if skipExisting {
			have := map[string]bool{}
			res, err := conn.Query("MATCH (m:Message) RETURN m.id")
			if err != nil {
				t.Fatal(err)
			}
			for res.HasNext() {
				row, err := res.Next()
				if err != nil {
					t.Fatal(err)
				}
				vals, err := row.GetAsSlice()
				if err != nil || len(vals) < 1 {
					t.Fatal("message id row")
				}
				have[fmt.Sprint(vals[0])] = true
			}
			res.Close()
			kept := inputs[:0]
			for _, in := range inputs {
				if !have[in.ID] {
					kept = append(kept, in)
				}
			}
			inputs = kept
		}
		if len(inputs) > 0 {
			if err := brain.UpsertMessages(conn, inputs); err != nil {
				t.Fatalf("upsert: %v", err)
			}
		}
		return len(inputs)
	}

	// первый прогон: все 3 письма
	if n := importAll(false); n != 3 {
		t.Fatalf("first import = %d messages, want 3", n)
	}
	if got := graphCount(t, conn, "MATCH (m:Message) RETURN count(m)"); got != 3 {
		t.Fatalf("Message count = %d, want 3", got)
	}
	if got := graphCount(t, conn, "MATCH (p:Person) RETURN count(p)"); got != 4 {
		t.Fatalf("Person count = %d, want 4 (alice/bob/carol/dave)", got)
	}
	wantRels := map[string]int{"SENT": 3, "TO": 2, "CC": 1, "BCC": 0, "REPLY_TO": 1}
	for rel, want := range wantRels {
		if got := graphCount(t, conn, "MATCH ()-[:"+rel+"]->() RETURN count(*)"); got != want {
			t.Fatalf(":%s = %d, want %d", rel, got, want)
		}
	}
	// REPLY_TO ребро только к существующему родителю (orphan dangling skipped)
	if got := graphCount(t, conn,
		"MATCH (:Message {id:'child@example.com'})-[:REPLY_TO]->(:Message {id:'parent@example.com'}) RETURN count(*)"); got != 1 {
		t.Fatalf("child REPLY_TO parent = %d, want 1", got)
	}
	if got := graphCount(t, conn,
		"MATCH (:Message {id:'orphan@example.com'})-[:REPLY_TO]->() RETURN count(*)"); got != 0 {
		t.Fatalf("orphan REPLY_TO must not exist (dangling), got %d", got)
	}

	// повторный полный прогон: MERGE, счётчики не растут
	if n := importAll(false); n != 3 {
		t.Fatalf("second import = %d messages, want 3", n)
	}
	if got := graphCount(t, conn, "MATCH (m:Message) RETURN count(m)"); got != 3 {
		t.Fatalf("after rerun Message = %d, want 3 (idempotent)", got)
	}
	if got := graphCount(t, conn, "MATCH (p:Person) RETURN count(p)"); got != 4 {
		t.Fatalf("after rerun Person = %d, want 4", got)
	}
	if got := graphCount(t, conn, "MATCH ()-[r]->() RETURN count(r)"); got != 7 {
		t.Fatalf("after rerun total rels = %d, want 7 (3 SENT + 2 TO + 1 CC + 1 REPLY_TO)", got)
	}

	// инкрементальный прогон --skip-existing: 0 новых записей
	if n := importAll(true); n != 0 {
		t.Fatalf("incremental import = %d, want 0", n)
	}
}
