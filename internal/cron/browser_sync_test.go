package cron

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var testCorpus = filepath.Join("testdata", "corpus")

// brainStub records every /ingest payload it receives.
type brainStub struct {
	mu     []byte
	posted []Leaf
}

func (s *brainStub) handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/ingest" {
			http.Error(w, "expected /ingest, got "+r.URL.Path, http.StatusNotFound)
			return
		}
		var p struct {
			Leafs []Leaf `json:"leafs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.posted = append([]Leaf(nil), p.Leafs...)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"mode":"add"}`))
	})
}

func newBrainStub(t *testing.T) (*brainStub, *httptest.Server) {
	t.Helper()
	stub := &brainStub{}
	srv := httptest.NewServer(stub.handler())
	t.Cleanup(srv.Close)
	return stub, srv
}

// TestRunOnceInvokesPipeline proves the daemon entry point runs end to end:
// corpus is loaded and posted to the brain ingest endpoint.
func TestRunOnceInvokesPipeline(t *testing.T) {
	stub, srv := newBrainStub(t)
	rep, err := RunOnce(context.Background(), Config{
		Corpus:      testCorpus,
		Brain:       srv.URL,
		SkipExtract: true,
	})
	if err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if rep.Leafs == 0 {
		t.Fatal("expected corpus leafs to be loaded")
	}
	if rep.Ingested == 0 {
		t.Fatal("expected leafs to be ingested")
	}
	if len(stub.posted) != rep.Leafs {
		t.Fatalf("brain received %d leafs, want %d", len(stub.posted), rep.Leafs)
	}
	if rep.Extracted {
		t.Fatal("extraction must be skipped when SkipExtract is set")
	}
}

// TestLoadCorpusHandlesGmailLinkedInDjinni proves all three sources are
// converted into typed leafs with non-empty text and source.
func TestLoadCorpusHandlesGmailLinkedInDjinni(t *testing.T) {
	leafs, err := LoadCorpus(testCorpus)
	if err != nil {
		t.Fatalf("LoadCorpus: %v", err)
	}
	bySource := map[string]int{}
	for _, lf := range leafs {
		prefix := strings.SplitN(lf.Source, ":", 2)[0] + ":"
		bySource[prefix]++
		if strings.TrimSpace(lf.Text) == "" {
			t.Errorf("leaf %q has empty text", lf.Source)
		}
		if strings.TrimSpace(lf.Source) == "" {
			t.Error("leaf has empty source")
		}
		if lf.Root == "" {
			t.Errorf("leaf %q has empty root", lf.Source)
		}
		if lf.Confidence == "" {
			t.Errorf("leaf %q has empty confidence", lf.Source)
		}
	}
	for _, prefix := range []string{"gmail:", "linkedin:", "djinni:"} {
		if bySource[prefix] == 0 {
			t.Errorf("expected leafs with source prefix %q, got %d", prefix, bySource[prefix])
		}
	}
	if bySource["gmail:"] != 2 {
		t.Errorf("expected 2 gmail leafs, got %d", bySource["gmail:"])
	}
}

// TestPushPostsToBrainIngest proves leafs are sent as {leafs:[...]} to
// <brain>/ingest over POST.
func TestPushPostsToBrainIngest(t *testing.T) {
	stub, srv := newBrainStub(t)
	leafs := []Leaf{
		{Text: "hello", Source: "gmail:a", Root: "info", Confidence: "confirmed", Type: "email"},
		{Text: "world", Source: "gmail:b", Root: "info", Confidence: "confirmed", Type: "email"},
	}
	n, err := Push(context.Background(), srv.URL, time.Second, leafs)
	if err != nil {
		t.Fatalf("Push: %v", err)
	}
	if n != 2 {
		t.Fatalf("Push returned %d, want 2", n)
	}
	if len(stub.posted) != 2 {
		t.Fatalf("brain got %d leafs, want 2", len(stub.posted))
	}
	if stub.posted[0].Text != "hello" || stub.posted[0].Type != "email" {
		t.Fatalf("unexpected first leaf: %+v", stub.posted[0])
	}
}

// TestExtractThoriumUnavailable proves a failing agent-browser call surfaces
// as ErrThoriumUnavailable so callers can skip gracefully.
func TestExtractThoriumUnavailable(t *testing.T) {
	execRun = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("connection refused")
	}
	defer func() { execRun = defaultRunner }()

	err := Extract(context.Background(), Config{})
	if err == nil {
		t.Fatal("expected an error when agent-browser fails")
	}
	if !errors.Is(err, ErrThoriumUnavailable) {
		t.Fatalf("error should wrap ErrThoriumUnavailable, got %v", err)
	}
}

// TestRunOnceSkipsWhenThoriumDown proves a missing Thorium does not abort the
// run: extraction is skipped and the last-known corpus is still ingested.
func TestRunOnceSkipsWhenThoriumDown(t *testing.T) {
	execRun = func(context.Context, string, ...string) ([]byte, error) {
		return nil, errors.New("connection refused")
	}
	defer func() { execRun = defaultRunner }()

	stub, srv := newBrainStub(t)
	rep, err := RunOnce(context.Background(), Config{
		Corpus: testCorpus,
		Brain:  srv.URL,
	})
	if err != nil {
		t.Fatalf("RunOnce should tolerate a down Thorium, got %v", err)
	}
	if !rep.Skipped {
		t.Fatal("expected report.Skipped when Thorium is unavailable")
	}
	if rep.Extracted {
		t.Fatal("extraction must not be marked when Thorium is down")
	}
	if rep.Ingested == 0 || len(stub.posted) == 0 {
		t.Fatal("last-known corpus should still be ingested when Thorium is down")
	}
}

// TestExtractSucceeds proves a healthy agent-browser call reports success.
func TestExtractSucceeds(t *testing.T) {
	execRun = func(context.Context, string, ...string) ([]byte, error) {
		return []byte("connected"), nil
	}
	defer func() { execRun = defaultRunner }()

	if err := Extract(context.Background(), Config{}); err != nil {
		t.Fatalf("Extract should succeed, got %v", err)
	}
}
