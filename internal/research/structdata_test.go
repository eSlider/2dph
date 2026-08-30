package research

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestStructifyWithoutDocker returns a clear error instead of panicking: the
// tool requires the warm compose service. Docker is not available in tests.
func TestStructifyWithoutDocker(t *testing.T) {
	if DockerOK() {
		t.Skip("docker present; offline test only")
	}
	r := NewRunner("", 0)
	dir := t.TempDir()
	src := filepath.Join(dir, "dummy.pdf")
	if err := os.WriteFile(src, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := r.Structify(t.Context(), DocSource{Path: src}, ConvertOpts{})
	if err == nil {
		t.Fatal("Structify without docker should error")
	}
	if !strings.Contains(err.Error(), "docker") {
		t.Errorf("error should mention docker, got: %v", err)
	}
}

// TestExistsIdempotency: an existing non-empty YAML means skip; a missing one
// or an empty file means re-conversion is allowed.
func TestExistsIdempotency(t *testing.T) {
	// StructPath is fixed under var/struct-data; simulate by writing directly.
	hash := "cafe00"
	p := StructPath(hash)
	_ = os.MkdirAll(filepath.Dir(p), 0o755)
	_ = os.Remove(p)
	if Exists(hash) {
		t.Fatal("Exists(true) for missing file")
	}
	if err := os.WriteFile(p, []byte("meta:\n  hash: cafe00\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !Exists(hash) {
		t.Fatal("Exists(false) for present non-empty file")
	}
	// Empty file → treated as missing (force re-convert).
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if Exists(hash) {
		t.Fatal("Exists(true) for empty file — should re-convert")
	}
	_ = os.Remove(p)
}

func TestStructDataYAMLMarshal(t *testing.T) {
	sd := StructData{
		Meta: Meta{
			Hash: "abc", SourcePath: "var/research/samples/x.pdf", Extension: "pdf",
			Format: "pdf", Size: 100, DigitizedAt: "2026-08-30T00:00:00Z", Engine: "liteparse",
		},
		Document: LitJSON{TotalPages: 1},
	}
	y, err := yamlMarshalSD(&sd)
	if err != nil {
		t.Fatal(err)
	}
	s := string(y)
	for _, want := range []string{"meta:", "hash: abc", "source_path:", "extension: pdf", "document:", "total_pages:"} {
		if !strings.Contains(s, want) {
			t.Errorf("YAML missing %q", want)
		}
	}
}