package bench

import (
	"testing"
)

func TestFragmentRecalledSingleWord(t *testing.T) {
	hits := []Hit{{ID: "a", Text: "BM25 ranks best-first"}}
	if !FragmentRecalled(hits, "bm25", 0) {
		t.Error("single word should match case-insensitively")
	}
	if FragmentRecalled(hits, "vector", 0) {
		t.Error("missing word must not match")
	}
}

func TestFragmentRecalledMultiWordAcrossLines(t *testing.T) {
	hits := []Hit{{ID: "a", Text: "First line.\nSecond: independent sources or (not confirmed)"}}
	if !FragmentRecalled(hits, "independent sources", 0) {
		t.Error("multi-word fragment must match across a newline inside one hit")
	}
	if FragmentRecalled(hits, "independent sources evidence", 0) {
		t.Error("fragment with a word absent from the hit must not match")
	}
}

func TestFragmentRecalledTopK(t *testing.T) {
	hits := []Hit{{ID: "a", Text: "nothing"}, {ID: "b", Text: "tesseract ocr pdf"}}
	if !FragmentRecalled(hits, "tesseract", 0) {
		t.Error("fragment in the 2nd hit must match")
	}
	// top-1 must NOT match, top-2 must.
	if FragmentRecalled(hits, "tesseract", 1) {
		t.Error("fragment in rank 2 must not match at k=1")
	}
	if !FragmentRecalled(hits, "tesseract", 2) {
		t.Error("fragment in rank 2 must match at k=2")
	}
}

func TestFragmentRecalledEmpty(t *testing.T) {
	if FragmentRecalled(nil, "", 0) {
		t.Error("empty fragment must not match")
	}
}

func TestBaselineRecall(t *testing.T) {
	base := []Hit{{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"}, {ID: "5"}}
	cand := []Hit{{ID: "1"}, {ID: "9"}, {ID: "3"}, {ID: "4"}, {ID: "7"}}
	r := BaselineRecall(cand, base, 5)
	if r.Score != 0.6 || r.Recalled != 3 || r.Total != 5 {
		t.Errorf("recall@5 = %+v, want 3/5", r)
	}
	// recall@10 should match all 5 when k >= len(base).
	r10 := BaselineRecall(cand, base, 10)
	if r10.Recalled != 3 {
		t.Errorf("recall@10 recalled=%d, want 3", r10.Recalled)
	}
}

func TestBaselineRecallShortCandidate(t *testing.T) {
	base := []Hit{{ID: "1"}, {ID: "2"}, {ID: "3"}}
	cand := []Hit{{ID: "1"}}
	r := BaselineRecall(cand, base, 3)
	if r.Recalled != 1 || r.Total != 3 {
		t.Errorf("short candidate = %+v, want 1/3", r)
	}
}

func TestFragmentRecallHonorsK(t *testing.T) {
	// fragment sits at rank 6: recall@5 must miss it, recall@10 must find it.
	results := []QueryResult{{
		Entry: GoldenEntry{Query: "q", Fragment: "deep"},
		Hits: []Hit{
			{Text: "one"}, {Text: "two"}, {Text: "three"}, {Text: "four"},
			{Text: "five"}, {Text: "six deep target"},
		},
	}}
	r5 := FragmentRecall(results, 5)
	if r5.Recalled != 0 || r5.Score != 0 {
		t.Errorf("recall@5 = %+v, want 0 (fragment at rank 6)", r5)
	}
	r10 := FragmentRecall(results, 10)
	if r10.Recalled != 1 || r10.Score != 1 {
		t.Errorf("recall@10 = %+v, want 1", r10)
	}
}

func TestFragmentRecallOverResults(t *testing.T) {
	results := []QueryResult{
		{Entry: GoldenEntry{Query: "a", Fragment: "x"}, Hits: []Hit{{Text: "x"}}},
		{Entry: GoldenEntry{Query: "b", Fragment: "y"}, Hits: []Hit{{Text: "nope"}}},
		{Entry: GoldenEntry{Query: "c"}}, // no fragment → skipped
	}
	r := FragmentRecall(results, 5)
	if r.Total != 2 || r.Recalled != 1 || r.Score != 0.5 {
		t.Errorf("recall = %+v, want 1/2 = 0.5", r)
	}
}

func TestTopIDs(t *testing.T) {
	hits := []Hit{{ID: "1"}, {ID: "2"}, {ID: "3"}}
	if got := TopIDs(hits, 2); len(got) != 2 || got[0] != "1" || got[1] != "2" {
		t.Errorf("TopIDs(hits,2)=%v", got)
	}
	if got := TopIDs(hits, 5); len(got) != 3 {
		t.Errorf("TopIDs(hits,5)=%v", got)
	}
}
