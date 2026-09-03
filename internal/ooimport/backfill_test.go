package ooimport

// Офлайн-тесты аддитивного дописывания matched-контактам (N-1.4 #271,
// решение владельца на #267): существующий по email контакт получает тег
// 2dph:network:<target> (если отсутствует) + about-premises-сводку (только
// если about пуст) — ручные правки не перезаписываются. cgo-free.

import (
	"reflect"
	"testing"
)

func TestBackfillChangesAdditive(t *testing.T) {
	const wantAbout = "4 писем / 2 тредов / 1 ответов, период 2026-01-01..2026-06-30"
	cases := []struct {
		name      string
		hasTag    bool
		about     string
		wantAbout string
		want      BackfillChange
	}{
		{"нет тега и нет about → тег + сводка", false, "", wantAbout, BackfillChange{AddTag: true, SetAbout: wantAbout}},
		{"тег есть, about пуст → только сводка", true, "", wantAbout, BackfillChange{SetAbout: wantAbout}},
		{"тега нет, about ручной → только тег", false, "ручная заметка", wantAbout, BackfillChange{AddTag: true}},
		{"всё есть → ничего", true, "ручная заметка", wantAbout, BackfillChange{}},
		{"about пробельный → сводка (не ручная правка)", false, "  \n\t ", wantAbout, BackfillChange{AddTag: true, SetAbout: wantAbout}},
		{"тег есть, about пуст, сводки нет → ничего", true, "", "", BackfillChange{}},
	}
	for _, c := range cases {
		got := BackfillChanges(c.hasTag, c.about, c.wantAbout)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: BackfillChanges(%v, %q, %q) = %+v, want %+v", c.name, c.hasTag, c.about, c.wantAbout, got, c.want)
		}
	}
}

func TestBackfillChangesKeepsManualAbout(t *testing.T) {
	// ручной about (в т.ч. Org из импорта #85) не перезаписывается сводкой
	got := BackfillChanges(false, "WhereGroup GmbH", "сводка")
	if got.AddTag != true || got.SetAbout != "" {
		t.Errorf("BackfillChanges = %+v, want тег без сводки", got)
	}
}

func TestBackfillChangeEmpty(t *testing.T) {
	if !(BackfillChange{}).Empty() {
		t.Error("zero BackfillChange must be empty")
	}
	if (BackfillChange{AddTag: true}).Empty() {
		t.Error("AddTag change must not be empty")
	}
	if (BackfillChange{SetAbout: "x"}).Empty() {
		t.Error("SetAbout change must not be empty")
	}
}
