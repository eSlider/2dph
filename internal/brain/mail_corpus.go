package brain

// Mail corpus loading. Kept cgo-free so the loader unit-tests without the
// Ladybug library (plain `go test ./internal/brain/...`).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eSlider/2dph/internal/markdown"
)

// MailRoots returns the mail corpus roots in index order (issue #199):
// the live corpus var/corpus/mail first, then the legacy corpus var/mail
// (issue #184). Both share the <folder>/<id>/message.md layout; ids are
// sha256:16 content addresses for mbox/PST corpora, so the same message
// imported twice collapses onto one leaf.
func MailRoots(root string) []string {
	return []string{
		filepath.Join(root, "var", "corpus", "mail"),
		filepath.Join(root, "var", "mail"),
	}
}

// LoadMailLeafs indexes message.md leafs (+ attachment .md) from every mail
// corpus root. A missing root is skipped. Leafs are deduplicated by their
// content address (LeafID(text, source)) across roots, so a message present
// in both the live and the legacy corpus is indexed exactly once.
func LoadMailLeafs(roots []string, since string, limit int) ([]CorpusLeaf, error) {
	var out []CorpusLeaf
	seen := make(map[string]bool)
	for _, root := range roots {
		leafs, err := mailLeafsIn(root, since)
		if err != nil {
			return nil, err
		}
		for _, lf := range leafs {
			id := LeafID(lf.Heading+"\n\n"+lf.Text, lf.Source)
			if seen[id] {
				continue
			}
			seen[id] = true
			out = append(out, lf)
		}
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// mailLeafsIn indexes one corpus root: every <id>/message.md (+ attachment .md).
func mailLeafsIn(root, since string) ([]CorpusLeaf, error) {
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
	var out []CorpusLeaf
	for _, md := range mds {
		date := ""
		if raw, err := os.ReadFile(md); err == nil {
			if fm, _ := markdown.ExtractFrontmatter(string(raw)); len(fm["date"]) >= 10 {
				date = fm["date"][:10]
			}
		}
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
			for _, lf := range markdown.ToAll(string(raw), f, "ooMail") {
				src := fmt.Sprintf("ooMail:%s:%s", id, filepath.Base(f))
				out = append(out, CorpusLeaf{
					Source: src, Repo: "ooMail", Heading: lf.Heading,
					Text: lf.Text, Type: lf.Type, How: "mail/import", Date: date,
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
