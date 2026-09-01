package corpus

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/contract"
)

// TestDocsDefaultsAndExtraDirs — defaults (README/docs) + --corpus dir дают
// leafs source=docs, external_id=rel-путь, kind=reference/seed, loc=abs.
func TestDocsDefaultsAndExtraDirs(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Repo\n\n## Leaf\n\nhello docs corpus\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(root, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "docs", "runbook.md"), []byte("# Runbook\n\n## Step\n\ndo the thing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	extra := filepath.Join(root, "extra")
	if err := os.MkdirAll(filepath.Join(extra, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extra, "sub", "notes.md"), []byte("# Notes\n\n## One\n\nnote one\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extra, "seed.yaml"), []byte("a: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	leafs := collect(t, Docs{Root: root, ExtraDirs: []string{extra}})
	if len(leafs) == 0 {
		t.Fatal("docs fixture must yield leafs")
	}
	var kinds = map[string]int{}
	var ids = map[string]bool{}
	for _, lf := range leafs {
		if lf.Source != "docs" {
			t.Errorf("source = %q, want docs", lf.Source)
		}
		if lf.ExternalID == "" {
			t.Errorf("external_id empty for %s", lf.Loc)
		}
		if filepath.IsAbs(lf.ExternalID) {
			t.Errorf("external_id %q must be a stable relative ref, not abs path", lf.ExternalID)
		}
		if !filepath.IsAbs(lf.Loc) {
			t.Errorf("loc %q must be the abs evidence path", lf.Loc)
		}
		if !strings.Contains(lf.Text, "\n\n") {
			t.Errorf("text %q must be heading + body (double newline)", lf.Text)
		}
		if err := lf.Validate(); err != nil {
			t.Errorf("leaf invalid: %v", err)
		}
		kinds[lf.Kind]++
		ids[lf.ContentHash()] = true
	}
	if kinds["reference"] == 0 {
		t.Errorf("no reference leafs, kinds=%v", kinds)
	}
	if kinds["seed"] == 0 {
		t.Errorf("no seed leafs (yaml), kinds=%v", kinds)
	}
	// README, docs/runbook, extra/sub/notes, extra/seed.yaml
	if len(ids) != 4 {
		t.Errorf("got %d distinct leafs, want 4: %v", len(ids), ids)
	}
}

// P-9.3: docs-адаптер исключает чужие корпуса (git/mail/chats прямые подпапки
// --corpus корня) и старые git-md (2dph__corpus__git__*) — устранение дубля git.
func TestDocsSkipsOtherCorporaAndGitDup(t *testing.T) {
	extra := t.TempDir()
	for _, sub := range []string{"git", "mail", "chats"} {
		d := filepath.Join(extra, sub)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "x.md"), []byte("# X\n\n## Y\n\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(filepath.Join(extra, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(extra, "docs", "2dph__corpus__git__abc.md"), []byte("# git md\n\n## dup\n\nold git md dump\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// легитимный docs-файл рядом — должен остаться
	if err := os.WriteFile(filepath.Join(extra, "docs", "runbook.md"), []byte("# Runbook\n\n## Step\n\nok\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	leafs := collect(t, Docs{Root: t.TempDir(), ExtraDirs: []string{extra}})
	for _, lf := range leafs {
		loc := filepath.ToSlash(lf.Loc)
		for _, drop := range []string{"/git/", "/mail/", "/chats/", "corpus__git"} {
			if strings.Contains(loc, drop) {
				t.Errorf("docs adapter indexed foreign corpus path %s (drop %s)", loc, drop)
			}
		}
	}
	if len(leafs) != 1 {
		t.Fatalf("got %d leafs, want only docs/runbook.md", len(leafs))
	}
	if !strings.Contains(filepath.ToSlash(leafs[0].Loc), "docs/runbook.md") {
		t.Errorf("kept %s, want docs/runbook.md", leafs[0].Loc)
	}
}

// TestDocsSkipsNoiseAndSecrets — indexable-фильтр (var/, .git, секреты).
func TestDocsSkipsNoiseAndSecrets(t *testing.T) {
	extra := t.TempDir()
	for _, sub := range []string{"docs", "var", ".git", "_archive", "node_modules"} {
		d := filepath.Join(extra, sub)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "x.md"), []byte("# X\n\n## Y\n\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(extra, "passwords.csv.md"), []byte("# P\n\n## Q\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	leafs := collect(t, Docs{Root: t.TempDir(), ExtraDirs: []string{extra}})
	if len(leafs) == 0 {
		t.Fatal("at least docs/x.md must be indexed")
	}
	for _, lf := range leafs {
		for _, drop := range []string{"var/", ".git/", "_archive/", "node_modules/", "passwords.csv.md"} {
			if strings.Contains(filepath.ToSlash(lf.Loc), drop) {
				t.Errorf("secret/noise path leaked into corpus: %s", lf.Loc)
			}
		}
	}
}

// TestDocsAdapterNoDefaults — NoDefaults пропускает README/docs корень.
func TestDocsAdapterNoDefaults(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "README.md"), []byte("# Repo\n\n## L\n\ndefault leaf\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	extra := t.TempDir()
	if err := os.WriteFile(filepath.Join(extra, "only.md"), []byte("# Only\n\n## L\n\nextra leaf\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	leafs := collect(t, Docs{Root: root, NoDefaults: true, ExtraDirs: []string{extra}})
	for _, lf := range leafs {
		if strings.Contains(filepath.ToSlash(lf.Loc), "README.md") {
			t.Errorf("NoDefaults must skip README, got %s", lf.Loc)
		}
	}
	if len(leafs) != 1 {
		t.Errorf("got %d leafs, want only the extra dir", len(leafs))
	}
}

// compile-time: Docs/и т.п. реализуют контракт Source.
func TestDocsImplementsSource(t *testing.T) {
	var _ contract.Source = Docs{}
	_ = context.Background
}
