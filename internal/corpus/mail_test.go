package corpus

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/contract"
)

// writeMailFixture создаёт синтетическое письмо (Alice/Bob, без PII) в
// корпус-раскладке <root>/<rel>/<id>/message.{md,json} — той же, что live
// (var/corpus/mail) и legacy (var/mail) корпуса.
func writeMailFixture(t *testing.T, root, rel, id, subject, date, msgBody string) {
	t.Helper()
	dir := filepath.Join(root, rel, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	md := fmt.Sprintf("---\nid: %q\nfolder: %q\nsource: \"raw-email\"\nsubject: %q\nfrom: \"Alice <alice@example.com>\"\nto: \"Bob <bob@example.com>\"\ndate: %q\ntype: mail\n---\n\n# %s\n\n%s\n", id, rel, subject, date, subject, msgBody)
	if err := os.WriteFile(filepath.Join(dir, "message.md"), []byte(md), 0o644); err != nil {
		t.Fatal(err)
	}
	j := fmt.Sprintf(`{"source":"raw-email","id":%q,"folder":%q,"subject":%q,"from":"Alice <alice@example.com>","to":"Bob <bob@example.com>","receivedAt":%q}`,
		id, rel, subject, date+"T10:00:00Z")
	if err := os.WriteFile(filepath.Join(dir, "message.json"), []byte(j), 0o644); err != nil {
		t.Fatal(err)
	}
}

func collect(t *testing.T, s contract.Source) []contract.Leaf {
	t.Helper()
	var out []contract.Leaf
	if err := s.Stream(context.Background(), func(l contract.Leaf) error {
		out = append(out, l)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	return out
}

// TestMailAdapterStreamsLiveAndLegacy — оба корпуса (live + legacy) отдаются
// одним адаптером, source=mail, external_id=content-address, kind=mail.
func TestMailAdapterStreamsLiveAndLegacy(t *testing.T) {
	root := t.TempDir()
	writeMailFixture(t, root, "var/mail/gmail", "aaaaaaaaaaaaaaaa", "Legacy greetings", "2020-01-02", "hello from the legacy corpus")
	writeMailFixture(t, root, "var/corpus/mail/inbox", "1001", "Live greeting", "2026-08-01", "hello from the live inbox")

	leafs := collect(t, Mail{Root: root})
	if len(leafs) != 2 {
		t.Fatalf("got %d leafs, want 2 (one per root)", len(leafs))
	}
	seen := map[string]bool{}
	for _, lf := range leafs {
		if lf.Source != "mail" {
			t.Errorf("source = %q, want mail", lf.Source)
		}
		if len(lf.ExternalID) != 16 {
			t.Errorf("external_id = %q, want 16-hex content address", lf.ExternalID)
		}
		if lf.Kind != "mail" {
			t.Errorf("kind = %q, want mail", lf.Kind)
		}
		if !strings.HasSuffix(filepath.ToSlash(lf.Loc), "message.md") {
			t.Errorf("loc = %q, want message.md path", lf.Loc)
		}
		if !strings.Contains(lf.Text, "greeting") {
			t.Errorf("text = %q, want subject heading", lf.Text)
		}
		if err := lf.Validate(); err != nil {
			t.Errorf("leaf invalid: %v", err)
		}
		seen[lf.Loc] = true
	}
	if len(seen) != 2 {
		t.Errorf("leafs must come from both roots, got %v", seen)
	}
}

// P-9.3 #5.2: один message в live и legacy корпусах (разные id-директории,
// одинаковый контент) → один external_id (content address) и один ContentHash
// → автоматический dedup при записи.
func TestMailAdapterContentHashMergesAcrossRoots(t *testing.T) {
	root := t.TempDir()
	writeMailFixture(t, root, "var/mail/archive/tb-backup-128g", "bbbbbbbbbbbbbbbb", "Re: project", "2021-03-04", "the body is identical")
	writeMailFixture(t, root, "var/corpus/mail/pst", "777", "Re: project", "2021-03-04", "the body is identical")

	leafs := collect(t, Mail{Root: root})
	if len(leafs) != 2 {
		t.Fatalf("got %d leafs, want 2 (адаптер не дедуплицирует — дедуп на id при записи)", len(leafs))
	}
	a, b := leafs[0], leafs[1]
	if a.ExternalID != b.ExternalID {
		t.Fatalf("external_id differs: %q vs %q — content address must be equal", a.ExternalID, b.ExternalID)
	}
	if a.ContentHash() != b.ContentHash() {
		t.Fatalf("ContentHash differs across roots: %s vs %s — один message должен давать один id", a.ContentHash(), b.ContentHash())
	}
}

// TestMailAdapterAttachment — вложения индексируются как отдельные leafs
// (kind=reference), Loc указывает на файл вложения.
func TestMailAdapterAttachment(t *testing.T) {
	root := t.TempDir()
	writeMailFixture(t, root, "var/mail/inbox", "3001", "With attachment", "2026-08-01", "see attached")
	attDir := filepath.Join(root, "var", "mail", "inbox", "3001", "attachments")
	if err := os.MkdirAll(attDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(attDir, "report.pdf.md"), []byte("# report\n\nthe report text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	leafs := collect(t, Mail{Root: root})
	found := false
	for _, lf := range leafs {
		if strings.Contains(filepath.ToSlash(lf.Loc), "attachments/report.pdf.md") {
			found = true
			if lf.Kind != "reference" {
				t.Errorf("attachment kind = %q, want reference", lf.Kind)
			}
			if !strings.Contains(lf.Text, "the report text") {
				t.Errorf("attachment text = %q", lf.Text)
			}
		}
	}
	if !found {
		t.Errorf("attachment leaf not emitted; got %d leafs", len(leafs))
	}
}

// TestMailAdapterSkipsContentLessLeafs — content-less leafs не эмитятся:
// CR-only/пустые attachments/*.md после нормализации дают text=="", такой leaf
// режется UpsertLeaf с "leaf needs text and source" и валит rebuild (#243).
// Валидные письма и вложения проходят.
func TestMailAdapterSkipsContentLessLeafs(t *testing.T) {
	root := t.TempDir()
	writeMailFixture(t, root, "var/mail/inbox", "4001", "With attachments", "2026-08-01", "the body is fine")

	attDir := filepath.Join(root, "var", "mail", "inbox", "4001", "attachments")
	if err := os.MkdirAll(attDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// CR-only: строки-каретки вычищаются TrimSpace → пустой text после normalize.
	if err := os.WriteFile(filepath.Join(attDir, "cr-only.md"), []byte("\r\n\r\n\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Полностью пустой файл — тоже content-less.
	if err := os.WriteFile(filepath.Join(attDir, "empty.md"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	// Валидное вложение обязано пройти (не выпасть по ошибке фильтра).
	if err := os.WriteFile(filepath.Join(attDir, "report.pdf.md"), []byte("# report\n\nthe report text\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	leafs := collect(t, Mail{Root: root})
	// message.md + валидный attachment; CR-only и empty пропущены.
	if len(leafs) != 2 {
		t.Fatalf("got %d leafs, want 2 (message + valid attachment; content-less skipped)", len(leafs))
	}
	for _, lf := range leafs {
		if err := lf.Validate(); err != nil {
			t.Errorf("leaf invalid: %v", err)
		}
	}
	for _, lf := range leafs {
		if !strings.Contains(filepath.ToSlash(lf.Loc), "attachments/") {
			continue
		}
		if !strings.Contains(lf.Text, "the report text") {
			t.Errorf("attachment leaf %s: got text %q, want the report text", lf.Loc, lf.Text)
		}
	}
}

// TestMailAdapterSince — фильтр по дате письма.
func TestMailAdapterSince(t *testing.T) {
	root := t.TempDir()
	writeMailFixture(t, root, "var/mail/gmail", "cccccccccccccccc", "Old message", "2019-06-01", "ancient")
	writeMailFixture(t, root, "var/corpus/mail/inbox", "2002", "New message", "2026-08-25", "fresh")

	leafs := collect(t, Mail{Root: root, Since: "2026-01-01"})
	if len(leafs) != 1 {
		t.Fatalf("since filter got %d leafs, want 1", len(leafs))
	}
	if !strings.Contains(leafs[0].Text, "New message") {
		t.Errorf("since filter kept %q, want the 2026 message", leafs[0].Text)
	}
}
