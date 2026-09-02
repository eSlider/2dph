package ooimport

// Офлайн-тесты чистой части идемпотентного прогона (N-1.2 #269):
// splitByExisting — распределение кандидатов на «новые» (создавать) и
// «уже есть в CRM» (skip, не перезаписываем). Повторный прогон при полном
// existing = 0 новых (приёмка #269).

import (
	"reflect"
	"testing"
)

func TestSplitByExistingAllNew(t *testing.T) {
	cs := []Contact{
		{Email: "bob@example.com", Given: "Bob"},
		{Email: "carol@example.com", Given: "Carol"},
	}
	fresh, matched := splitByExisting(cs, map[string]bool{})
	if matched != 0 {
		t.Errorf("matched = %d, want 0", matched)
	}
	if !reflect.DeepEqual(fresh, cs) {
		t.Errorf("fresh = %+v, want all", fresh)
	}
}

func TestSplitByExistingRepeatZeroNew(t *testing.T) {
	// повтор прогона: все кандидаты уже в CRM → 0 новых
	cs := []Contact{
		{Email: "bob@example.com", Given: "Bob"},
		{Email: "carol@example.com", Given: "Carol"},
		{Email: "dave@example.com", Given: "Dave"},
	}
	existing := map[string]bool{"bob@example.com": true, "carol@example.com": true, "dave@example.com": true}
	fresh, matched := splitByExisting(cs, existing)
	if len(fresh) != 0 {
		t.Errorf("fresh = %+v, want none (повтор = 0 новых)", fresh)
	}
	if matched != 3 {
		t.Errorf("matched = %d, want 3", matched)
	}
}

func TestSplitByExistingMixed(t *testing.T) {
	cs := []Contact{
		{Email: "bob@example.com"},
		{Email: "carol@example.com"},
		{Email: "dave@example.com"},
	}
	fresh, matched := splitByExisting(cs, map[string]bool{"carol@example.com": true})
	if matched != 1 {
		t.Errorf("matched = %d, want 1", matched)
	}
	if len(fresh) != 2 || fresh[0].Email != "bob@example.com" || fresh[1].Email != "dave@example.com" {
		t.Errorf("fresh = %+v, want bob,dave (порядок плана сохранён)", fresh)
	}
}

func TestSplitByExistingRequiresNormalizedKeys(t *testing.T) {
	// контракт: кандидаты (BuildPlan) и ключи индекса (BuildContactEmailIndex)
	// нормализованы в lowercase — split точный. Не-нормализованный ключ не
	// даёт ложного match (иначе повтор создал бы дубль).
	cs := []Contact{{Email: "bob@example.com"}}
	fresh, matched := splitByExisting(cs, map[string]bool{"BOB@example.com": true})
	if matched != 0 || len(fresh) != 1 {
		t.Errorf("matched = %d fresh = %+v: ключ не нормализован — не должен совпасть", matched, fresh)
	}
}
