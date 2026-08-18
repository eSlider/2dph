//go:build cgo && system_ladybug

package brain

import (
	"os"
	"path/filepath"
	"testing"
)

func TestModelDirDefaultHFHomeLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("KBSEARCH_MODEL", "")
	t.Setenv("KB_ROOT", "")
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
	base := t.TempDir()
	t.Setenv("HF_HOME", base)
	t.Setenv("KBSEARCH_MODEL", "")
	t.Setenv("KB_ROOT", "")
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
	t.Setenv("KBSEARCH_MODEL", "/opt/models/potion")
	t.Setenv("HF_HOME", t.TempDir())
	t.Setenv("KB_ROOT", "")
	dir, err := modelDir()
	if err != nil {
		t.Fatal(err)
	}
	if dir != "/opt/models/potion" {
		t.Fatalf("KBSEARCH_MODEL must win, got %q", dir)
	}
}