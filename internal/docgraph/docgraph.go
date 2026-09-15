// Package docgraph — коннектор gator kind=document → searchable Leaf (C1 #117,
// ADR-0013). Читает row parquet/documents (source=*/channel=*/dt=*) и мапит в
// contract.Leaf: документ-ссылка (title/oo_path/sha256) +, если задан
// BodyProvider, текст PDF-тела (T12 #310). Пакет cgo-free: parquet читает
// вызывающая сторона через pkg/duckdb.QueryRows, здесь — SQL read-запроса и
// типизированный маппинг. Зеркало internal/mailgraph, но свой Row: mail-поля
// не переиспользуются.
//
// Канон (gator origin/master, PR #140): дерево parquet/documents (PLURAL),
// hive source=portals/channel=<vendor>/dt=<YYYY-MM-DD>. Колонки — ровно
// query.DocumentsColumns / model.Document: source, channel, dt, content_hash,
// posted_at, observed_at, external_id, title, doc_date, filename, mime, size,
// sha256, oo_path, ref. Нет name/url/content_type/converted/fetched_at.
package docgraph

import (
	"context"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/eSlider/2dph/internal/contract"
)

// BodyProvider возвращает текст PDF-тела документа (T12 #310). Реализация
// сама достаёт артефакт (например OnlyOffice по oo_path/ref) и извлекает
// текст; пустая строка = тела нет, leaf остаётся metadata-only. Ошибка не
// должна ронять импорт: вызывающая сторона сохраняет metadata-leaf.
type BodyProvider interface {
	Body(ctx context.Context, r Row) (string, error)
}

// DefaultBodyLimit — лимит байт текста PDF-тела, добавляемого к метаданным
// leaf (T12 #310). Документы порталов редко больше; лимит держит Leaf и
// embedding в разумных рамках.
const DefaultBodyLimit = 256 << 10

// Row — типизированный row канала kind=document (read-схема
// parquet/documents). ContentHash — полный hash версии, из него
// GatorRef = kind=document#v-<hash8>.
type Row struct {
	Source      string
	Channel     string
	Date        time.Time // dt (день партиции)
	ContentHash string
	PostedAt    time.Time
	ObservedAt  time.Time
	ExternalID  string
	Title       string
	DocDate     time.Time
	Filename    string
	MIME        string
	Size        int64
	SHA256      string
	OOPath      string
	Ref         string
}

// gatorRef — deeplink версии канона: kind=document#v-<первые 8 hex content_hash>
// (та же нотация, что mailgraph gatorRef / gator VersionHash).
func (r Row) gatorRef() string {
	h := strings.TrimSpace(r.ContentHash)
	if h == "" {
		return ""
	}
	if len(h) > 8 {
		h = h[:8]
	}
	return "kind=document#v-" + h
}

// RowToLeaf мапит gator kind=document row в searchable contract.Leaf.
// ExternalID = external_id (идентичность как в model.Document). Если портал
// не дал external_id, fallback — sha256 (стабильная content-identity: прогон
// остаётся идемпотентным); row без обоих пропускается. Текст — метаданные
// (title + filename + oo_path); тело PDF добавляет RowToLeafBody.
func RowToLeaf(r Row) (contract.Leaf, bool) {
	return rowToLeaf(r, "")
}

// RowToLeafBody — как RowToLeaf, но после метаданных добавляет извлечённый
// текст PDF-тела (T12 #310), обрезанный до limit байт (limit <= 0 →
// DefaultBodyLimit). Пустое тело даёт результат, идентичный RowToLeaf.
func RowToLeafBody(r Row, body string, limit int) (contract.Leaf, bool) {
	return rowToLeaf(r, clampBody(body, limit))
}

// rowToLeaf собирает leaf с общим телом body (уже обрезанным).
func rowToLeaf(r Row, body string) (contract.Leaf, bool) {
	ext := strings.TrimSpace(r.ExternalID)
	if ext == "" {
		ext = strings.TrimSpace(r.SHA256)
	}
	if ext == "" {
		return contract.Leaf{}, false
	}
	text := leafText(r, body)
	if contract.NormalizeText(text) == "" {
		return contract.Leaf{}, false
	}
	observed := ""
	t := r.DocDate
	if t.IsZero() {
		t = r.ObservedAt
	}
	if !t.IsZero() {
		observed = t.UTC().Format(time.RFC3339)
	}
	return contract.Leaf{
		Source:     "document",
		ExternalID: ext,
		Kind:       "document",
		Text:       text,
		Root:       "info",
		Confidence: "confirmed",
		How:        "gator/document",
		Loc:        r.gatorRef(),
		ObservedAt: observed,
	}, true
}

// leafText — нормализованная комбинация title + filename + oo_path
// (метаданные документа) и, при наличии, текста PDF-тела. Пустые части
// отбрасываются, разделитель — двойной перевод строки, чтобы один документ
// из разных путей давал стабильный текст.
func leafText(r Row, body string) string {
	parts := make([]string, 0, 4)
	for _, s := range []string{r.Title, r.Filename, r.OOPath} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	if body = strings.TrimSpace(body); body != "" {
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n\n")
}

// clampBody нормализует текст тела и обрезает его до limit байт, не разрывая
// UTF-8-руну. limit <= 0 → DefaultBodyLimit.
func clampBody(body string, limit int) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if limit <= 0 {
		limit = DefaultBodyLimit
	}
	if len(body) <= limit {
		return body
	}
	cut := limit
	for cut > 0 && !utf8.RuneStart(body[cut]) {
		cut--
	}
	return strings.TrimSpace(body[:cut])
}
