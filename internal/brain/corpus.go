//go:build cgo && system_ladybug

package brain

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eSlider/2dph/internal/mdleaves"

	lbug "github.com/LadybugDB/go-ladybug"
)

var corpusDefaults = []string{"README.md", "PLAN.md", "AGENTS.md", "docs", "skills"}

// CorpusLeaf is a markdown-derived info leaf before embed/write.
type CorpusLeaf struct {
	Source  string
	Repo    string
	Heading string
	Text    string
	Type    string
	How     string
}

// LoadDefaultCorpus walks README/PLAN/AGENTS/docs/skills.
func LoadDefaultCorpus(root string) ([]CorpusLeaf, error) {
	var files []string
	for _, entry := range corpusDefaults {
		p := filepath.Join(root, entry)
		st, err := os.Stat(p)
		if err != nil {
			continue
		}
		if st.IsDir() {
			mds, err := mdleaves.WalkMarkdown(p)
			if err != nil {
				return nil, err
			}
			files = append(files, mds...)
		} else {
			files = append(files, p)
		}
	}
	return leafsFromMarkdownFiles(files, "eSlider/2dph", "kb/index")
}

// indexable keeps vendor noise and secret-ish paths out of the brain.
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
	return true
}

// LoadCorpusPath indexes an extra markdown file or directory.
func LoadCorpusPath(source string) ([]CorpusLeaf, error) {
	st, err := os.Stat(source)
	if err != nil {
		return nil, nil
	}
	repo := filepath.Base(source)
	var files []string
	if st.IsDir() {
		repo = filepath.Base(source)
		mds, err := mdleaves.WalkMarkdown(source)
		if err != nil {
			return nil, err
		}
		for _, m := range mds {
			if indexable(m) {
				files = append(files, m)
			}
		}
	} else {
		repo = filepath.Base(filepath.Dir(source))
		files = []string{source}
	}
	leafs, err := leafsFromMarkdownFiles(files, repo, "kb/index")
	if err != nil {
		return nil, err
	}
	if st.IsDir() {
		_ = filepath.Walk(source, func(p string, info os.FileInfo, err error) error {
			if err != nil {
				if os.IsPermission(err) {
					return nil
				}
				return err
			}
			if info.IsDir() {
				return nil
			}
			ext := strings.ToLower(filepath.Ext(p))
			if ext != ".yaml" && ext != ".yml" {
				return nil
			}
			if !indexable(p) {
				return nil
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return nil
			}
			text := string(raw)
			if len(text) > 20000 {
				text = text[:20000]
			}
			leafs = append(leafs, CorpusLeaf{
				Source: p, Repo: repo, Heading: strings.TrimSuffix(filepath.Base(p), ext),
				Text: text, Type: "seed", How: "kb/index",
			})
			return nil
		})
	}
	return leafs, nil
}

func leafsFromMarkdownFiles(files []string, repo, how string) ([]CorpusLeaf, error) {
	out := make([]CorpusLeaf, 0, len(files))
	for _, path := range files {
		if !strings.HasSuffix(strings.ToLower(path), ".md") && !strings.HasSuffix(strings.ToLower(path), ".markdown") {
			continue
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintf(os.Stderr, "brain/index: skip %s: %v\n", path, err)
			continue
		}
		for _, lf := range mdleaves.ToAll(string(raw), path, repo) {
			out = append(out, CorpusLeaf{
				Source: lf.Source, Repo: lf.Repo, Heading: lf.Heading,
				Text: lf.Text, Type: lf.Type, How: how,
			})
		}
	}
	return out, nil
}

// LoadMailLeafs indexes var/mail/**/message.md (+ attachment .md).
func LoadMailLeafs(root, since string, limit int) ([]CorpusLeaf, error) {
	if st, err := os.Stat(root); err != nil || !st.IsDir() {
		return nil, nil
	}
	var mds []string
	_ = filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		if filepath.Base(p) == "message.md" {
			mds = append(mds, p)
		}
		return nil
	})
	if since != "" {
		filtered := mds[:0]
		for _, md := range mds {
			if msgDate(md) >= since {
				filtered = append(filtered, md)
			}
		}
		mds = filtered
	}
	if limit > 0 && len(mds) > limit {
		mds = mds[:limit]
	}
	var out []CorpusLeaf
	for _, md := range mds {
		files := []string{md}
		att := filepath.Join(filepath.Dir(md), "attachments")
		if entries, err := os.ReadDir(att); err == nil {
			for _, e := range entries {
				if strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
					files = append(files, filepath.Join(att, e.Name()))
				}
			}
		}
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil {
				continue
			}
			id := filepath.Base(filepath.Dir(md))
			for _, lf := range mdleaves.ToAll(string(raw), f, "ooMail") {
				src := fmt.Sprintf("ooMail:%s:%s", id, filepath.Base(f))
				out = append(out, CorpusLeaf{
					Source: src, Repo: "ooMail", Heading: lf.Heading,
					Text: lf.Text, Type: lf.Type, How: "mail/import",
				})
			}
		}
	}
	return out, nil
}

func msgDate(md string) string {
	j := filepath.Join(filepath.Dir(md), "message.json")
	raw, err := os.ReadFile(j)
	if err != nil {
		return ""
	}
	var d map[string]any
	if json.Unmarshal(raw, &d) != nil {
		return ""
	}
	for _, k := range []string{"receivedDate", "receivedAt"} {
		if s, ok := d[k].(string); ok && len(s) >= 10 {
			return s[:10]
		}
	}
	return ""
}

// WriteCorpus embeds and upserts corpus leafs, linking FROM_FILE.
func WriteCorpus(conn *lbug.Connection, leafs []CorpusLeaf, model *StaticModel, limit int) (int, error) {
	n := 0
	for i, lf := range leafs {
		if limit > 0 && i >= limit {
			break
		}
		query := strings.ToValidUTF8(lf.Heading+"\n\n"+lf.Text, "\uFFFD")
		var emb []float64
		if model != nil && lf.Text != "" {
			var err error
			emb, err = model.Embed(lf.Text)
			if err != nil {
				return n, err
			}
		}
		typ := lf.Type
		if typ == "" {
			typ = "reference"
		}
		how := lf.How
		if how == "" {
			how = "kb/index"
		}
		id, err := UpsertLeaf(conn, LeafInput{
			Text: query, Root: "info", Confidence: "confirmed",
			Source: lf.Source, SourceRev: "working-tree", How: how,
			Loc: lf.Source, Type: typ, Embedding: emb,
		})
		if err != nil {
			return n, err
		}
		if _, err := LinkFromFile(conn, id, lf.Source, lf.Repo, ""); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
