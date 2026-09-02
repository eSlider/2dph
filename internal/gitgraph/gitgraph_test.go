package gitgraph

// Юнит-тесты коннектора git-истории → brain.CommitInput (L-9.4 #233),
// cgo-free: резолв репозиториев (root-скан/явные), чтение через gitlog,
// канон id = repo:sha, маппинг/нормализация email, сортировка по дате,
// dedup (идемпотентность повтора), статистика dry-run, слабые subject-ы.
// Git-фикстуры создаются go-git'ом (как gitlog_test.go), без git-бинара.

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/config"
	"github.com/go-git/go-git/v5/plumbing/object"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/gitlog"
)

// repoFixture — настоящий git-репо (go-git) с двумя коммитами и origin в
// подкаталоге name (base имени = name — для root-скана).
func repoFixture(t *testing.T, name, origin string) string {
	t.Helper()
	return repoFixtureIn(t, t.TempDir(), name, origin)
}

func repoFixtureIn(t *testing.T, parent, name, origin string) string {
	t.Helper()
	dir := filepath.Join(parent, name)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatal(err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	commit := func(subject, email string, when time.Time) {
		t.Helper()
		full := filepath.Join(dir, "README.md")
		if err := os.WriteFile(full, []byte(subject+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Add("README.md"); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Commit(subject, &git.CommitOptions{
			Author: &object.Signature{Name: "Ada Lovelace", Email: email, When: when},
		}); err != nil {
			t.Fatal(err)
		}
	}
	commit("feat: first", "Ada@Example.COM", time.Date(2026, 8, 10, 12, 0, 0, 0, time.FixedZone("CEST", 3600)))
	commit("fix: typo", "bob@example.com", time.Date(2026, 8, 11, 9, 30, 0, 0, time.UTC))
	if origin != "" {
		if _, err := r.CreateRemote(&config.RemoteConfig{Name: "origin", URLs: []string{origin}}); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// CommitID — канон узла Commit: repo:sha (полный sha), как FileID repo:path.
func TestCommitIDCanon(t *testing.T) {
	sha := "0123456789abcdef0123456789abcdef01234567"
	if got := CommitID("2dph", sha); got != "2dph:"+sha {
		t.Fatalf("CommitID = %q", got)
	}
	if CommitID("2dph", sha) == CommitID("gator", sha) {
		t.Fatal("same sha in different repos must produce different ids")
	}
}

// Маппинг RepoCommit → CommitInput: id = repo:sha, email lowercase (Person.id
// канон), поля автора/даты/subject сохраняются.
func TestToInputsMapping(t *testing.T) {
	in := []RepoCommit{{
		Repo: Repo{Path: "/tmp/demo", Name: "demo-repo"},
		Commit: gitlog.Commit{
			SHA:     "0123456789abcdef0123456789abcdef01234567",
			Author:  "Ada Lovelace",
			Email:   "Ada@Example.COM",
			Date:    "2026-08-10T12:00:00+01:00",
			Subject: "feat: mesh node",
		},
	}}
	got := ToInputs(in)
	if len(got) != 1 {
		t.Fatalf("inputs = %d, want 1", len(got))
	}
	c := got[0]
	if c.ID != "demo-repo:0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("id = %q", c.ID)
	}
	if c.Repo != "demo-repo" || c.SHA != in[0].Commit.SHA {
		t.Fatalf("repo/sha = %q/%q", c.Repo, c.SHA)
	}
	if c.Email != "ada@example.com" {
		t.Fatalf("email must be lowercased, got %q", c.Email)
	}
	if c.Author != "Ada Lovelace" || c.Subject != "feat: mesh node" || c.Date != "2026-08-10T12:00:00+01:00" {
		t.Fatalf("author/subject/date = %q/%q/%q", c.Author, c.Subject, c.Date)
	}
}

// Сортировка по дате ASC (родители раньше), tie-break по (repo, sha);
// dedup по (repo, sha) — повторный прогон даёт 0 новых входов.
func TestToInputsSortedAndDedup(t *testing.T) {
	shaA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	shaB := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	// 12:00+01:00 == 11:00Z — раньше, чем 09:30Z следующего дня нет;
	// для сортировки берём время, а не лексикографию строк.
	rcs := []RepoCommit{
		{Repo: Repo{Name: "demo"}, Commit: gitlog.Commit{
			SHA: shaB, Email: "b@example.com", Date: "2026-08-11T09:30:00Z", Subject: "b",
		}},
		{Repo: Repo{Name: "demo"}, Commit: gitlog.Commit{
			SHA: shaA, Email: "a@example.com", Date: "2026-08-10T12:00:00+01:00", Subject: "a",
		}},
		// дубль того же (repo,sha) — должен схлопнуться
		{Repo: Repo{Name: "demo"}, Commit: gitlog.Commit{
			SHA: shaA, Email: "a@example.com", Date: "2026-08-10T12:00:00+01:00", Subject: "a",
		}},
	}
	got := ToInputs(rcs)
	if len(got) != 2 {
		t.Fatalf("inputs = %d, want 2 (dedup)", len(got))
	}
	if got[0].SHA != shaA || got[1].SHA != shaB {
		t.Fatalf("order wrong: %s, %s (want oldest first)", got[0].SHA[:8], got[1].SHA[:8])
	}
	// повторный прогон поверх результата — стабилен (идемпотентность)
	again := ToInputs(append(rcs, rcs...))
	if len(again) != 2 {
		t.Fatalf("rerun grew inputs: %d", len(again))
	}
}

// Коммит без email: вход остаётся (Commit-узел без AUTHORED), паники нет.
func TestToInputsNoEmail(t *testing.T) {
	in := []RepoCommit{{Repo: Repo{Name: "demo"}, Commit: gitlog.Commit{
		SHA: "cccccccccccccccccccccccccccccccccccccccc", Email: "", Subject: "x",
	}}}
	got := ToInputs(in)
	if len(got) != 1 || got[0].Email != "" {
		t.Fatalf("no-email commit must stay an input: %+v", got)
	}
}

// WeakSubject: пустой/служебный subject → weaken (git-facts), содержательный
// (feat:/fix: typo/…#233) — accept.
func TestWeakSubject(t *testing.T) {
	weak := []string{"", "   ", "Update", "updates", "WIP", "minor", "fix", "cleanup"}
	for _, s := range weak {
		if !WeakSubject(s) {
			t.Fatalf("%q must be weak", s)
		}
	}
	strong := []string{"feat: mesh node", "fix: typo", "add knowledge-mesh-seed.yaml", "Update docs for api (#12)"}
	for _, s := range strong {
		if WeakSubject(s) {
			t.Fatalf("%q must not be weak", s)
		}
	}
}

// Stats: total, раскладка по repo (сортировка), уникальные email, weak.
func TestComputeStats(t *testing.T) {
	inputs := []brain.CommitInput{
		{Repo: "gator", Email: "ada@example.com", Subject: "feat: x"},
		{Repo: "gator", Email: "ada@example.com", Subject: "Update"},
		{Repo: "2dph", Email: "bob@example.com", Subject: "fix: typo"},
		{Repo: "2dph", Email: "ada@example.com", Subject: "wip"},
	}
	st := ComputeStats(inputs)
	if st.Total != 4 {
		t.Fatalf("total = %d", st.Total)
	}
	if len(st.Repos) != 2 || st.Repos[0].Repo != "2dph" || st.Repos[0].N != 2 || st.Repos[1].Repo != "gator" || st.Repos[1].N != 2 {
		t.Fatalf("repos = %+v", st.Repos)
	}
	if st.Emails != 2 {
		t.Fatalf("emails = %d", st.Emails)
	}
	if st.Weak != 2 {
		t.Fatalf("weak = %d", st.Weak)
	}
}

// Resolve: явные пути → Repo{Path, Name из origin}; root-скан подкаталогов с
// .git (одноуровневый, как corpus.Git); репо без origin → имя каталога.
func TestResolveExplicitAndScan(t *testing.T) {
	root := t.TempDir()
	dirA := repoFixtureIn(t, root, "demo-a", "https://git.example.com/eSlider/demo-a.git")
	dirB := repoFixtureIn(t, root, "demo-b", "")

	explicit, err := Resolve("", []string{dirA, dirB})
	if err != nil {
		t.Fatal(err)
	}
	if len(explicit) != 2 {
		t.Fatalf("explicit repos = %d, want 2", len(explicit))
	}
	if explicit[0].Name != "demo-a" || explicit[0].Path != dirA {
		t.Fatalf("repo A = %+v", explicit[0])
	}
	if explicit[1].Name != "demo-b" {
		t.Fatalf("repo B name = %q, want dir base %q", explicit[1].Name, "demo-b")
	}

	// root-скан тех же репо под общим корнем
	scanned, err := Resolve(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(scanned))
	for _, r := range scanned {
		names = append(names, r.Name)
	}
	sort.Strings(names)
	if len(scanned) != 2 || names[0] != "demo-a" || names[1] != "demo-b" {
		t.Fatalf("scan repos = %v (%d)", names, len(scanned))
	}
}

// Resolve: root сам является репо (нет детей с .git) → возвращает его.
func TestResolveRootIsRepo(t *testing.T) {
	dir := repoFixture(t, "solo", "")
	got, err := Resolve(dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Path != dir {
		t.Fatalf("root-as-repo = %+v", got)
	}
}

// ReadAll: читает историю go-git'ом (skip merges — gitlog.Log), эмитит
// RepoCommit с именем репо; репо без читаемой истории пропускается.
func TestReadAllCommits(t *testing.T) {
	dirA := repoFixture(t, "demo-a", "https://git.example.com/eSlider/demo-a.git")
	repos, err := Resolve("", []string{dirA})
	if err != nil {
		t.Fatal(err)
	}
	rcs, err := ReadAll(context.Background(), repos, gitlog.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if len(rcs) != 2 {
		t.Fatalf("commits = %d, want 2", len(rcs))
	}
	for _, rc := range rcs {
		if rc.Repo.Name != "demo-a" {
			t.Fatalf("repo name = %q", rc.Repo.Name)
		}
	}
	if rcs[0].Commit.Email != "bob@example.com" {
		t.Fatalf("head (newest) commit email = %q, want bob@example.com", rcs[0].Commit.Email)
	}
	if rcs[1].Commit.Email != "Ada@Example.COM" {
		t.Fatalf("oldest commit email = %q, want Ada@Example.COM", rcs[1].Commit.Email)
	}

	// Since-фильтр
	since := time.Date(2026, 8, 11, 0, 0, 0, 0, time.UTC)
	rcs, err = ReadAll(context.Background(), repos, gitlog.Options{Since: since})
	if err != nil {
		t.Fatal(err)
	}
	if len(rcs) != 1 || rcs[0].Commit.Subject != "fix: typo" {
		t.Fatalf("since filter: %+v", rcs)
	}
}
