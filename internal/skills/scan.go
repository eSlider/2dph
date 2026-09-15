// Package skills validates that in-repo skills only reference tools that exist.
//
// A SKILL.md that names a missing bin/ path tells an agent to run something it
// cannot, which is how skills/agent-cost shipped with a non-existent
// bin/agents/cost (#3). audit self calls MissingBinRefs so CI fails instead.
package skills

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// binRef matches a repo-relative path under bin/ (optionally prefixed with ./).
// A leading boundary keeps it from matching inside a longer token.
var binRef = regexp.MustCompile(`(?:^|[^A-Za-z0-9_./+-])((?:\./)?bin/[A-Za-z0-9_][A-Za-z0-9_./+-]*)`)

// MissingBinRefs walks skills/**/*.md under root and returns "file: bin/path"
// for every bin/ path that does not exist. The result is sorted and deduplicated.
func MissingBinRefs(root string) ([]string, error) {
	skillsDir := filepath.Join(root, "skills")
	seen := map[string]bool{}
	var out []string

	err := filepath.WalkDir(skillsDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(d.Name(), ".md") {
			return nil
		}
		raw, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		for _, m := range binRef.FindAllStringSubmatch(string(raw), -1) {
			ref := strings.TrimRight(strings.TrimPrefix(m[1], "./"), ".,;:)]}>\"'`")
			if ref == "" || ref == "bin" || seen[rel+": "+ref] {
				continue
			}
			seen[rel+": "+ref] = true
			if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(ref))); err != nil {
				out = append(out, rel+": "+ref)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(out)
	return out, nil
}
