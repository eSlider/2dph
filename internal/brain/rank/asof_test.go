package rank

import "testing"

func TestFilterAsOfKeepsXDropsY(t *testing.T) {
	hits := []Hit{
		{ID: "x", Text: "Andrey works at X", ValidFrom: "2024-03-01", ValidTo: "2025-07-15"},
		{ID: "y", Text: "Andrey works at Y", ValidFrom: "2025-07-16", ValidTo: ""},
		{ID: "legacy", Text: "always true claim", ValidFrom: "", ValidTo: ""},
	}
	out := FilterAsOf(hits, "2025-01-01")
	if len(out) != 2 {
		t.Fatalf("len=%d want 2: %+v", len(out), out)
	}
	if out[0].ID != "x" || out[1].ID != "legacy" {
		t.Fatalf("got %+v", out)
	}
	if FilterAsOf(hits, "") == nil || len(FilterAsOf(hits, "")) != 3 {
		t.Fatal("empty as-of must keep all")
	}
}

func TestParseArgsAsOf(t *testing.T) {
	opt, err := ParseArgs([]string{"who works where", "--as-of", "2025-01-01", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	if opt.AsOf != "2025-01-01" {
		t.Fatalf("AsOf=%q", opt.AsOf)
	}
	if _, err := ParseArgs([]string{"q", "--as-of", "not-a-date"}); err == nil {
		t.Fatal("expected bad as-of error")
	}
}
