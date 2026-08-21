package cmdbin

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Root is the 2dph checkout (KB_ROOT, or walk up for .git / var).
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

// ExecFile runs repo-relative path (python/bash shebang scripts) with stdio.
func ExecFile(rel string, args []string) int {
	path := filepath.Join(Root(), filepath.FromSlash(rel))
	cmd := exec.Command(path, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = Root()
	if err := cmd.Run(); err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return ee.ExitCode()
		}
		if errors.Is(err, os.ErrNotExist) || strings.Contains(err.Error(), "no such file") {
			return 127
		}
		return 1
	}
	return 0
}
