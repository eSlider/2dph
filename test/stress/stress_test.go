// test/stress/stress.go is a live-brain load generator (no live brain in CI).
package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func stressSource(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("stress.go"))
	if err != nil {
		t.Fatalf("read stress.go: %v", err)
	}
	return string(b)
}

func TestStressSourceGates(t *testing.T) {
	src := stressSource(t)
	for _, want := range []string{
		"--json", "--c", "--d", "--mix", "BRAIN_URL",
		"search", "get", "stats", "audit",
		"gateHealthMS", "gateSearchP95MS", "gateErrPct",
		"req_per_sec",
	} {
		if !strings.Contains(src, want) {
			t.Errorf("stress.go missing %q", want)
		}
	}
	lower := strings.ToLower(src)
	for _, forbid := range []string{"/ingest", "kb.lbug", "password", "token"} {
		if strings.Contains(lower, forbid) {
			t.Errorf("stress.go must not reference %q", forbid)
		}
	}
}

func TestStressDoesNotWriteFiles(t *testing.T) {
	src := stressSource(t)
	for _, call := range []string{"os.WriteFile", "os.Create", "os.OpenFile"} {
		if strings.Contains(src, call) {
			t.Errorf("stress.go must not write files (found %s)", call)
		}
	}
}

func TestStressIngestGone(t *testing.T) {
	src := stressSource(t)
	if strings.Contains(src, "handleIngest") || strings.Contains(src, "POST /ingest") {
		t.Errorf("stress.go must stay read-only (no ingest)")
	}
}