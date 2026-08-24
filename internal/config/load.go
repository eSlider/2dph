package config

import (
	"context"
	"os"
	"path/filepath"
)

// Load discovers the repo root (a directory holding etc/brain/config.yml or
// .git, walking up from the working directory) and loads the stack rooted
// there. Missing layers are skipped; the result always carries Defaults().
func Load(ctx context.Context) (*Config, error) {
	wd, err := os.Getwd()
	if err != nil {
		wd = "."
	}
	return LoadFrom(ctx, findRoot(wd))
}

// LoadFrom loads the config stack rooted at dir. When dir is empty, only the
// process-env layer (and legacy mapping) apply. LoadFrom never fails on a
// missing file: missing layers contribute nothing.
func LoadFrom(ctx context.Context, dir string) (*Config, error) {
	merged := map[string]any{}

	if dir != "" {
		m, err := loadYAML(ctx, filepath.Join(dir, "etc", "brain", "config.yml"))
		if err != nil {
			return nil, err
		}
		mergeMaps(merged, m)

		m, err = loadYAML(ctx, filepath.Join(dir, "etc", "brain", "config.local.yml"))
		if err != nil {
			return nil, err
		}
		mergeMaps(merged, m)

		m, err = loadEnv(ctx, filepath.Join(dir, ".env"))
		if err != nil {
			return nil, err
		}
		mergeMaps(merged, m)
	}

	mergeMaps(merged, legacyEnv())

	cfg := Defaults()
	if err := decode(merged, &cfg); err != nil {
		return nil, err
	}
	if cfg.Root == "" {
		cfg.Root = dir
	}
	return &cfg, nil
}

// findRoot walks up from dir looking for the repo root marker.
func findRoot(dir string) string {
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "etc", "brain", "config.yml")); err == nil {
			return dir
		}
		if _, err := os.Stat(filepath.Join(dir, ".git")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	return ""
}
