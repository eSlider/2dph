package ooimport

// Дописывание тега/about существующим matched-контактам (N-1.4 #271,
// решение владельца #267, вопрос 3): существующий по email контакт (в т.ч.
// импорт VCF/MAB #85) получает тег 2dph:network:<target> (если отсутствует)
// и about-premises-сводку (только если about пуст) — аддитивно, ручные
// правки не перезаписываются. Механизм по email-ключу от манифеста (не
// хардкод под id); переиспользуется N-1.5 #275 для eslider@. Повтор = 0
// изменений (все Unchanged).

import (
	"context"
	"fmt"
	"strings"

	"github.com/eslider/go-onlyoffice"
)

// BackfillChange — что дописать существующему matched-контакту (аддитивно).
// AddTag — тег источника отсутствует (добавить); SetAbout — about пуст и
// сводка есть (записать сводку; иначе "").
type BackfillChange struct {
	AddTag   bool
	SetAbout string
}

// Empty — изменений нет (у контакта уже всё, что нужно).
func (c BackfillChange) Empty() bool { return !c.AddTag && c.SetAbout == "" }

// BackfillChanges — аддитивное решение по текущему состоянию контакта:
// тег добавляем, если его нет; about-сводку пишем только если about пуст/
// пробельный (ручные правки, в т.ч. Org из импорта #85, не перезаписываем).
// wantAbout — сводка из манифеста (уже готова в Contact.About).
func BackfillChanges(hasTag bool, about, wantAbout string) BackfillChange {
	ch := BackfillChange{AddTag: !hasTag}
	if strings.TrimSpace(about) == "" {
		if w := strings.TrimSpace(wantAbout); w != "" {
			ch.SetAbout = w
		}
	}
	return ch
}

// BackfillReport — итоги дописывания matched-контактам.
type BackfillReport struct {
	Updated   int // персон, которым дописано (тег и/или about; write)
	Would     int // dry-run: было бы дописано
	Unchanged int // matched, у кого тег и about уже есть (повтор = все)
	Missing   int // кандидат плана не найден в CRM по email
	Failed    int // сбои дописывания
	Failures  []Failure
}

// RunBackfill дописывает тег и about существующим matched-контактам плана
// (аддитивно, BackfillChanges). hasTag определяется множеством контактов под
// тегом (ListContactsByTag), about/имена — GetContact. Dry-run: только
// lookups (тег не создаётся, ничего не пишется). Идемпотентно: повтор = 0
// Updated, все Unchanged.
func RunBackfill(ctx context.Context, c *onlyoffice.Client, p Plan, opts Options) (BackfillReport, error) {
	rep := BackfillReport{}
	if len(p.Contacts) == 0 {
		return rep, nil
	}
	idx, err := c.BuildContactEmailIndex(ctx)
	if err != nil {
		return rep, fmt.Errorf("contact index: %w", err)
	}
	haveTag, err := contactIDsByTag(ctx, c, p.Tag)
	if err != nil {
		if !opts.DryRun {
			return rep, fmt.Errorf("tag index %q: %w", p.Tag, err)
		}
		haveTag = map[string]bool{} // dry-run до создания тега: считаем, что тега нет ни у кого
	}
	if !opts.DryRun {
		if err := c.CreateContactTag(ctx, p.Tag); err != nil {
			return rep, fmt.Errorf("create tag %q: %w", p.Tag, err)
		}
	}
	for _, ct := range p.Contacts {
		id, ok := idx[ct.Email]
		if !ok {
			rep.Missing++
			continue
		}
		full, err := c.GetContact(ctx, id)
		if err != nil {
			rep.Failed++
			rep.Failures = append(rep.Failures, Failure{Email: ct.Email, Err: fmt.Sprintf("get contact: %v", err)})
			continue
		}
		ch := BackfillChanges(haveTag[id], fieldString(full, "about"), ct.About)
		if ch.Empty() {
			rep.Unchanged++
			continue
		}
		if opts.DryRun {
			rep.Would++
			continue
		}
		rep.Updated += backfillWrite(ctx, c, ct, id, full, ch, p.Tag, &rep)
	}
	return rep, nil
}

// backfillWrite применяет изменение к одному контакту; возвращает 1 при
// успехе хотя бы одной записи, 0 при сбое (сбой заносится в rep.Failures).
func backfillWrite(ctx context.Context, c *onlyoffice.Client, ct Contact, id string, full map[string]any, ch BackfillChange, tag string, rep *BackfillReport) int {
	changed := false
	var firstErr error
	if ch.AddTag {
		if err := c.AddContactTag(ctx, id, tag); err != nil {
			firstErr = err
		} else {
			changed = true
		}
	}
	if ch.SetAbout != "" {
		if _, err := c.UpdatePerson(ctx, id, fieldString(full, "firstName"), fieldString(full, "lastName"), 0, "", ch.SetAbout); err != nil {
			if firstErr == nil {
				firstErr = err
			}
		} else {
			changed = true
		}
	}
	if firstErr != nil {
		rep.Failed++
		rep.Failures = append(rep.Failures, Failure{Email: ct.Email, Err: firstErr.Error()})
		return 0
	}
	if !changed {
		return 0
	}
	return 1
}

// fieldString — строковое поле контакта OO; null/отсутствие → "" (не "<nil>").
func fieldString(m map[string]any, key string) string {
	s := strings.TrimSpace(fmt.Sprint(m[key]))
	if s == "<nil>" {
		return ""
	}
	return s
}

// contactIDsByTag возвращает множество id контактов под тегом (все страницы).
func contactIDsByTag(ctx context.Context, c *onlyoffice.Client, tag string) (map[string]bool, error) {
	ids := map[string]bool{}
	const page = 100
	for start := 0; ; start += page {
		rows, total, err := c.ListContactsByTag(ctx, tag, page, start)
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			ids[onlyoffice.ContactID(r)] = true
		}
		if len(rows) == 0 || start+page >= total {
			return ids, nil
		}
	}
}
