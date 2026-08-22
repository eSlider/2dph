// Package utils holds small, dependency-free helpers shared across pkg/ and
// internal/. It must never import internal/ (pkg/ stays internal-free), so it
// carries no 2dph domain types.
package utils

import (
	"os"
	"path/filepath"
)

// Root locates the 2dph project root: the KB_ROOT env var, or walking up from
// the working directory for a `.git` or `var` marker. It returns "." if nothing
// is found. This is the single source of truth for repo-root resolution so
// pkg/repo and internal/chat share one implementation.
func Root() string {
	if v := os.Getenv("KB_ROOT"); v != "" {
		return v
	}
	wd, err := os.Getwd()
	if err != nil {
		return "."
	}
	for i := 0; i < 10; i++ {
		if _, err := os.Stat(filepath.Join(wd, ".git")); err == nil {
			return wd
		}
		if _, err := os.Stat(filepath.Join(wd, "var")); err == nil {
			return wd
		}
		parent := filepath.Dir(wd)
		if parent == wd {
			break
		}
		wd = parent
	}
	return "."
}
