// Package mailgraph — коннектор gator kind=mail → граф 2dph (D-1.3 #260).
// Читает row parquet/mail (канал wheregroup/gmail, latest-версия на
// message_id, tombstone deleted исключён) и мапит в canon.Message + атрибуты
// узла (Folder/Subject/GatorRef), которые пишет brain.UpsertMessages (D-1.2
// #259). MIME-парсинг не нужен — gator уже разобрал envelope (ADR-0013).
//
// Пакет cgo-free: чтение parquet исполняет вызывающая сторона через
// pkg/duckdb.QueryRows (транспорт), здесь — SQL read-запроса, типизированный
// маппинг row → brain.MessageInput, thread_id по цепочкам in_reply_to,
// сортировка по дате (родители REPLY_TO раньше ответов) и статистика
// dry-run.
package mailgraph

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/canon"
)

// Address — один адрес из gator row {name,email} (уже декодирован gator'ом).
type Address struct {
	Name  string
	Email string
}

// Row — типизированный row канала kind=mail (read-схема gator query.Mail,
// #89). MessageID — канон Message-ID (CanonMessageID или FallbackMessageID);
// ContentHash — полный hash версии, из него GatorRef = kind=mail#v-<hash8>.
type Row struct {
	MessageID   string
	Folder      string
	From        Address
	To          []Address
	CC          []Address
	BCC         []Address
	InReplyTo   []string
	Subject     string
	Date        time.Time
	Body        string
	ContentHash string
	// ThreadID — корень треда по цепочке in_reply_to (ResolveThreads).
	ThreadID string
}

// gatorRef — deeplink версии канона: kind=mail#v-<первые 8 hex content_hash>
// (та же нотация, что gator VersionHash, #89).
func (r Row) gatorRef() string {
	h := r.ContentHash
	if h == "" {
		return ""
	}
	if len(h) > 8 {
		h = h[:8]
	}
	return "kind=mail#v-" + h
}

// person строит canon.Person из адреса: ID = email lowercase (как canon #99,
// стабильность Person.id). Пустой email → нулевой Person (рёбер не даёт).
// Email уже нормализован в addrValue; trim/lower — страховка для ручных Row.
func person(a Address) canon.Person {
	email := strings.ToLower(strings.TrimSpace(a.Email))
	if email == "" {
		return canon.Person{}
	}
	return canon.Person{ID: email, Name: a.Name, Email: email}
}

func persons(as []Address) []canon.Person {
	if len(as) == 0 {
		return nil
	}
	out := make([]canon.Person, 0, len(as))
	for _, a := range as {
		if p := person(a); p.ID != "" {
			out = append(out, p)
		}
	}
	return out
}

// Input собирает brain.MessageInput (canon.Message + Folder/Subject/GatorRef)
// для записи в граф. ReplyTo — первый in_reply_to (canon.ReplyTo = *string).
func (r Row) Input() brain.MessageInput {
	var reply *string
	if len(r.InReplyTo) > 0 && r.InReplyTo[0] != "" {
		p := r.InReplyTo[0]
		reply = &p
	}
	return brain.MessageInput{
		Message: canon.Message{
			ID:       r.MessageID,
			ThreadID: r.ThreadID,
			Platform: "mail",
			From:     person(r.From),
			ReplyTo:  reply,
			To:       persons(r.To),
			CC:       persons(r.CC),
			BCC:      persons(r.BCC),
			SentAt:   r.Date,
			Body:     r.Body,
		},
		Folder:   r.Folder,
		Subject:  r.Subject,
		GatorRef: r.gatorRef(),
	}
}

// strValue нормализует скаляр колонки duckdb/json (string/[]byte) в строку.
func strValue(v any) string {
	switch s := v.(type) {
	case string:
		return s
	case []byte:
		return string(s)
	case nil:
		return ""
	default:
		return fmt.Sprint(s)
	}
}

// timeValue принимает date-колонку как time.Time (duckdb TIMESTAMP) или
// RFC3339-строку (parquet VARCHAR, как пишет gator pack).
func timeValue(v any) (time.Time, error) {
	switch t := v.(type) {
	case time.Time:
		return t, nil
	case nil:
		return time.Time{}, nil
	default:
		tm, err := time.Parse(time.RFC3339, strValue(v))
		if err != nil {
			return time.Time{}, fmt.Errorf("mailgraph: date %q: %w", strValue(v), err)
		}
		return tm, nil
	}
}

// addrValue принимает колонку LIST(STRUCT(name,email)) как []any из
// map[string]any (JSON-shape duckdb) и возвращает адреса. Email
// нормализуется в lowercase (Person.id = email, стабильность узлов #99).
func addrValue(v any) ([]Address, error) {
	switch l := v.(type) {
	case nil:
		return nil, nil
	case []any:
		out := make([]Address, 0, len(l))
		for i, e := range l {
			m, ok := e.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("mailgraph: address[%d] = %T, want map", i, e)
			}
			out = append(out, Address{Name: strValue(m["name"]), Email: strings.ToLower(strings.TrimSpace(strValue(m["email"])))})
		}
		return out, nil
	case []map[string]any:
		out := make([]Address, 0, len(l))
		for _, m := range l {
			out = append(out, Address{Name: strValue(m["name"]), Email: strings.ToLower(strings.TrimSpace(strValue(m["email"])))})
		}
		return out, nil
	default:
		return nil, fmt.Errorf("mailgraph: address column = %T, want []any", v)
	}
}

// strListValue принимает VARCHAR[] колонку ([]any строк).
func strListValue(v any) ([]string, error) {
	switch l := v.(type) {
	case nil:
		return nil, nil
	case []any:
		out := make([]string, 0, len(l))
		for _, e := range l {
			out = append(out, strValue(e))
		}
		return out, nil
	case []string:
		return l, nil
	default:
		return nil, fmt.Errorf("mailgraph: list column = %T, want []any", v)
	}
}

// MapRow конвертирует один row kind=mail (JSON-shape: []any/map[string]any,
// как отдаёт pkg/duckdb.QueryRows) в типизированный Row. Письмо без
// message_id не является ошибкой маппера: оно считается в статистике и
// пропускается на записи (узел без id бессмыслен).
func MapRow(m map[string]any) (Row, error) {
	var r Row
	r.MessageID = strings.TrimSpace(strValue(m["message_id"]))
	r.Folder = strings.TrimSpace(strValue(m["folder"]))
	r.Subject = strValue(m["subject"])
	r.Body = strValue(m["body"])
	r.ContentHash = strings.TrimSpace(strValue(m["content_hash"]))
	var err error
	if r.Date, err = timeValue(m["date"]); err != nil {
		return Row{}, err
	}
	froms, err := addrValue(m["from"])
	if err != nil {
		return Row{}, fmt.Errorf("mailgraph: from: %w", err)
	}
	if len(froms) > 0 {
		r.From = froms[0]
	}
	if r.To, err = addrValue(m["to"]); err != nil {
		return Row{}, fmt.Errorf("mailgraph: to: %w", err)
	}
	if r.CC, err = addrValue(m["cc"]); err != nil {
		return Row{}, fmt.Errorf("mailgraph: cc: %w", err)
	}
	if r.BCC, err = addrValue(m["bcc"]); err != nil {
		return Row{}, fmt.Errorf("mailgraph: bcc: %w", err)
	}
	if r.InReplyTo, err = strListValue(m["in_reply_to"]); err != nil {
		return Row{}, fmt.Errorf("mailgraph: in_reply_to: %w", err)
	}
	return r, nil
}

// MapRows конвертирует пачку rows; строки с ошибкой маппинга пропускаются
// (битая строка не валит весь импорт), счётчик skipped и первая ошибка
// возвращаются для dry-run отчёта.
func MapRows(raw []map[string]any) (rows []Row, skipped int, firstErr error) {
	rows = make([]Row, 0, len(raw))
	for _, m := range raw {
		r, err := MapRow(m)
		if err != nil {
			skipped++
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		rows = append(rows, r)
	}
	return rows, skipped, firstErr
}

// ResolveThreads заполняет ThreadID каждой строки: корень треда — первое
// письмо цепочки in_reply_to (root = письмо без in_reply_to → thread_id =
// message_id). Предок вне импортируемого набора (dangling) становится
// внешним корнем — ребро REPLY_TO к нему появится после импорта родителя.
// Цикл in_reply_to (битые данные) — fallback на собственный message_id.
func ResolveThreads(rows []Row) {
	byID := make(map[string]int, len(rows))
	for i := range rows {
		if rows[i].MessageID != "" {
			byID[rows[i].MessageID] = i
		}
	}
	for i := range rows {
		rows[i].ThreadID = threadRoot(rows, byID, i)
	}
}

func threadRoot(rows []Row, byID map[string]int, start int) string {
	cur := start
	seen := map[string]bool{}
	for {
		r := &rows[cur]
		if len(r.InReplyTo) == 0 || r.InReplyTo[0] == "" {
			return r.MessageID
		}
		parent := r.InReplyTo[0]
		next, ok := byID[parent]
		if !ok {
			return parent // корень цепочки вне набора
		}
		if seen[parent] {
			return rows[start].MessageID // цикл → fallback на себя
		}
		seen[parent] = true
		cur = next
	}
}

// SortByDate упорядочивает строки по возрастанию SentAt (родители REPLY_TO
// раньше ответов — ребро к существующему родителю), письма без даты в
// конец, детерминизм по MessageID.
func SortByDate(rows []Row) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i].Date, rows[j].Date
		aZero, bZero := a.IsZero(), b.IsZero()
		if aZero != bZero {
			return !aZero // zero в конец
		}
		if !a.Equal(b) {
			return a.Before(b)
		}
		return rows[i].MessageID < rows[j].MessageID
	})
}

// ToInputs мапит строки (после ResolveThreads + SortByDate) в записываемые
// MessageInput-ы. Письма без message_id пропускаются (узел не строится).
func ToInputs(rows []Row) []brain.MessageInput {
	out := make([]brain.MessageInput, 0, len(rows))
	for i := range rows {
		if rows[i].MessageID == "" {
			continue
		}
		out = append(out, rows[i].Input())
	}
	return out
}

// FolderCount — одна строка раскладки dry-run по folder.
type FolderCount struct {
	Folder string
	N      int
}

// Stats — числа dry-run импорта: раскладка, REPLY_TO-цепи, уникальные
// адреса (приближение Person-узлов до записи).
type Stats struct {
	Total     int
	ByFolder  []FolderCount
	WithReply int // писем с in_reply_to (потенциальные REPLY_TO-рёбра)
	Dangling  int // in_reply_to[0] отсутствует среди message_id набора
	Emails    int // уникальных email (from+to+cc+bcc, lowercase)
}

// Stats считает раскладку строк до записи (dry-run).
func ComputeStats(rows []Row) Stats {
	s := Stats{Total: len(rows)}
	folders := map[string]int{}
	emails := map[string]bool{}
	ids := make(map[string]bool, len(rows))
	for i := range rows {
		if rows[i].MessageID != "" {
			ids[rows[i].MessageID] = true
		}
	}
	for i := range rows {
		r := &rows[i]
		folders[r.Folder]++
		if r.From.Email != "" {
			emails[strings.ToLower(r.From.Email)] = true
		}
		add := func(as []Address) {
			for _, a := range as {
				if a.Email != "" {
					emails[strings.ToLower(a.Email)] = true
				}
			}
		}
		add(r.To)
		add(r.CC)
		add(r.BCC)
		if len(r.InReplyTo) > 0 && r.InReplyTo[0] != "" {
			s.WithReply++
			if !ids[r.InReplyTo[0]] {
				s.Dangling++
			}
		}
	}
	s.Emails = len(emails)
	for f, n := range folders {
		s.ByFolder = append(s.ByFolder, FolderCount{Folder: f, N: n})
	}
	sort.Slice(s.ByFolder, func(i, j int) bool { return s.ByFolder[i].Folder < s.ByFolder[j].Folder })
	return s
}
