// Package gitgraph — коннектор git-истории → граф 2dph (L-9.4 #233).
// Читает локальные репозитории git.produktor.io (eSlider/*, produktor/*)
// через internal/gitlog (go-git, без git-бинара) — тот же источник, что
// premises-корпус Leaf (source=git, kind=commit) — и мапит в
// brain.CommitInput: Commit-узел (id = repo:sha) + Person по email +
// ребро AUTHORED. Слой чистый (cgo-free): запись в Ladybug исполняет
// bin/git/graph.go через brain.UpsertCommits.
//
// Канон git-коммита: (repo, sha) — полный sha уникален в репозитории, один
// и тот же sha в разных репо (форк/копия истории) — разные узлы. repo —
// имя origin-remote (gitlog.RepoName), стабильное при переезде клона.
// Авторство — по Author (как gitlog/git-корпус); merge-коммиты gitlog.Log
// пропускает (тот же канон, что premises-слой Leaf).
package gitgraph

import (
	"context"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/brain"
	"github.com/eSlider/2dph/internal/gitlog"
)

// Repo — один источник git-фактов: путь клона и стабильное имя из
// origin-remote (fallback — имя каталога).
type Repo struct {
	Path string
	Name string
}

// Resolve собирает репозитории: явные пути (repos) или одноуровневый скан
// root за каталогами с .git (алгоритм corpus.Git.resolveRepos). Root без
// детей-репозиториев сам считается репозиторием. Репо, чьё имя не
// резолвится (битый .git), пропускается.
func Resolve(root string, repos []string) ([]Repo, error) {
	paths := repos
	if len(paths) == 0 {
		paths = scanRoot(root)
	}
	out := make([]Repo, 0, len(paths))
	for _, p := range paths {
		name, err := gitlog.RepoName(p)
		if err != nil && name == "" {
			continue
		}
		out = append(out, Repo{Path: p, Name: name})
	}
	return out, nil
}

// scanRoot ищет каталоги с .git среди прямых детей root; если таких нет —
// сам root (когда он репозиторий).
func scanRoot(root string) []string {
	if root == "" {
		root = "."
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		dir := filepath.Join(root, e.Name())
		if st, err := os.Stat(filepath.Join(dir, ".git")); err == nil && st.IsDir() {
			out = append(out, dir)
		}
	}
	if len(out) == 0 {
		if st, err := os.Stat(filepath.Join(root, ".git")); err == nil && st.IsDir() {
			out = append(out, root)
		}
	}
	return out
}

// RepoCommit — один коммит истории с репозиторием (контекст канона).
type RepoCommit struct {
	Repo   Repo
	Commit gitlog.Commit
}

// ReadAll читает историю репозиториев через gitlog.Log (skip merges — тот
// же канон, что git-корпус Leaf) и возвращает коммиты в порядке истории
// (HEAD → старые). Репо без читаемой истории пропускаются молча (как
// corpus.Git.Stream).
func ReadAll(ctx context.Context, repos []Repo, opt gitlog.Options) ([]RepoCommit, error) {
	var out []RepoCommit
	for _, r := range repos {
		cs, err := gitlog.Log(r.Path, opt)
		if err != nil {
			continue // репо без читаемой истории — пропускаем
		}
		for _, cm := range cs {
			out = append(out, RepoCommit{Repo: r, Commit: cm})
		}
	}
	return out, ctx.Err()
}

// CommitID — канон узла Commit: repo:sha (полный sha), формат как FileID
// (repo:path). Стабилен при переезде клона; одинаковый sha в разных репо —
// разные узлы.
func CommitID(repo, sha string) string {
	return repo + ":" + sha
}

// ToInputs мапит RepoCommit → brain.CommitInput: id = repo:sha, email
// lowercase (Person.id = email канон, как mail D-1 #257), dedup по
// (repo,sha) — повторный прогон не плодит дублей, сортировка по дате ASC
// (родители раньше; детерминизм отчёта/записи), tie-break по id.
func ToInputs(rcs []RepoCommit) []brain.CommitInput {
	seen := make(map[string]bool, len(rcs))
	out := make([]brain.CommitInput, 0, len(rcs))
	for _, rc := range rcs {
		c := rc.Commit
		key := CommitID(rc.Repo.Name, c.SHA)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, brain.CommitInput{
			ID:      key,
			Repo:    rc.Repo.Name,
			SHA:     c.SHA,
			Subject: c.Subject,
			Author:  c.Author,
			Email:   strings.ToLower(strings.TrimSpace(c.Email)),
			Date:    c.Date,
		})
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i].Date, out[j].Date
		ta, aok := parseRFC3339(a)
		tb, bok := parseRFC3339(b)
		if aok && bok {
			if !ta.Equal(tb) {
				return ta.Before(tb)
			}
		} else if aok != bok {
			return aok // непарсящиеся даты в конец
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func parseRFC3339(s string) (time.Time, bool) {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, false
	}
	return t, true
}

// RepoCount — строка раскладки dry-run по репозиторию.
type RepoCount struct {
	Repo string
	N    int
}

// Stats — числа dry-run импорта: раскладка по repo, уникальные автор-email
// (приближение Person-узлов до записи), коммиты со слабым subject.
type Stats struct {
	Total  int
	Repos  []RepoCount
	Emails int
	Weak   int
}

// ComputeStats считает числа dry-run до записи.
func ComputeStats(inputs []brain.CommitInput) Stats {
	s := Stats{Total: len(inputs)}
	byRepo := map[string]int{}
	emails := map[string]bool{}
	for _, in := range inputs {
		byRepo[in.Repo]++
		if in.Email != "" {
			emails[in.Email] = true
		}
		if WeakSubject(in.Subject) {
			s.Weak++
		}
	}
	for repo, n := range byRepo {
		s.Repos = append(s.Repos, RepoCount{Repo: repo, N: n})
	}
	sort.Slice(s.Repos, func(i, j int) bool { return s.Repos[i].Repo < s.Repos[j].Repo })
	s.Emails = len(emails)
	return s
}

// weakSubjects — служебные subject-ы без информации о работе. Git-facts
// маркирует такие выводы weaken/OPEN («трогал файлы», не «что именно») —
// границы дедукции L-9.4, docs/audit-recipes.md.
var weakSubjects = map[string]bool{
	"update": true, "updates": true, "updated": true, "updating": true,
	"fix": true, "fixes": true, "fixed": true,
	"wip": true, "minor": true, "cleanup": true, "chore": true,
	"init": true, "initial": true, "test": true, "typo": true, "format": true,
	"merge": true, "revert": true,
}

// WeakSubject — неинформативный subject коммита: пустой/пробельный или
// целиком служебное слово (Update/WIP/fix…). Содержательный subject
// («feat: mesh node», «fix: typo») слабым не считается.
func WeakSubject(subject string) bool {
	s := strings.ToLower(strings.TrimSpace(subject))
	if s == "" {
		return true
	}
	return weakSubjects[s]
}
