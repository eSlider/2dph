//go:build research_docpipe

// Live integration test for the hybrid docpipe handler (issue #223): runs the
// real sample var/research/samples/invoice-text.pdf through BOTH paths — the
// pdftotext fast path and the warm liteparse service (docker compose exec).
// Skipped when docker / the liteparse service / the sample is unavailable, so
// the default offline `go test ./...` stays green. Run with:
//
//	go test -tags=research_docpipe -run TestLive -v ./internal/docpipe/
package docpipe

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/research"
)

// repoRoot walks up from the package dir to the module root (go.mod).
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above", dir)
		}
		dir = parent
	}
}

func TestLiveInvoiceTextHybrid(t *testing.T) {
	root := repoRoot(t)
	t.Chdir(root) // docker compose must see compose.yaml at the module root
	if !research.DockerOK() {
		t.Skip("docker not on PATH")
	}
	probe := exec.Command("docker", "compose", "exec", "-T", "liteparse", "lit", "--version")
	if err := probe.Run(); err != nil {
		t.Skipf("liteparse service not reachable: %v", err)
	}
	sample := filepath.Join("var", "research", "samples", "invoice-text.pdf")
	if _, err := os.Stat(sample); err != nil {
		t.Skipf("sample %s not present: %v", sample, err)
	}

	// path 1: fast path (text-layer PDF)
	res, err := Handle(context.Background(), sample, Opts{})
	if err != nil {
		t.Fatalf("fast path: %v", err)
	}
	t.Logf("fastpath: method=%s ms=%.1f text=%q", res.Method, res.FastPathMs, firstLine(res.Text))
	if res.Method != MethodFastPath {
		t.Errorf("method = %s, want fastpath for a text-layer PDF", res.Method)
	}
	if !strings.Contains(res.Text, "Widget A") {
		t.Errorf("fast path text missing table row: %q", res.Text)
	}

	// path 2: forced liteparse JSON + table reconstruction
	res2, err := Handle(context.Background(), sample, Opts{ForceLiteparse: true})
	if err != nil {
		t.Fatalf("liteparse path: %v", err)
	}
	t.Logf("liteparse: method=%s ms=%.1f tables=%d", res2.Method, res2.LiteparseMs, len(res2.Tables))
	if res2.Method != MethodLiteparse {
		t.Fatalf("method = %s, want liteparse", res2.Method)
	}
	if len(res2.Tables) != 1 {
		t.Fatalf("tables = %d, want 1", len(res2.Tables))
	}
	tab := res2.Tables[0]
	if len(tab.Rows) != 4 {
		t.Fatalf("rows = %d, want 4", len(tab.Rows))
	}
	want := [][]string{
		{"Item", "Qty", "Price"},
		{"Widget A", "2", "10.00"},
		{"Widget B", "1", "25.00"},
		{"Total", "45.00"},
	}
	for i, w := range want {
		got := rowTexts(tab.Rows[i])
		if !slicesEqual(got, w) {
			t.Errorf("row %d = %v, want %v", i, got, w)
		}
	}
	if len(res2.YAML) == 0 {
		t.Error("YAML empty on liteparse path")
	}
}

// TestLiveBillTables verifies the handler on a real bill PDF with actual
// bordered tables (invoice-bill.pdf): liteparse detects kind:table blocks and
// ReconstructTables rebuilds both the Items table and the Totals table from
// text_items geometry. Skipped when the sample/service is unavailable.
func TestLiveBillTables(t *testing.T) {
	root := repoRoot(t)
	t.Chdir(root)
	if !research.DockerOK() {
		t.Skip("docker not on PATH")
	}
	probe := exec.Command("docker", "compose", "exec", "-T", "liteparse", "lit", "--version")
	if err := probe.Run(); err != nil {
		t.Skipf("liteparse service not reachable: %v", err)
	}
	sample := filepath.Join("var", "research", "samples", "invoice-bill.pdf")
	if _, err := os.Stat(sample); err != nil {
		t.Skipf("sample %s not present: %v", sample, err)
	}

	res, err := Handle(context.Background(), sample, Opts{ForceLiteparse: true})
	if err != nil {
		t.Fatalf("liteparse path: %v", err)
	}
	t.Logf("liteparse: method=%s ms=%.1f tables=%d", res.Method, res.LiteparseMs, len(res.Tables))
	if res.Method != MethodLiteparse {
		t.Fatalf("method = %s, want liteparse", res.Method)
	}
	if len(res.Tables) < 2 {
		t.Fatalf("tables = %d, want >= 2 (Items + Totals)", len(res.Tables))
	}
	// Items table: header row #/Description/Qty/Unit Price/Amount + 3 rows.
	items := res.Tables[0]
	if len(items.Rows) < 4 {
		t.Fatalf("Items rows = %d, want >= 4", len(items.Rows))
	}
	header := rowTexts(items.Rows[0])
	for _, want := range []string{"#", "Qty", "Amount"} {
		if !slicesContain(header, want) {
			t.Errorf("Items header %v missing %q", header, want)
		}
	}
	// Totals table: Subtotal/VAT/Total Due with amounts.
	totals := res.Tables[len(res.Tables)-1]
	totalTexts := tableTexts(totals)
	for _, want := range []string{"Subtotal", "VAT (19%)", "Total Due", "2856.00"} {
		if !slicesContain(totalTexts, want) {
			t.Errorf("Totals table missing %q (got %v)", want, totalTexts)
		}
	}
	if len(res.YAML) == 0 {
		t.Error("YAML empty on liteparse path")
	}
}

func tableTexts(t Table) []string {
	var out []string
	for _, r := range t.Rows {
		out = append(out, rowTexts(r)...)
	}
	return out
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}
