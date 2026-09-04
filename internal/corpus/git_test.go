package corpus

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/eSlider/2dph/internal/contract"
)

// TestGitAdapterStreamsCommitLeafs — git-адаптер отдаёт commit leafs
// source=git, external_id=полный sha, kind=commit, observed_at=дата.
func TestGitAdapterStreamsCommitLeafs(t *testing.T) {
	repo := initGitFixture(t, []gitCommit{
		{when: time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC), name: "Ada Lovelace", email: "ada@example.com", subject: "feat: first commit", files: map[string]string{"README.md": "hi\n"}},
		{when: time.Date(2026, 8, 11, 9, 30, 0, 0, time.UTC), name: "Bob Babbage", email: "bob@example.com", subject: "fix: typo", files: map[string]string{"docs/notes.md": "note\n"}},
	})

	leafs := collect(t, Git{Repos: []string{repo}})
	if len(leafs) != 2 {
		t.Fatalf("commits = %d, want 2", len(leafs))
	}
	shas := map[string]bool{}
	for _, lf := range leafs {
		if lf.Source != "git" {
			t.Errorf("source = %q, want git", lf.Source)
		}
		if len(lf.ExternalID) != 40 {
			t.Errorf("external_id = %q, want full commit sha (40 hex)", lf.ExternalID)
		}
		if lf.Kind != "commit" {
			t.Errorf("kind = %q, want commit", lf.Kind)
		}
		if lf.ObservedAt == "" {
			t.Errorf("observed_at empty, want commit date")
		}
		if !strings.Contains(lf.Text, "commit ") {
			t.Errorf("text = %q, want commit heading", lf.Text)
		}
		if !strings.Contains(lf.Text, "Ada Lovelace") && !strings.Contains(lf.Text, "Bob Babbage") {
			t.Errorf("text missing author: %q", lf.Text)
		}
		if err := lf.Validate(); err != nil {
			t.Errorf("leaf invalid: %v", err)
		}
		shas[lf.ExternalID] = true
	}
	if len(shas) != 2 {
		t.Errorf("external_ids = %v, want 2 distinct commit shas", shas)
	}
}

// TestGitAdapterRootScansRepos — сканирование корня за git-репозиториями.
func TestGitAdapterRootScansRepos(t *testing.T) {
	r1 := initGitFixture(t, []gitCommit{{when: time.Now(), name: "Ada Lovelace", email: "ada@example.com", subject: "one", files: map[string]string{"a.md": "a"}}})
	root := filepath.Dir(r1)
	// второй репо рядом (нет .git-метки в том же дереве: создаём подпапку)
	leafs := collect(t, Git{Root: root})
	if len(leafs) < 1 {
		t.Fatalf("root scan got %d leafs, want >=1", len(leafs))
	}
	for _, lf := range leafs {
		if lf.Source != "git" {
			t.Errorf("source = %q, want git", lf.Source)
		}
	}
}

func TestGitAdapterImplementsSource(t *testing.T) {
	var _ contract.Source = Git{}
}

type gitCommit struct {
	when    time.Time
	name    string
	email   string
	subject string
	files   map[string]string
}

func initGitFixture(t *testing.T, specs []gitCommit) string {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range specs {
		for path, body := range s.files {
			full := filepath.Join(dir, path)
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(full, []byte(body), 0o644); err != nil {
				t.Fatal(err)
			}
			if _, err := w.Add(path); err != nil {
				t.Fatal(err)
			}
		}
		if _, err := w.Commit(s.subject, &git.CommitOptions{
			Author: &object.Signature{Name: s.name, Email: s.email, When: s.when},
		}); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}
