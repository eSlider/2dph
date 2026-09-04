package corpus

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/pkg/utils"
)

// MailRoots возвращает корни mail-корпуса в порядке индексации (issue #199):
// live var/corpus/mail первый, затем legacy var/mail (issue #184). Обе
// раскладки <folder>/<id>/message.md; id-директории живут по разным схемам
// (OO numeric vs sha256:16) — внешний id адаптера от них не зависит (#5.2).
func MailRoots(root string) []string {
	return []string{
		filepath.Join(root, "var", "corpus", "mail"),
		filepath.Join(root, "var", "mail"),
	}
}

// Mail — адаптер mail-корпуса (live + legacy).
//
// P-9.3 #5.2: единая схема external_id — content-address итогового текста
// лифа sha256(NormalizeText(text))[:16]. Оба корпуса проходят один конвертер
// (mailconv) → один message из live и legacy при идентичном рендере даёт один
// external_id И один ContentHash → автоматический dedup при записи (seen-сет
// LoadMailLeafs больше не нужен). Message-ID отклонён: legacy message.json его
// не содержит (mailconv.Message без поля MessageID).
type Mail struct {
	Root  string // repo root; пусто → utils.Root()
	Since string // YYYY-MM-DD: только письма >= даты
}

func (m Mail) Name() string { return "mail" }

func (m Mail) Stream(ctx context.Context, emit func(contract.Leaf) error) error {
	if m.Root == "" {
		m.Root = utils.Root()
	}
	for _, root := range MailRoots(m.Root) {
		if err := mailLeafsIn(root, m.Since, emit); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// mailLeafsIn стримит один корпус-корень: каждый <id>/message.md (+ attachment .md).
func mailLeafsIn(root, since string, emit func(contract.Leaf) error) error {
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil
	}
	var mds []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if filepath.Base(p) == "message.md" {
			mds = append(mds, p)
		}
		return nil
	})
	if since != "" {
		filtered := mds[:0]
		for _, md := range mds {
			if msgDate(md) >= since {
				filtered = append(filtered, md)
			}
		}
		mds = filtered
	}
	for _, md := range mds {
		files := []string{md}
		att := filepath.Join(filepath.Dir(md), "attachments")
		if entries, err := os.ReadDir(att); err == nil {
			for _, e := range entries {
				if strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
					files = append(files, filepath.Join(att, e.Name()))
				}
			}
		}
		for _, f := range files {
			for _, lf := range markdownLeafs(f, "ooMail", "mail/import") {
				text := lf.Heading + "\n\n" + lf.Text
				if lf.Heading == "" {
					text = lf.Text
				}
				// Content-less leaf (CR-only/пустое тело): после нормализации
				// text=="" — UpsertLeaf режет такие записи и валит rebuild (#243).
				if contract.NormalizeText(text) == "" {
					continue
				}
				if err := emit(contract.Leaf{
					Source: "mail", ExternalID: contentAddr(text), Kind: lf.Type,
					Text: text, Root: "info", Confidence: "confirmed",
					How: "mail/import", Loc: f, ObservedAt: msgDate(md),
				}); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

// contentAddr — content-address: sha256(normalized text)[:16]. Стабильный
// external_id, вычисляемый из контента лифа (см. Mail.Stream).
func contentAddr(text string) string {
	sum := sha256.Sum256([]byte(contract.NormalizeText(text)))
	return hex.EncodeToString(sum[:])[:16]
}

// msgDate возвращает дату письма (YYYY-MM-DD) из message.json.
func msgDate(md string) string {
	j := filepath.Join(filepath.Dir(md), "message.json")
	raw, err := os.ReadFile(j)
	if err != nil {
		return ""
	}
	var d map[string]any
	if json.Unmarshal(raw, &d) != nil {
		return ""
	}
	for _, k := range []string{"receivedDate", "receivedAt"} {
		if s, ok := d[k].(string); ok && len(s) >= 10 {
			return s[:10]
		}
	}
	return ""
}
