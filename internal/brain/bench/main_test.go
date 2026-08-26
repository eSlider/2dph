package bench

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Main runs against --inproc (injected fake) or --candidate (URL/exec); the
// golden file must exist on disk.

func writeGolden(t *testing.T, queries []GoldenEntry) string {
	t.Helper()
	g := GoldenSet{Version: 1, Source: "test", Queries: queries}
	raw, err := json.Marshal(g)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(t.TempDir(), "golden.json")
	if err := os.WriteFile(p, raw, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// fakeOpen returns a searcher whose hits recall the fragment for every query.
func fakeOpen(delay time.Duration) InprocOpener {
	return func(context.Context, string) (Searcher, error) {
		return &fakeSearcher{
			byQ:   map[string][]Hit{"q": {{ID: "1", Text: "BM25 ranks best-first"}}},
			delay: delay,
		}, nil
	}
}

func TestMainBaselinePass(t *testing.T) {
	golden := writeGolden(t, []GoldenEntry{
		{Query: "q", Topic: "docs", Lang: "en", Fragment: "BM25"},
	})
	code := Main([]string{"--inproc", "--golden", golden}, fakeOpen(0))
	if code != 0 {
		t.Fatalf("exit=%d, want 0", code)
	}
}

func TestMainBaselineGateFail(t *testing.T) {
	golden := writeGolden(t, []GoldenEntry{
		{Query: "q", Topic: "docs", Lang: "en", Fragment: "absent-fragment"},
	})
	code := Main([]string{"--inproc", "--golden", golden}, fakeOpen(0))
	if code != 2 {
		t.Fatalf("exit=%d, want 2 (recall gate)", code)
	}
}

func TestMainBaselineMissingGolden(t *testing.T) {
	code := Main([]string{"--inproc", "--golden", filepath.Join(t.TempDir(), "nope.json")}, fakeOpen(0))
	if code != 1 {
		t.Fatalf("exit=%d, want 1 (load error)", code)
	}
}

func TestMainParseBadFlags(t *testing.T) {
	code := Main([]string{"--bogus-flag"}, nil)
	if code != 2 {
		t.Fatalf("exit=%d, want 2 (usage)", code)
	}
}

// candidateHTTPServer serves search hits that recall the baseline truth
// (same IDs), optionally slowing each call.
func candidateHTTPServer(t *testing.T, delay time.Duration) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if delay > 0 {
			time.Sleep(delay)
		}
		w.Header().Set("Content-Type", "application/json")
		content := `{"results":[{"id":"1","text":"BM25 ranks best-first","root":"info"}]}`
		body, _ := json.Marshal(content)
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[{"text":` +
			string(body) + `,"isError":false}]}}`))
	}))
}

func TestMainCandidateLatencyRatioFail(t *testing.T) {
	golden := writeGolden(t, []GoldenEntry{
		{Query: "q", Topic: "docs", Lang: "en", Fragment: "BM25"},
	})
	srv := candidateHTTPServer(t, 50*time.Millisecond)
	defer srv.Close()
	// baseline is fast (5ms), candidate is 50ms slower → ratio = 10 > 1.5.
	code := Main([]string{"--inproc", "--candidate", srv.URL, "--golden", golden}, fakeOpen(5*time.Millisecond))
	if code != 2 {
		t.Fatalf("exit=%d, want 2 (latency ratio gate)", code)
	}
}

func TestMainCandidatePass(t *testing.T) {
	golden := writeGolden(t, []GoldenEntry{
		{Query: "q", Topic: "docs", Lang: "en", Fragment: "BM25"},
	})
	// candidate fixture script returns the same IDs fast → recall+ratio pass.
	// baseline gets a small real delay so the ratio is meaningful (near-zero
	// latencies otherwise amplify process-spawn noise under -race).
	fixture := filepath.Join("testdata", "fake-search.sh")
	code := Main([]string{"--inproc", "--candidate", fixture, "--golden", golden}, fakeOpen(2*time.Millisecond))
	if code != 0 {
		t.Fatalf("exit=%d, want 0", code)
	}
}

func TestMainJSONOutput(t *testing.T) {
	golden := writeGolden(t, []GoldenEntry{
		{Query: "q", Topic: "docs", Lang: "en", Fragment: "BM25"},
	})
	// --json prints a machine report; assert it parses and carries gates.
	old := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout = w
	code := Main([]string{"--inproc", "--json", "--golden", golden}, fakeOpen(0))
	w.Close()
	os.Stdout = old
	if code != 0 {
		t.Fatalf("exit=%d, want 0", code)
	}
	var buf strings.Builder
	// read the pipe in a goroutine to avoid blocking on full buffer
	done := make(chan struct{})
	go func() {
		b := make([]byte, 4096)
		for {
			n, err := r.Read(b)
			buf.Write(b[:n])
			if err != nil {
				break
			}
		}
		close(done)
	}()
	<-done
	var rep Report
	if err := json.Unmarshal([]byte(buf.String()), &rep); err != nil {
		t.Fatalf("json report: %v\n%s", err, buf.String())
	}
	if rep.Tool != "brain/bench" || !rep.Gates.Recall5.Passed {
		t.Errorf("report = %+v", rep)
	}
}
