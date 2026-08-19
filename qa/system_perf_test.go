// qa/system_perf.go is an offline-gated system test (no live brain in CI).
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func source(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("system_perf.go"))
	if err != nil {
		t.Fatalf("read system_perf.go: %v", err)
	}
	return string(b)
}

func TestSystemPerfSourceGates(t *testing.T) {
	src := source(t)
	for _, want := range []string{
		"--json", "qwen3.5:9b", "--picoclaw", "BRAIN_URL",
		"tools/list", "tools/call",
		"gateHealthMS", "gateGetP50MS",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("system_perf.go missing %q", want)
		}
	}
	lower := strings.ToLower(src)
	for _, forbid := range []string{"kb.lbug", "password", "token"} {
		if strings.Contains(lower, forbid) {
			t.Errorf("system_perf.go must not reference %q", forbid)
		}
	}
}

func TestSystemPerfDoesNotWriteFiles(t *testing.T) {
	src := source(t)
	for _, call := range []string{"os.WriteFile", "os.Create", "os.OpenFile"} {
		if strings.Contains(src, call) {
			t.Errorf("system_perf.go must not write files (found %s)", call)
		}
	}
}
