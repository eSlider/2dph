// Package docgraph — коннектор gator kind=document → searchable Leaf (C1 #117,
// ADR-0013). Читает row parquet/documents (source=*/channel=*/dt=*) и мапит в
// contract.Leaf: документ-ссылка (title/oo_path/sha256) без тела PDF. Пакет
// cgo-free: parquet читает вызывающая сторона через pkg/duckdb.QueryRows,
// здесь — SQL read-запроса и типизированный маппинг. Зеркало
// internal/mailgraph, но свой Row: mail-поля не переиспользуются.
//
// Канон (gator origin/master, PR #140): дерево parquet/documents (PLURAL),
// hive source=portals/channel=<vendor>/dt=<YYYY-MM-DD>. Колонки — ровно
// query.DocumentsColumns / model.Document: source, channel, dt, content_hash,
// posted_at, observed_at, external_id, title, doc_date, filename, mime, size,
// sha256, oo_path, ref. Нет name/url/content_type/converted/fetched_at.
package docgraph

import (
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/contract"
)

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
// остаётся идемпотентным); row без обоих пропускается. Тело PDF не читается:
// v1 индексирует метаданные (title + filename + oo_path), см. open question в
// задаче.
func RowToLeaf(r Row) (contract.Leaf, bool) {
	ext := strings.TrimSpace(r.ExternalID)
	if ext == "" {
		ext = strings.TrimSpace(r.SHA256)
	}
	if ext == "" {
		return contract.Leaf{}, false
	}
	text := leafText(r)
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
// (метаданные документа). Пустые части отбрасываются, разделитель — двойной
// перевод строки, чтобы один документ из разных путей давал стабильный текст.
func leafText(r Row) string {
	parts := make([]string, 0, 3)
	for _, s := range []string{r.Title, r.Filename, r.OOPath} {
		if s = strings.TrimSpace(s); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, "\n\n")
}
