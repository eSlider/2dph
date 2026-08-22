//go:build cgo && system_ladybug

package brain

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// modelHasWeights reports whether a HF snapshot holds what StaticModel loads.
func modelHasWeights(dir string) bool {
	if _, err := os.Stat(filepath.Join(dir, "tokenizer.json")); err != nil {
		return false
	}
	for _, f := range []string{"model.safetensors", "model.bin", "pytorch_model.bin", "model.onnx"} {
		if _, err := os.Stat(filepath.Join(dir, f)); err == nil {
			return true
		}
	}
	return false
}

// findHFSnapshot scans $HF_HOME/hub/models--*/snapshots/* and returns the
// first snapshot that actually carries tokenizer + weights, preferring the
// potion-multilingual-128M model when present.
func findHFSnapshot(base string) string {
	prefer := ""
	if entries, err := os.ReadDir(base); err == nil {
		for _, e := range entries {
			if !strings.HasPrefix(e.Name(), "models--") {
				continue
			}
			isPotion := strings.Contains(strings.ToLower(e.Name()), "potion-multilingual-128m")
			snapDir := filepath.Join(base, e.Name(), "snapshots")
			snaps, err := os.ReadDir(snapDir)
			if err != nil {
				continue
			}
			for _, s := range snaps {
				cand := filepath.Join(snapDir, s.Name())
				if !modelHasWeights(cand) {
					continue
				}
				if isPotion {
					return cand
				}
				if prefer == "" {
					prefer = cand
				}
			}
		}
	}
	return prefer
}

func modelDir() (string, error) {
	// 1. Explicit model path
	if v := brainCfg().Model; v != "" {
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
	if v := brainCfg().Root; v != "" {
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
	// 4. HF cache (new layout: models--*/snapshots/*); default HF_HOME is
	//    ~/.cache/huggingface (matching huggingface_hub), not only explicit env.
	hfcache := brainCfg().HFHome
	if hfcache == "" {
		if hd, err := os.UserHomeDir(); err == nil {
			hfcache = filepath.Join(hd, ".cache", "huggingface")
		}
	}
	if hfcache != "" {
		if dir := findHFSnapshot(filepath.Join(hfcache, "hub")); dir != "" {
			return dir, nil
		}
	}
	// 5. Legacy HF cache (symlinked model dir)
	if hfcache != "" {
		cand := filepath.Join(hfcache, "potion-multilingual-128m")
		if st, err := os.Stat(cand); err == nil && st.IsDir() {
			return cand, nil
		}
	}
	return "", fmt.Errorf("model not found (set KBSEARCH_MODEL or KB_ROOT, or download to HF cache)")
}
