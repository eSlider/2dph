//go:build cgo && system_ladybug

package brain

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eSlider/2dph/internal/config"
)

// resetConfig restores the typed config to a clean Defaults() (no model/root/
// HFHome overrides) so each test controls it via Configure.
func resetConfig(t *testing.T) {
	t.Helper()
	cfg := config.Defaults()
	Configure(&cfg)
}

func TestModelDirDefaultHFHomeLayout(t *testing.T) {
	resetConfig(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	snap := filepath.Join(home, ".cache", "huggingface", "hub",
		"models--minishlab--potion-multilingual-128M", "snapshots", "abc123")
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"tokenizer.json", "model.safetensors"} {
		if err := os.WriteFile(filepath.Join(snap, f), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := modelDir()
	if err != nil {
		t.Fatalf("default HF cache (no HF_HOME) must resolve: %v", err)
	}
	if dir != snap {
		t.Fatalf("modelDir=%q want %q", dir, snap)
	}
}

func TestModelDirExplicitHFHomeLayout(t *testing.T) {
	resetConfig(t)
	base := t.TempDir()
	cfg := config.Defaults()
	cfg.HFHome = base
	Configure(&cfg)
	snap := filepath.Join(base, "hub", "models--minishlab--potion-multilingual-128M",
		"snapshots", "abc456")
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"tokenizer.json", "model.safetensors"} {
		if err := os.WriteFile(filepath.Join(snap, f), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	dir, err := modelDir()
	if err != nil {
		t.Fatalf("explicit HF_HOME layout must resolve: %v", err)
	}
	if dir != snap {
		t.Fatalf("modelDir=%q want %q", dir, snap)
	}
}

func TestModelDirEnvOverride(t *testing.T) {
	resetConfig(t)
	cfg := config.Defaults()
	cfg.Model = "/opt/models/potion"
	cfg.HFHome = t.TempDir()
	Configure(&cfg)
	dir, err := modelDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/opt/models/potion" {
		t.Fatalf("model override must win, got %q", dir)
	}
}
