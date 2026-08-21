package contact

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// loadSources ingests every path in sources. A path may be a single file or a
// directory (walked recursively). Supported extensions: .csv (Google Contacts),
// .vcf / .vcard (vCard 2.1 + 3.0), .mab (Thunderbird Mork).
func Load(sources []string) ([]Contact, error) {
	var out []Contact
	seen := map[string]bool{}
	for _, src := range sources {
		if src == "" {
			continue
		}
		fi, err := os.Stat(src)
		if err != nil {
			return nil, fmt.Errorf("source %q: %w", src, err)
		}
		if fi.IsDir() {
			paths, err := collectDir(src)
			if err != nil {
				return nil, err
			}
			for _, p := range paths {
				if seen[p] {
					continue
				}
				seen[p] = true
				cs, err := parseFileByExt(p)
				if err != nil {
					return nil, err
				}
				out = append(out, cs...)
			}
			continue
		}
		if seen[src] {
			continue
		}
		seen[src] = true
		cs, err := parseFileByExt(src)
		if err != nil {
			return nil, err
		}
		out = append(out, cs...)
	}
	return out, nil
}

func parseFileByExt(path string) ([]Contact, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".csv":
		return parseGoogleCSV(path)
	case ".vcf", ".vcard":
		return parseVCardFile(path)
	case ".mab":
		return parseMAB(path)
	default:
		return nil, nil // skip unknown
	}
}

func collectDir(dir string) ([]string, error) {
	var paths []string
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		ext := strings.ToLower(filepath.Ext(p))
		switch ext {
		case ".csv", ".vcf", ".vcard", ".mab":
			paths = append(paths, p)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(paths)
	return paths, nil
}
