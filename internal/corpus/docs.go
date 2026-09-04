package corpus

import (
	"context"
	"os"
	"path/filepath"
	"strings"

	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/internal/markdown"
	"github.com/eSlider/2dph/pkg/utils"
)

// docsDefaults — корневые docs-пути, индексируемые по умолчанию.
var docsDefaults = []string{"README.md", "PLAN.md", "AGENTS.md", "docs", "skills"}

// Docs — адаптер docs-корпуса: markdown (README/PLAN/AGENTS/docs/skills +
// любые --corpus пути) и yaml-seed. source=docs, external_id=rel-путь
// (стабилен между машинами), loc=абс-путь (evidence pointer).
type Docs struct {
	Root       string   // repo root; пусто → utils.Root()
	ExtraDirs  []string // доп. пути (--corpus): md-файлы/директории + yaml
	NoDefaults bool     // пропустить docsDefaults
}

func (d Docs) Name() string { return "docs" }

func (d Docs) Stream(ctx context.Context, emit func(contract.Leaf) error) error {
	if d.Root == "" {
		d.Root = utils.Root()
	}
	if !d.NoDefaults {
		var files []string
		for _, entry := range docsDefaults {
			p := filepath.Join(d.Root, entry)
			st, err := os.Stat(p)
			if err != nil {
				continue
			}
			if st.IsDir() {
				mds, err := markdown.WalkMarkdown(p)
				if err != nil {
					return err
				}
				files = append(files, mds...)
			} else {
				files = append(files, p)
			}
		}
		for _, f := range files {
			if err := emitDocsFiles(d.Root, f, emit); err != nil {
				return err
			}
		}
	}
	for _, dir := range d.ExtraDirs {
		if err := emitExtraDir(dir, emit); err != nil {
			return err
		}
	}
	return ctx.Err()
}

// emitDocsFiles индексирует один файл по умолчанию (abs-путь → rel external_id).
func emitDocsFiles(root, path string, emit func(contract.Leaf) error) error {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		rel = filepath.Base(path)
	}
	for _, lf := range markdownLeafs(path, "eSlider/2dph", "kb/index") {
		if err := emit(docsLeaf(lf, rel, path)); err != nil {
			return err
		}
	}
	return nil
}

// emitExtraDir индексирует --corpus путь: md-файлы (+ yaml-seed) с фильтром
// indexable и исключением чужих корпусов (git/mail/chats).
func emitExtraDir(source string, emit func(contract.Leaf) error) error {
	st, err := os.Stat(source)
	if err != nil {
		return nil
	}
	if !st.IsDir() {
		// одиночный md-файл
		rel := filepath.Base(source)
		for _, lf := range markdownLeafs(source, filepath.Base(filepath.Dir(source)), "kb/index") {
			if err := emit(docsLeaf(lf, rel, source)); err != nil {
				return err
			}
		}
		return nil
	}
	return filepath.Walk(source, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			if os.IsPermission(err) {
				return nil
			}
			return err
		}
		if info.IsDir() {
			// ponytail: прямые подпапки-корпуса (git/mail/chats) исключаются —
			// старый --corpus /corpus индексировал их как docs (дубль git, #10);
			// легитимный docs/git/ редок, миримся.
			if p != source && foreignCorpusDir(filepath.Base(p)) {
				return filepath.SkipDir
			}
			return nil
		}
		if !indexable(p) {
			return nil
		}
		rel, rerr := filepath.Rel(source, p)
		if rerr != nil {
			rel = filepath.Base(p)
		}
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".md", ".markdown":
			for _, lf := range markdownLeafs(p, filepath.Base(source), "kb/index") {
				if err := emit(docsLeaf(lf, rel, p)); err != nil {
					return err
				}
			}
		case ".yaml", ".yml":
			raw, rerr := os.ReadFile(p)
			if rerr != nil {
				return nil
			}
			text := string(raw)
			if len(text) > 20000 {
				text = text[:20000]
			}
			heading := strings.TrimSuffix(filepath.Base(p), ext)
			if err := emit(contract.Leaf{
				Source: "docs", ExternalID: rel, Kind: "seed",
				Text: heading + "\n\n" + text, Root: "info", Confidence: "confirmed",
				How: "kb/index", Loc: p,
			}); err != nil {
				return err
			}
		}
		return nil
	})
}

// foreignCorpusDir — имя прямого подкаталога, который принадлежит другому
// адаптеру корпуса (git/mail/chats), а не docs.
func foreignCorpusDir(name string) bool {
	switch strings.ToLower(name) {
	case "git", "mail", "chats":
		return true
	}
	return false
}

// docsLeaf строит contract.Leaf для markdown-чанка: text = heading+body
// (единообразно со всеми корпусами), external_id = rel-путь.
func docsLeaf(lf markdown.Leaf, externalID, loc string) contract.Leaf {
	kind := lf.Type
	if kind == "" {
		kind = "reference"
	}
	text := lf.Heading + "\n\n" + lf.Text
	if lf.Heading == "" {
		text = lf.Text
	}
	return contract.Leaf{
		Source: "docs", ExternalID: externalID, Kind: kind,
		Text: text, Root: "info", Confidence: "confirmed",
		How: "kb/index", Loc: loc,
	}
}

// markdownLeafs парсит md-файл в чанки (frontmatter type/heading/text).
func markdownLeafs(path, repo, how string) []markdown.Leaf {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	return markdown.ToAll(string(raw), path, repo)
}

// indexable держит vendor-шум и секрето-подобные пути вне мозга. Перенесён
// из internal/brain (P-9.3) — фильтр docs-корпуса.
func indexable(path string) bool {
	segs := strings.Split(filepath.ToSlash(path), "/")
	for _, s := range segs {
		if s == "" {
			continue
		}
		low := strings.ToLower(s)
		for _, d := range []string{"node_modules", ".venv", "venv", ".git", "_archive",
			"var", "dist", "build", ".next", ".cache", "target"} {
			if low == d {
				return false
			}
		}
		for _, d := range []string{".ssh", "secrets", "credentials", "certs", "keys",
			"tokens", "wallets", "private"} {
			if low == d {
				return false
			}
		}
	}
	base := strings.ToLower(filepath.Base(path))
	for _, x := range []string{".env", "secret", "credential", "allowlist", "token",
		"id_rsa", ".pem", ".p12", "password", "passwords"} {
		if strings.Contains(base, x) {
			return false
		}
	}
	// старые git-md (var/corpus/docs/2dph__corpus__git__*.md) — дубль git (#10)
	if strings.Contains(base, "corpus__git") {
		return false
	}
	return true
}
