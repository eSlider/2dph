package duckdb

import (
	"os"
	"testing"
)

func TestQuantilesEmpty(t *testing.T) {
	_, err := Quantiles(nil)
	if err == nil {
		t.Fatal("empty slice must error")
	}
}

func TestQuantilesOdd(t *testing.T) {
	s, err := Quantiles([]float64{1, 2, 3, 4, 5})
	if err != nil {
		t.Fatal(err)
	}
	if s.N != 5 {
		t.Fatalf("n=%d", s.N)
	}
	if s.Min != 1 || s.Max != 5 {
		t.Fatalf("min=%v max=%v", s.Min, s.Max)
	}
	if s.P50 != 3 {
		t.Fatalf("p50=%v want 3", s.P50)
	}
	if s.Avg != 3 {
		t.Fatalf("avg=%v want 3", s.Avg)
	}
	if s.P95 < 4.5 || s.P95 > 5 {
		t.Fatalf("p95=%v want in [4.5,5]", s.P95)
	}
}

func TestCountJSONL(t *testing.T) {
	dir := t.TempDir()
	p := dir + "/rows.jsonl"
	body := "{\"ms\":1}\n{\"ms\":2}\n{\"ms\":3}\n"
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	n, err := CountJSONL(p)
	if err != nil {
		t.Fatal(err)
	}
	if n != 3 {
		t.Fatalf("count=%d want 3", n)
	}
}
