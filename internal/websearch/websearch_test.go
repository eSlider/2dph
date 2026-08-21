package websearch

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"
)

func loadFixture(t *testing.T, name string) Payload {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	path := filepath.Join(filepath.Dir(file), "testdata", name)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var p Payload
	if err := json.Unmarshal(raw, &p); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestClassifyHealthyIsOK(t *testing.T) {
	if got := Classify(loadFixture(t, "healthy.json")); got != StatusOK {
		t.Fatalf("classify healthy = %q, want ok", got)
	}
}

func TestClassifyEmptyIsThrottledNotEmpty(t *testing.T) {
	got := Classify(loadFixture(t, "throttled.json"))
	if got != StatusThrottled {
		t.Fatalf("classify empty = %q, want throttled", got)
	}
	if got == "empty" || got == "no_results" {
		t.Fatal("status must never sound like absence")
	}
}

func TestProjectKeepsContextFields(t *testing.T) {
	out := Project(loadFixture(t, "healthy.json"), 3, DefaultSnippetChars)
	if out.Status != StatusOK {
		t.Fatalf("status = %q", out.Status)
	}
	if len(out.Results) != 3 {
		t.Fatalf("len = %d, want 3", len(out.Results))
	}
	r := out.Results[0]
	if r.Rank != 1 || r.Title == "" || r.URL == "" {
		t.Fatalf("hit = %+v", r)
	}
}

func TestProjectTrimsSnippet(t *testing.T) {
	out := Project(loadFixture(t, "healthy.json"), 5, 40)
	for _, r := range out.Results {
		n := utf8.RuneCountInString(r.Snippet)
		if n > 43 {
			t.Fatalf("snippet len %d > 43: %q", n, r.Snippet)
		}
	}
}

func TestProjectIsCheaperThanRaw(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join(fixtureDir(t), "healthy.json"))
	if err != nil {
		t.Fatal(err)
	}
	out, err := json.Marshal(Project(loadFixture(t, "healthy.json"), 5, DefaultSnippetChars))
	if err != nil {
		t.Fatal(err)
	}
	if len(out)*3 >= len(raw) {
		t.Fatalf("projected %d not cheaper than raw %d", len(out), len(raw))
	}
}

func TestThrottledProjectionCarriesEngineReasons(t *testing.T) {
	out := Project(loadFixture(t, "throttled.json"), 5, DefaultSnippetChars)
	if out.Status != StatusThrottled {
		t.Fatalf("status = %q", out.Status)
	}
	if len(out.Results) != 0 {
		t.Fatalf("results = %v", out.Results)
	}
	if len(out.Unresponsive) == 0 {
		t.Fatal("unresponsive empty")
	}
	if !strings.Contains(out.Note, "not evidence that nothing exists") {
		t.Fatalf("note = %q", out.Note)
	}
}

func TestCacheKeyStable(t *testing.T) {
	if CacheKey("Pflegegrad", nil) != CacheKey("Pflegegrad", map[string]string{}) {
		t.Fatal("nil vs empty params")
	}
	if CacheKey("  Pflegegrad ", nil) != CacheKey("pflegegrad", nil) {
		t.Fatal("case/padding")
	}
	if CacheKey("x", map[string]string{"lang": "de"}) == CacheKey("x", nil) {
		t.Fatal("params must change key")
	}
	a := CacheKey("x", map[string]string{"a": "1", "b": "2"})
	b := CacheKey("x", map[string]string{"b": "2", "a": "1"})
	if a != b {
		t.Fatal("param order must not change key")
	}
}

func TestPHIGuard(t *testing.T) {
	if PHIReason("Pflegegrad SGB XI Einstufung") != "" {
		t.Fatal("technical query refused")
	}
	if PHIReason("site:example.com technical query") != "" {
		t.Fatal("site query refused")
	}
	if PHIReason("SGB XI Paragraph 45b") != "" {
		t.Fatal("short numbers refused")
	}
	if PHIReason("Kunde 4711220385 Adresse") == "" {
		t.Fatal("long digit run allowed")
	}
	if PHIReason("KV-Nr A123456789") == "" {
		t.Fatal("KV-Nr allowed")
	}
	if PHIReason("Hauptstraße 14 Berlin") == "" {
		t.Fatal("street allowed")
	}
	if PHIReason("Lindenstr. 7") == "" {
		t.Fatal("str. allowed")
	}
	if PHIReason("Personalnummer 12") == "" {
		t.Fatal("Personalnummer allowed")
	}
}

func TestWaitFor(t *testing.T) {
	last := 100.0
	if got := WaitFor(&last, 104.0, 10); got != 6 {
		t.Fatalf("wait = %v, want 6", got)
	}
	if got := WaitFor(&last, 130.0, 10); got != 0 {
		t.Fatalf("wait = %v, want 0", got)
	}
	if got := WaitFor(nil, 130.0, 10); got != 0 {
		t.Fatalf("first call wait = %v", got)
	}
}

func TestSQLiteCacheRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c, err := OpenCache(filepath.Join(dir, "web-search.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	p := loadFixture(t, "healthy.json")
	key := CacheKey("pflegegrad", nil)
	if got, err := c.Get(key, CacheTTL, 1_000); err != nil || got != nil {
		t.Fatalf("empty get = %v %v", got, err)
	}
	if err := c.Put(key, p, 1_000); err != nil {
		t.Fatal(err)
	}
	got, err := c.Get(key, CacheTTL, 1_001)
	if err != nil || got == nil {
		t.Fatalf("get = %v %v", got, err)
	}
	if Classify(*got) != StatusOK {
		t.Fatalf("cached classify = %s", Classify(*got))
	}
	expired, err := c.Get(key, 10, 2_000)
	if err != nil || expired != nil {
		t.Fatalf("expired = %v %v", expired, err)
	}
	if v, err := c.LastCall(); err != nil || v != nil {
		t.Fatalf("last = %v %v", v, err)
	}
	if err := c.MarkCall(50); err != nil {
		t.Fatal(err)
	}
	v, err := c.LastCall()
	if err != nil || v == nil || *v != 50 {
		t.Fatalf("last after mark = %v %v", v, err)
	}
}

func fixtureDir(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller")
	}
	return filepath.Join(filepath.Dir(file), "testdata")
}
