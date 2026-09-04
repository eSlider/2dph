package contract

import (
	"context"
	"strings"
	"testing"
)

// Фикстуры Alice/Bob — синтетические, без PII.
func aliceLeaf() Leaf {
	return Leaf{
		Source:     "mail",
		ExternalID: "msg-42",
		Kind:       "mail",
		Text:       "Alice wrote: ship the report",
		Root:       "facts",
		Confidence: "confirmed",
		ObservedAt: "2026-08-31T09:15:00Z",
	}
}

func bobLeaf() Leaf {
	l := aliceLeaf()
	l.ExternalID = "msg-43"
	l.Text = "Bob replied: report shipped"
	return l
}

func TestValidateRequired(t *testing.T) {
	// пустой leaf отвергается на границе записи
	if err := (Leaf{}).Validate(); err == nil {
		t.Fatal("empty leaf: want error")
	}
	cases := []struct {
		name string
		mut  func(*Leaf)
	}{
		{"source", func(l *Leaf) { l.Source = "" }},
		{"external_id", func(l *Leaf) { l.ExternalID = "" }},
		{"kind", func(l *Leaf) { l.Kind = "" }},
		{"text", func(l *Leaf) { l.Text = "" }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			l := aliceLeaf()
			tc.mut(&l)
			err := l.Validate()
			if err == nil {
				t.Fatalf("missing %s: want error", tc.name)
			}
			if !strings.Contains(err.Error(), tc.name) {
				t.Fatalf("error %q does not name %s", err, tc.name)
			}
		})
	}
}

func TestValidateOK(t *testing.T) {
	if err := aliceLeaf().Validate(); err != nil {
		t.Fatalf("valid leaf: %v", err)
	}
}

func TestContentHashDeterministic(t *testing.T) {
	a := aliceLeaf().ContentHash()
	b := aliceLeaf().ContentHash()
	if a != b {
		t.Fatalf("same content hashed differently: %s vs %s", a, b)
	}
}

func TestContentHashLen32(t *testing.T) {
	if h := aliceLeaf().ContentHash(); len(h) != 32 {
		t.Fatalf("ContentHash len=%d, want 32", len(h))
	}
}

// Ключевая семантика G-8.0: observed_at НЕ входит в dedup-ключ. Тот же контент,
// записанный позже, — та же версия (первый observed_at сохраняется).
func TestContentHashExcludesObservedAt(t *testing.T) {
	a := aliceLeaf()
	b := aliceLeaf()
	b.ObservedAt = "2026-09-01T10:00:00Z"
	if a.ContentHash() != b.ContentHash() {
		t.Fatal("observed_at must not change ContentHash (gator ContentHash semantics)")
	}
	// заодно остальные темпоральные/мета-поля не влияют на ключ
	for _, f := range []struct{ name, val string }{
		{"Root", "info"}, {"Confidence", "inferred"}, {"SourceRev", "abc123"},
		{"How", "git-log"}, {"Loc", "other"}, {"ValidFrom", "2026-01-01"}, {"ValidTo", "2026-12-31"},
	} {
		c := aliceLeaf()
		switch f.name {
		case "Root":
			c.Root = f.val
		case "Confidence":
			c.Confidence = f.val
		case "SourceRev":
			c.SourceRev = f.val
		case "How":
			c.How = f.val
		case "Loc":
			c.Loc = f.val
		case "ValidFrom":
			c.ValidFrom = f.val
		case "ValidTo":
			c.ValidTo = f.val
		}
		if a.ContentHash() != c.ContentHash() {
			t.Fatalf("%s must not change ContentHash", f.name)
		}
	}
}

// Изменение любого из identity-полей или контента даёт новый ключ.
func TestContentHashSensitivity(t *testing.T) {
	base := aliceLeaf()
	muts := []struct {
		name string
		leaf Leaf
	}{
		{"text", bobLeaf()},
		{"source", Leaf{Source: "git", ExternalID: "msg-42", Kind: "mail", Text: aliceLeaf().Text}},
		{"external_id", Leaf{Source: "mail", ExternalID: "msg-99", Kind: "mail", Text: aliceLeaf().Text}},
		{"kind", Leaf{Source: "mail", ExternalID: "msg-42", Kind: "chat", Text: aliceLeaf().Text}},
	}
	for _, m := range muts {
		t.Run(m.name, func(t *testing.T) {
			if m.leaf.ContentHash() == base.ContentHash() {
				t.Fatalf("%s change must yield a new ContentHash", m.name)
			}
		})
	}
}

// Согласованность с gator: разные источники/корпуса не пересекаются по ключу.
func TestContentHashDistinctSources(t *testing.T) {
	mail := Leaf{Source: "mail", ExternalID: "msg-42", Kind: "mail", Text: "same"}
	chat := Leaf{Source: "chat", ExternalID: "msg-42", Kind: "chat", Text: "same"}
	if mail.ContentHash() == chat.ContentHash() {
		t.Fatal("same external id in different sources must not collide")
	}
}

// P-9.3 #5.3: ContentHash нормализует text внутри себя — один контент из
// разных путей (CRLF vs LF, хвостовые пробелы, невалидный UTF-8) даёт один
// id. Это и есть обещание «id стабилен между источниками».
func TestContentHashNormalizesText(t *testing.T) {
	base := aliceLeaf()
	variants := []Leaf{
		{Source: "mail", ExternalID: "msg-42", Kind: "mail", Text: "Alice wrote: ship the report\r\n"},
		{Source: "mail", ExternalID: "msg-42", Kind: "mail", Text: "  Alice wrote: ship the report  "},
		{Source: "mail", ExternalID: "msg-42", Kind: "mail", Text: "\n\nAlice wrote: ship the report\n\n\n"},
	}
	for i, v := range variants {
		if v.ContentHash() != base.ContentHash() {
			t.Fatalf("variant %d (whitespace/CRLF/UTF-8) must hash equal to the normalized base", i)
		}
	}
}

func TestNormalizeText(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"plain", "plain"},
		{"  padded  ", "padded"},
		{"a\r\nb", "a\nb"},
		{"\n\na\n\n\n", "a"},
		{"x\xffy", "x\uFFFdy"}, // невалидный байт → U+FFFD
		{"", ""},
	}
	for _, c := range cases {
		if got := NormalizeText(c.in); got != c.want {
			t.Errorf("NormalizeText(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// P-9.3: каждый корпус — адаптер по контракту gator Source.
// Компилируемая проверка: любая реализация Source должна отдавать Name и
// Stream(ctx, emit).
func TestSourceInterface(t *testing.T) {
	var _ Source = fakeSource{}
}

type fakeSource struct{}

func (fakeSource) Name() string { return "fake" }
func (fakeSource) Stream(ctx context.Context, emit func(Leaf) error) error {
	return emit(Leaf{Source: "fake", ExternalID: "1", Kind: "reference", Text: "x"})
}
