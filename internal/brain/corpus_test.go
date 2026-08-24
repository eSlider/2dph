//go:build cgo && system_ladybug

package brain

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestIndexableFilter(t *testing.T) {
	for _, keep := range []string{
		"/users/alice/projects/eSlider/docs/runbook.md",
		"/users/bob/projects/inventar/docs/catalog/clients.yaml",
		"/repo/README.md",
	} {
		if !indexable(keep) {
			t.Errorf("expected %q to be indexable", keep)
		}
	}
	for _, drop := range []string{
		"/users/alice/projects/eSlider/_archive/energy/notes.md",
		"/app/node_modules/readme.md",
		"/app/.venv/lib/foo.md",
		"/app/var/corpus/mail/msg.md",
		"/app/.git/HEAD",
		"/users/alice/projects/secret-trust-wallet.png.md",
		"/users/alice/projects/creds/allowlist.yaml",
		"/users/alice/projects/brave/Passwords.csv.md",
		"/users/alice/.ssh/id_rsa.md",
		"/users/alice/projects/certs/cert.p12.md",
		"/users/alice/projects/app/.env.md",
	} {
		if indexable(drop) {
			t.Errorf("expected %q to be filtered", drop)
		}
	}
}

func TestLoadCorpusPathFixture(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "note.md"), []byte("# Fixture\n\n## Leaf\n\nhello corpus\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	leafs, err := LoadCorpusPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(leafs) == 0 {
		t.Fatal("fixture dir must yield at least one leaf")
	}
	found := false
	for _, lf := range leafs {
		if lf.Heading == "Leaf" && strings.Contains(lf.Text, "hello corpus") {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected Leaf heading + body in corpus, got %+v", leafs)
	}
}

func TestLoadCorpusPathSkipsNoiseAndSecrets(t *testing.T) {
	dir := t.TempDir()
	for _, sub := range []string{"docs", "var", ".git", "_archive", "node_modules"} {
		d := filepath.Join(dir, sub)
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(d, "x.md"), []byte("# X\n\n## Y\n\nbody\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "passwords.csv.md"), []byte("# P\n\n## Q\n\nbody\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	leafs, err := LoadCorpusPath(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(leafs) == 0 {
		t.Fatal("at least docs/x.md must be indexed")
	}
	for _, lf := range leafs {
		for _, drop := range []string{"var/", ".git/", "_archive/", "node_modules/", "passwords.csv.md"} {
			if strings.Contains(lf.Source, drop) {
				t.Errorf("secret/noise path leaked into corpus: %s", lf.Source)
			}
		}
	}
}