//go:build cgo && system_ladybug

package brain

import (
	"context"
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

// TestModelDirViaEnvConfigLoad reproduces the container wiring: the binary
// loads the config stack from process env (KB_ROOT + HF_HOME), calls
// Configure, then modelDir() must resolve the HF snapshot. Before the fix
// bin/brain/add.go and the search daemon never Configured, so activeCfg
// stayed at Defaults() with empty Root/HFHome/Model and modelDir() returned
// "model not found" even though HF_HOME pointed at a valid snapshot.
func TestModelDirViaEnvConfigLoad(t *testing.T) {
	resetConfig(t)
	base := t.TempDir()
	snap := filepath.Join(base, "var", "hf", "hub",
		"models--minishlab--potion-multilingual-128M", "snapshots", "abc789")
	if err := os.MkdirAll(snap, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"tokenizer.json", "model.safetensors"} {
		if err := os.WriteFile(filepath.Join(snap, f), []byte("{}"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	t.Setenv("KB_ROOT", filepath.Join(base, "data"))
	t.Setenv("HF_HOME", filepath.Join(base, "var", "hf"))
	cfg, err := config.LoadFrom(context.Background(), "")
	if err != nil {
		t.Fatalf("config.LoadFrom: %v", err)
	}
	Configure(cfg)

	dir, err := modelDir()
	if err != nil {
		t.Fatalf("modelDir after env-driven Configure must resolve: %v", err)
	}
	if dir != snap {
		t.Fatalf("modelDir=%q want %q", dir, snap)
	}
}
