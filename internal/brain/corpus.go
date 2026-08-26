//go:build cgo && system_ladybug

package brain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/eSlider/2dph/internal/markdown"

	lbug "github.com/LadybugDB/go-ladybug"
)

var corpusDefaults = []string{"README.md", "PLAN.md", "AGENTS.md", "docs", "skills"}

// CorpusLeaf is a markdown-derived info leaf before embed/write.
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
			mds, err := markdown.WalkMarkdown(p)
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
		mds, err := markdown.WalkMarkdown(source)
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
		for _, lf := range markdown.ToAll(string(raw), path, repo) {
			out = append(out, CorpusLeaf{
				Source: lf.Source, Repo: lf.Repo, Heading: lf.Heading,
				Text: lf.Text, Type: lf.Type, How: how,
			})
		}
	}
	return out, nil
}

// WriteOptions controls corpus writing (worker concurrency, batch size, resume).
type WriteOptions struct {
	Limit    int               // max leafs to embed/write (0 = all)
	Workers  int               // parallel embedding workers (0 = 4)
	Batch    int               // leafs per transaction (0 = 64)
	Skip     bool              // skip leafs whose id already exists (resume)
	Progress *ProgressReporter // optional progress/ETA monitor
}

// WriteCorpus embeds (in parallel) and upserts corpus leafs in batches, linking
// FROM_FILE. Embedding errors abort; per-batch writes use one transaction.
func WriteCorpus(conn *lbug.Connection, leafs []CorpusLeaf, model *StaticModel, opt WriteOptions) (int, error) {
	if opt.Workers <= 0 {
		opt.Workers = 4
	}
	if opt.Batch <= 0 {
		opt.Batch = 64
	}
	if opt.Limit > 0 && len(leafs) > opt.Limit {
		leafs = leafs[:opt.Limit]
	}

	// Resume: drop leafs already present before embedding, so a re-run skips
	// the costly embedding step entirely.
	if opt.Skip {
		existing, err := existingLeafIDSet(conn)
		if err != nil {
			return 0, err
		}
		leafs = filterExistingLeafs(leafs, existing)
	}
	if opt.Progress != nil {
		opt.Progress.Report(0, len(leafs))
	}
	if len(leafs) == 0 {
		if opt.Progress != nil {
			opt.Progress.Finish(0, 0)
		}
		return 0, nil
	}

	items := make([]poolItem, len(leafs))
	for i, lf := range leafs {
		items[i] = poolItem{i: i, text: lf.Heading + "\n\n" + lf.Text}
	}
	embed := func(text string) ([]float64, error) {
		if model == nil || text == "" {
			return nil, nil
		}
		return model.Embed(text)
	}
	var progress func(int, int)
	if opt.Progress != nil {
		progress = opt.Progress.Report
	}
	results, err := parallelEmbed(context.Background(), items, embed, opt.Workers, progress)
	if err != nil {
		return 0, err
	}

	inputs := make([]LeafInput, 0, len(results))
	repos := make([]string, 0, len(results))
	for i, r := range results {
		if r.err != nil {
			return 0, fmt.Errorf("embed %d: %w", i, r.err)
		}
		lf := leafs[r.i]
		typ := lf.Type
		if typ == "" {
			typ = "reference"
		}
		how := lf.How
		if how == "" {
			how = "kb/index"
		}
		inputs = append(inputs, LeafInput{
			Text: strings.ToValidUTF8(items[r.i].text, "\uFFFD"), Root: "info",
			Confidence: "confirmed", Source: lf.Source, SourceRev: "working-tree",
			How: how, Loc: lf.Source, Type: typ, Embedding: r.emb, ValidFrom: lf.Date,
		})
		repos = append(repos, lf.Repo)
	}

	n := 0
	for _, b := range chunkBounds(len(inputs), opt.Batch) {
		ids, err := AddLeafs(conn, inputs[b[0]:b[1]])
		if err != nil {
			return n, err
		}
		for j, id := range ids {
			in := inputs[b[0]+j]
			if _, err := LinkFromFile(conn, id, in.Source, repos[b[0]+j], ""); err != nil {
				return n, err
			}
			n++
		}
		if opt.Progress != nil {
			opt.Progress.Report(n, len(inputs))
		}
	}
	if opt.Progress != nil {
		opt.Progress.Finish(n, len(leafs))
	}
	return n, nil
}

// existingLeafIDSet returns the set of all current leaf ids (for resume).
func existingLeafIDSet(conn *lbug.Connection) (map[string]bool, error) {
	res, err := conn.Query("MATCH (l:Leaf) RETURN l.id")
	if err != nil {
		return nil, err
	}
	defer res.Close()
	set := map[string]bool{}
	for res.HasNext() {
		row, err := res.Next()
		if err != nil {
			return nil, err
		}
		vals, err := row.GetAsSlice()
		if err != nil || len(vals) < 1 {
			continue
		}
		set[fmt.Sprint(vals[0])] = true
	}
	return set, nil
}
