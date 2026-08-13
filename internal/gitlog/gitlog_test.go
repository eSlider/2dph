package gitlog

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing"
	"github.com/go-git/go-git/v5/plumbing/object"
)

func TestLogReadsCommitsWithoutGitBinary(t *testing.T) {
	dir := initRepo(t, []commitSpec{
		{
			when:    time.Date(2026, 8, 10, 12, 0, 0, 0, time.FixedZone("CEST", 3600)),
			name:    "Ada Lovelace",
			email:   "ada@example.com",
			subject: "feat: first commit",
			files:   map[string]string{"README.md": "hi\n", "src/main.c": "int main(){}\n"},
		},
		{
			when:    time.Date(2026, 8, 11, 9, 30, 0, 0, time.FixedZone("CEST", 3600)),
			name:    "Bob Babbage",
			email:   "bob@example.com",
			subject: "fix: typo",
			files:   map[string]string{"docs/notes.md": "note\n"},
		},
	})

	cs, err := Log(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 2 {
		t.Fatalf("commits = %d, want 2", len(cs))
	}
	if cs[0].Subject != "fix: typo" {
		t.Fatalf("head subject = %q, want fix: typo", cs[0].Subject)
	}
	if cs[1].Author != "Ada Lovelace" || cs[1].Email != "ada@example.com" {
		t.Fatalf("author = %s <%s>", cs[1].Author, cs[1].Email)
	}
	sort.Strings(cs[1].Files)
	if got := cs[1].Files; len(got) != 2 || got[0] != "README.md" || got[1] != "src/main.c" {
		t.Fatalf("first commit files = %v", got)
	}
	if cs[0].Files[0] != "docs/notes.md" {
		t.Fatalf("second commit files = %v", cs[0].Files)
	}
}

func TestLogSkipsMerges(t *testing.T) {
	dir := initRepo(t, []commitSpec{{
		when: time.Now(), name: "Ada Lovelace", email: "ada@example.com",
		subject: "base", files: map[string]string{"a.txt": "a\n"},
	}})
	r, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	head, err := r.Head()
	if err != nil {
		t.Fatal(err)
	}
	c, err := r.CommitObject(head.Hash())
	if err != nil {
		t.Fatal(err)
	}
	// Second parent: duplicate the same tree so we do not need a real branch.
	merge := &object.Commit{
		Author:       object.Signature{Name: "Ada Lovelace", Email: "ada@example.com", When: time.Now()},
		Committer:    object.Signature{Name: "Ada Lovelace", Email: "ada@example.com", When: time.Now()},
		Message:      "merge",
		TreeHash:     c.TreeHash,
		ParentHashes: []plumbing.Hash{c.Hash, c.Hash},
	}
	obj := r.Storer.NewEncodedObject()
	if err := merge.Encode(obj); err != nil {
		t.Fatal(err)
	}
	h, err := r.Storer.SetEncodedObject(obj)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Storer.SetReference(plumbing.NewHashReference(head.Name(), h)); err != nil {
		t.Fatal(err)
	}

	cs, err := Log(dir, Options{})
	if err != nil {
		t.Fatal(err)
	}
	for _, x := range cs {
		if x.Subject == "merge" {
			t.Fatal("merge commit was not skipped")
		}
	}
	if len(cs) != 1 || cs[0].Subject != "base" {
		t.Fatalf("after skip merges: %+v", subjects(cs))
	}
}

func TestLogSinceAndLimit(t *testing.T) {
	old := time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	neu := time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC)
	dir := initRepo(t, []commitSpec{
		{when: old, name: "Ada Lovelace", email: "ada@example.com", subject: "old", files: map[string]string{"old.md": "x"}},
		{when: neu, name: "Ada Lovelace", email: "ada@example.com", subject: "new", files: map[string]string{"new.md": "y"}},
	})
	cs, err := Log(dir, Options{Since: neu.Add(-time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 || cs[0].Subject != "new" {
		t.Fatalf("since filter: %v", subjects(cs))
	}
	cs, err = Log(dir, Options{Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(cs) != 1 {
		t.Fatalf("limit=1 got %d", len(cs))
	}
}

func TestLeafShape(t *testing.T) {
	c := Commit{
		SHA:     "a1b2c3d4e5f6aaaa",
		Author:  "Ada Lovelace",
		Email:   "ada@example.com",
		Date:    "2026-08-10T12:00:00+01:00",
		Subject: "feat: first commit",
		Files:   []string{"README.md", "src/main.c"},
	}
	lf := ToLeaf(c, "sample-repo")
	if lf.Type != "commit" || lf.Repo != "sample-repo" {
		t.Fatalf("leaf meta = %+v", lf)
	}
	if lf.Source != "sample-repo@a1b2c3d4e5f6aaaa" {
		t.Fatalf("source = %s", lf.Source)
	}
	if lf.Related != "README.md,src/main.c" {
		t.Fatalf("related = %s", lf.Related)
	}
	if lf.Heading != "commit a1b2c3d4e5f6 — feat: first commit" {
		t.Fatalf("heading = %q", lf.Heading)
	}
	if !strings.Contains(lf.Text, "Ada Lovelace") || !strings.Contains(lf.Text, "README.md") {
		t.Fatalf("text = %s", lf.Text)
	}
}

func TestRepoNameFromOrigin(t *testing.T) {
	dir := initRepo(t, []commitSpec{{
		when: time.Now(), name: "Ada Lovelace", email: "ada@example.com",
		subject: "init", files: map[string]string{"README.md": "x"},
	}})
	r, err := git.PlainOpen(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := r.CreateRemote(&config.RemoteConfig{
		Name: "origin",
		URLs: []string{"https://git.example.com/eSlider/sample-repo.git"},
	}); err != nil {
		t.Fatal(err)
	}
	name, err := RepoName(dir)
	if err != nil {
		t.Fatal(err)
	}
	if name != "sample-repo" {
		t.Fatalf("RepoName = %q, want sample-repo", name)
	}
}

func TestRepoNameFallsBackToDir(t *testing.T) {
	dir := initRepo(t, []commitSpec{{
		when: time.Now(), name: "Ada Lovelace", email: "ada@example.com",
		subject: "init", files: map[string]string{"README.md": "x"},
	}})
	name, err := RepoName(dir)
	if err != nil {
		t.Fatal(err)
	}
	if name != filepath.Base(dir) {
		t.Fatalf("RepoName = %q, want %s", name, filepath.Base(dir))
	}
}

type commitSpec struct {
	when    time.Time
	name    string
	email   string
	subject string
	files   map[string]string
}

func initRepo(t *testing.T, specs []commitSpec) string {
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
			if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil && !os.IsExist(err) {
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

func subjects(cs []Commit) []string {
	out := make([]string, len(cs))
	for i, c := range cs {
		out[i] = c.Subject
	}
	return out
}
