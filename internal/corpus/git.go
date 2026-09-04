package corpus

import (
	"context"
	"os"
	"path/filepath"
	"time"

	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/internal/gitlog"
	"github.com/eSlider/2dph/pkg/utils"
)

// Git — адаптер git-корпуса: история коммитов через go-git (без git-бинаря).
// source=git, external_id=полный commit sha, kind=commit, observed_at=дата.
// Единственный путь индексации git-истории (var/corpus/git и
// 2dph__corpus__git__* из docs исключены — дубль git устранён, #10).
type Git struct {
	Repos []string  // явные репозитории
	Root  string    // каталог, сканируется за .git-репозиториями
	Limit int       // max коммитов на репо (0 = все)
	Since time.Time // только коммиты после момента
}

func (g Git) Name() string { return "git" }

func (g Git) Stream(ctx context.Context, emit func(contract.Leaf) error) error {
	repos, err := g.resolveRepos()
	if err != nil {
		return err
	}
	opt := gitlog.Options{Limit: g.Limit, Since: g.Since}
	for _, rp := range repos {
		name, err := gitlog.RepoName(rp)
		if err != nil && name == "" {
			continue
		}
		cs, err := gitlog.Log(rp, opt)
		if err != nil {
			continue // репо без читаемой истории — пропускаем
		}
		for _, cm := range cs {
			lf := gitlog.ToLeaf(cm, name)
			text := lf.Heading + "\n\n" + lf.Text
			if err := emit(contract.Leaf{
				Source: "git", ExternalID: cm.SHA, Kind: "commit",
				Text: text, Root: "info", Confidence: "confirmed",
				How: "git-log", Loc: rp, ObservedAt: cm.Date,
			}); err != nil {
				return err
			}
		}
	}
	return ctx.Err()
}

// resolveRepos собирает репозитории: явные --repos, иначе скан --root,
// иначе корень проекта (utils.Root()).
func (g Git) resolveRepos() ([]string, error) {
	if len(g.Repos) > 0 {
		return g.Repos, nil
	}
	if g.Root == "" {
		g.Root = utils.Root()
	}
	entries, err := os.ReadDir(g.Root)
	if err != nil {
		return nil, err
	}
	var repos []string
	for _, e := range entries {
		dir := filepath.Join(g.Root, e.Name())
		if st, err := os.Stat(filepath.Join(dir, ".git")); err == nil && st.IsDir() {
			repos = append(repos, dir)
		}
	}
	if len(repos) == 0 {
		// корень сам является репозиторием
		if st, err := os.Stat(filepath.Join(g.Root, ".git")); err == nil && st.IsDir() {
			repos = append(repos, g.Root)
		}
	}
	return repos, nil
}
