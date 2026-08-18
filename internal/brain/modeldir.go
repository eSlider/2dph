//go:build cgo && system_ladybug

package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func modelDir() (string, error) {
	// 1. Explicit env
	if v := os.Getenv("KBSEARCH_MODEL"); v != "" {
		return v, nil
	}
	// 2. Next to the binary (dev or installed)
	self, err := os.Executable()
	if err == nil {
		if dir, err := filepath.EvalSymlinks(filepath.Dir(self)); err == nil {
			cand := filepath.Join(dir, "potion-multilingual-128m")
			if st, err := os.Stat(cand); err == nil && st.IsDir() {
				return cand, nil
			}
		}
	}
	// 3. Repo root models/ or lib/
	if v := os.Getenv("KB_ROOT"); v != "" {
		for _, sub := range []string{"models/potion-multilingual-128m", "lib/potion-multilingual-128m"} {
			cand := filepath.Join(v, sub)
			if st, err := os.Stat(cand); err == nil && st.IsDir() {
				return cand, nil
			}
		}
	}
	if root := repoRoot(); root != "" && root != "." {
		for _, sub := range []string{"models/potion-multilingual-128m", "lib/potion-multilingual-128m"} {
			cand := filepath.Join(root, sub)
			if st, err := os.Stat(cand); err == nil && st.IsDir() {
				return cand, nil
			}
		}
	}
	// 4. HF cache (new layout: models--*/snapshots/*)
	if v := os.Getenv("HF_HOME"); v != "" {
		base := filepath.Join(v, "hub")
		if entries, err := os.ReadDir(base); err == nil {
			for _, e := range entries {
				if strings.HasPrefix(e.Name(), "models--") {
					snapDir := filepath.Join(base, e.Name(), "snapshots")
					if snaps, err := os.ReadDir(snapDir); err == nil {
						for _, s := range snaps {
							cand := filepath.Join(snapDir, s.Name())
							if st, _ := os.Stat(cand); st != nil && st.IsDir() {
								return cand, nil
							}
						}
					}
				}
			}
		}
	}
	// 5. Legacy HF cache (symlinked model dir)
	if v := os.Getenv("HF_HOME"); v != "" {
		cand := filepath.Join(v, "potion-multilingual-128m")
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand, nil
		}
	}
	return "", fmt.Errorf("model not found (set KBSEARCH_MODEL or KB_ROOT, or download to HF cache)")
}