package config

import (
	"context"
	"os"

	"github.com/eslider/go-config/yaml"
)

// loadYAML reads one optional YAML file into a normalized (lower+alnum,
// deep-merged) map. A missing file yields an empty map, not an error.
func loadYAML(ctx context.Context, path string) (map[string]any, error) {
	if path == "" {
		return map[string]any{}, nil
	}
	if _, err := os.Stat(path); err != nil {
		return map[string]any{}, nil
	}
	return yaml.New(yaml.WithFile(path)).Map(ctx)
}
