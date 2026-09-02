package mailgraph

// Юнит-тесты коннектора gator kind=mail → MessageInput (D-1.3 #260),
// cgo-free: маппинг row parquet → Row/MessageInput, сортировка по дате,
// thread_id по цепочкам in_reply_to, SQL read-запроса, статистика.
// Данные synthetic (Alice/Bob/example.com), БД не нужна.

import (
	"strings"
	"testing"
	"time"

	"github.com/eSlider/2dph/internal/canon"
)

func mustTime(t *testing.T, s string) time.Time {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, s)
	if err != nil {
		t.Fatal(err)
	}
	return tm
}

// fullRow — типичный row kind=mail (JSON-shape duckdb): from/to/cc/bcc —
// []any из map[string]any, in_reply_to — []any строк, date — RFC3339.
func fullRow(t *testing.T) map[string]any {
	t.Helper()
	return map[string]any{
		"message_id":   "child@example.com",
		"folder":       "INBOX",
		"from":         []any{map[string]any{"name": "Bob Example", "email": "bob@example.com"}},
		"to":           []any{map[string]any{"name": "Alice", "email": "alice@example.com"}},
		"cc":           []any{map[string]any{"name": "Carol", "email": "carol@example.com"}},
		"bcc":          []any{map[string]any{"name": "", "email": "dave@example.com"}},
		"in_reply_to":  []any{"parent@example.com"},
		"subject":      "Re: thread",
		"date":         "2026-08-23T10:00:00Z",
		"body":         "reply body",
		"content_hash": "abcdef1234567890abcdef1234567890",
	}
}

// Маппинг полного row: все списки адресов, in_reply_to, атрибуты.
func TestMapRowFull(t *testing.T) {
	r, err := MapRow(fullRow(t))
	if err != nil {
		t.Fatal(err)
	}
	if r.MessageID != "child@example.com" || r.Folder != "INBOX" {
		t.Fatalf("id/folder = %q/%q", r.MessageID, r.Folder)
	}
	if r.From.Email != "bob@example.com" || r.From.Name != "Bob Example" {
		t.Fatalf("from = %+v", r.From)
	}
	if len(r.To) != 1 || r.To[0].Email != "alice@example.com" {
		t.Fatalf("to = %+v", r.To)
	}
	if len(r.CC) != 1 || r.CC[0].Email != "carol@example.com" {
		t.Fatalf("cc = %+v", r.CC)
	}
	if len(r.BCC) != 1 || r.BCC[0].Name != "" || r.BCC[0].Email != "dave@example.com" {
		t.Fatalf("bcc = %+v", r.BCC)
	}
	if len(r.InReplyTo) != 1 || r.InReplyTo[0] != "parent@example.com" {
		t.Fatalf("in_reply_to = %+v", r.InReplyTo)
	}
	if !r.Date.Equal(mustTime(t, "2026-08-23T10:00:00Z")) {
		t.Fatalf("date = %v", r.Date)
	}
	if r.Subject != "Re: thread" || r.Body != "reply body" {
		t.Fatalf("subject/body = %q/%q", r.Subject, r.Body)
	}
	if r.ContentHash != "abcdef1234567890abcdef1234567890" {
		t.Fatalf("content_hash = %q", r.ContentHash)
	}
}

// NULL-колонки (cc/bcc/in_reply_to отсутствуют), email с верхним регистром
// нормализуется в нижний (Person.id = email lowercase, как canon #99).
func TestMapRowNullsAndCase(t *testing.T) {
	row := fullRow(t)
	row["cc"] = nil
	row["bcc"] = nil
	row["in_reply_to"] = nil
	row["from"] = []any{map[string]any{"name": "Bob", "email": "Bob@Example.COM"}}

	r, err := MapRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.CC) != 0 || len(r.BCC) != 0 || len(r.InReplyTo) != 0 {
		t.Fatalf("nil lists must map to empty: cc=%v bcc=%v irt=%v", r.CC, r.BCC, r.InReplyTo)
	}
	if r.From.Email != "bob@example.com" {
		t.Fatalf("email must be lowercased: %+v", r.From)
	}
	if in := r.Input(); in.From.ID != "bob@example.com" || in.From.Email != "bob@example.com" {
		t.Fatalf("from person id/email = %q/%q", in.From.ID, in.From.Email)
	}
}

// date как time.Time (duckdb TIMESTAMP) тоже принимается.
func TestMapRowDateAsTime(t *testing.T) {
	row := fullRow(t)
	row["date"] = mustTime(t, "2026-08-23T10:00:00Z")
	r, err := MapRow(row)
	if err != nil {
		t.Fatal(err)
	}
	if !r.Date.Equal(mustTime(t, "2026-08-23T10:00:00Z")) {
		t.Fatalf("date = %v", r.Date)
	}
}

// Битый date — ошибка маппера (строка пропускается импортёром со счётом).
func TestMapRowBadDate(t *testing.T) {
	row := fullRow(t)
	row["date"] = "not-a-date"
	if _, err := MapRow(row); err == nil {
		t.Fatal("bad date must error")
	}
}

// Row → brain.MessageInput: ID/folder/subject/gator_ref, Person из адресов,
// ReplyTo = первый in_reply_to.
func TestRowInput(t *testing.T) {
	r, err := MapRow(fullRow(t))
	if err != nil {
		t.Fatal(err)
	}
	in := r.Input()
	if in.ID != "child@example.com" {
		t.Fatalf("id = %q", in.ID)
	}
	if in.Folder != "INBOX" || in.Subject != "Re: thread" {
		t.Fatalf("folder/subject = %q/%q", in.Folder, in.Subject)
	}
	if in.GatorRef != "kind=mail#v-abcdef12" {
		t.Fatalf("gator_ref = %q, want kind=mail#v-abcdef12", in.GatorRef)
	}
	if in.From.ID != "bob@example.com" || in.From.Name != "Bob Example" {
		t.Fatalf("from person = %+v", in.From)
	}
	if len(in.To) != 1 || in.To[0].Email != "alice@example.com" {
		t.Fatalf("to persons = %+v", in.To)
	}
	if len(in.CC) != 1 || len(in.BCC) != 1 {
		t.Fatalf("cc/bcc persons = %+v/%+v", in.CC, in.BCC)
	}
	if in.ReplyTo == nil || *in.ReplyTo != "parent@example.com" {
		t.Fatalf("reply_to = %v", in.ReplyTo)
	}
	if !in.SentAt.Equal(mustTime(t, "2026-08-23T10:00:00Z")) {
		t.Fatalf("sent_at = %v", in.SentAt)
	}
	// Edges() — SENT/TO/CC/BCC/REPLY_TO из canon (PART_OF отбрасывается в
	// planGraph, #259); здесь достаточно проверить, что письмо отдаёт рёбра.
	if len(in.Edges()) < 5 {
		t.Fatalf("edges = %d, want >= 5", len(in.Edges()))
	}
}

// Группирующий импорт: полный цикл маппинга в MessageInput с Person.
func TestRowsToInputsOrder(t *testing.T) {
	parent := map[string]any{
		"message_id": "parent@example.com", "folder": "INBOX",
		"from":         []any{map[string]any{"name": "Alice", "email": "alice@example.com"}},
		"to":           []any{map[string]any{"email": "bob@example.com"}},
		"date":         "2026-08-23T09:00:00Z",
		"body":         "thread start",
		"content_hash": "11111111111111111111111111111111",
	}
	rows, _, err := MapRows([]map[string]any{fullRow(t), parent})
	if err != nil {
		t.Fatal(err)
	}
	SortByDate(rows)
	if rows[0].MessageID != "parent@example.com" || rows[1].MessageID != "child@example.com" {
		t.Fatalf("sort order = %s, %s; want parent then child", rows[0].MessageID, rows[1].MessageID)
	}
	inputs := ToInputs(rows)
	if len(inputs) != 2 {
		t.Fatalf("inputs = %d", len(inputs))
	}
	if inputs[0].ID != "parent@example.com" {
		t.Fatalf("first input = %q", inputs[0].ID)
	}
}

// Сортировка по дате: письмо без даты уходит в конец.
func TestSortByDateZeroLast(t *testing.T) {
	rows := []Row{
		{MessageID: "later@example.com", Date: mustTime(t, "2026-08-24T00:00:00Z")},
		{MessageID: "no-date@example.com"},
		{MessageID: "early@example.com", Date: mustTime(t, "2026-08-01T00:00:00Z")},
	}
	SortByDate(rows)
	got := []string{rows[0].MessageID, rows[1].MessageID, rows[2].MessageID}
	want := []string{"early@example.com", "later@example.com", "no-date@example.com"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("order = %v, want %v", got, want)
	}
}

// thread_id: корень треда = письмо без in_reply_to; ответ наследует корень
// по цепочке. Родитель вне набора (dangling) — внешний корень цепочки.
func TestResolveThreads(t *testing.T) {
	rows := []Row{
		{MessageID: "root@example.com", InReplyTo: nil},
		{MessageID: "mid@example.com", InReplyTo: []string{"root@example.com"}},
		{MessageID: "leaf@example.com", InReplyTo: []string{"mid@example.com"}},
		{MessageID: "orphan@example.com", InReplyTo: []string{"outside@example.com"}},
		{MessageID: "self@example.com", InReplyTo: []string{"self@example.com"}},
	}
	ResolveThreads(rows)
	want := map[string]string{
		"root@example.com":   "root@example.com",
		"mid@example.com":    "root@example.com",
		"leaf@example.com":   "root@example.com",
		"orphan@example.com": "outside@example.com",
		"self@example.com":   "self@example.com", // цикл → fallback сам
	}
	for _, r := range rows {
		if r.ThreadID != want[r.MessageID] {
			t.Fatalf("%s thread = %q, want %q", r.MessageID, r.ThreadID, want[r.MessageID])
		}
	}
}

// Статистика dry-run: раскладка по folder, REPLY_TO-цепи, уникальные email.
// Порядок строк не должен влиять: родитель, идущий ПОЗЖЕ ребёнка, не
// считается dangling (ids собираются до подсчёта).
func TestStats(t *testing.T) {
	parent := map[string]any{
		"message_id": "p1@example.com", "folder": "Sent",
		"from":         []any{map[string]any{"email": "alice@example.com"}},
		"to":           []any{map[string]any{"email": "bob@example.com"}},
		"date":         "2026-08-23T09:00:00Z",
		"content_hash": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}
	child := map[string]any{
		"message_id": "c1@example.com", "folder": "INBOX",
		"from":         []any{map[string]any{"email": "bob@example.com"}},
		"to":           []any{map[string]any{"email": "alice@example.com"}},
		"cc":           []any{map[string]any{"email": "carol@example.com"}},
		"in_reply_to":  []any{"p1@example.com"},
		"date":         "2026-08-23T10:00:00Z",
		"content_hash": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
	}
	orphan := map[string]any{
		"message_id": "o1@example.com", "folder": "INBOX",
		"from":         []any{map[string]any{"email": "dave@example.com"}},
		"in_reply_to":  []any{"missing@example.com"},
		"date":         "2026-08-24T10:00:00Z",
		"content_hash": "cccccccccccccccccccccccccccccccc",
	}
	// child раньше parent в слайсе — родитель всё равно находится в ids.
	rows, _, err := MapRows([]map[string]any{child, orphan, parent})
	if err != nil {
		t.Fatal(err)
	}
	s := ComputeStats(rows)
	if s.Total != 3 {
		t.Fatalf("total = %d", s.Total)
	}
	if len(s.ByFolder) != 2 || s.ByFolder[0].Folder != "INBOX" || s.ByFolder[0].N != 2 {
		t.Fatalf("byFolder = %+v", s.ByFolder)
	}
	if s.WithReply != 2 {
		t.Fatalf("withReply = %d", s.WithReply)
	}
	if s.Dangling != 1 { // только orphan: missing@example.com
		t.Fatalf("dangling = %d", s.Dangling)
	}
	if s.Emails != 4 { // alice, bob, carol, dave
		t.Fatalf("emails = %d", s.Emails)
	}
	// canon.Person согласован: ID=email, Name из gator
	_ = canon.Person{ID: "x@example.com", Name: "X", Email: "x@example.com"}
}

// SQL read-запроса: latest на message_id, фильтр канала, tombstone deleted
// исключается, glob экранируется.
func TestReadSQL(t *testing.T) {
	got := ReadSQL("/hive/mail/**/*.parquet", "wheregroup")
	if !strings.Contains(got, "read_parquet('/hive/mail/**/*.parquet', hive_partitioning=true)") {
		t.Fatalf("sql missing glob: %s", got)
	}
	if !strings.Contains(got, `"channel" = 'wheregroup'`) {
		t.Fatalf("sql missing channel filter: %s", got)
	}
	if !strings.Contains(got, "PARTITION BY \"message_id\"") ||
		!strings.Contains(got, "ORDER BY \"observed_at\" DESC") {
		t.Fatalf("sql missing latest window: %s", got)
	}
	if !strings.Contains(got, `__rn = 1`) || !strings.Contains(got, "COALESCE(\"deleted\", false)") {
		t.Fatalf("sql missing latest/tombstone filter: %s", got)
	}
	if !strings.Contains(got, `"from"`) || !strings.Contains(got, `"date"`) {
		t.Fatalf("sql must quote reserved columns: %s", got)
	}
}

// glob с кавычкой экранируется (QuoteLiteral).
func TestReadSQLEscapesGlob(t *testing.T) {
	got := ReadSQL("/hive/it's/**/*.parquet", "ch")
	if !strings.Contains(got, "'/hive/it''s/**/*.parquet'") {
		t.Fatalf("glob not escaped: %s", got)
	}
}
