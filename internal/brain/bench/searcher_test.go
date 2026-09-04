package bench

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

func TestExecSearcherFixture(t *testing.T) {
	path := filepath.Join("testdata", "fake-search.sh")
	s := &ExecSearcher{Path: path}
	defer s.Close()
	hits, err := s.Search(context.Background(), "hybrid fts vector", 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 || hits[0].ID != "1" || !strings.Contains(hits[0].Text, "BM25") {
		t.Errorf("hits = %+v", hits)
	}
	if !strings.HasPrefix(s.Name(), "exec:") {
		t.Errorf("Name=%q, want exec: prefix", s.Name())
	}
}

func TestExecSearcherMissingBinary(t *testing.T) {
	s := &ExecSearcher{Path: filepath.Join("testdata", "no-such-binary")}
	defer s.Close()
	if _, err := s.Search(context.Background(), "q", 5); err == nil {
		t.Error("missing binary must error")
	}
}

func TestExecSearcherNonZeroExit(t *testing.T) {
	s := &ExecSearcher{Path: "/bin/false"}
	defer s.Close()
	if _, err := s.Search(context.Background(), "q", 5); err == nil {
		t.Error("non-zero exit must error")
	}
}

func TestExecSearcherEmptyPath(t *testing.T) {
	s := &ExecSearcher{}
	if _, err := s.Search(context.Background(), "q", 5); err == nil {
		t.Error("empty path must error")
	}
}
