//go:build cgo && system_ladybug

package brain

import (
	"math"
	"testing"
)

// TestCosineToQuery covers the cosine similarity used by the in-process
// brute-force vector scan (queryVector). It replaces the removed HNSW
// QUERY_VECTOR_INDEX path and must agree with plain cosine on known vectors.
func TestCosineToQuery(t *testing.T) {
	cases := []struct {
		name string
		q    []float64
		e    []any
		want float64
	}{
		{"identical", []float64{1, 0, 0}, []any{float32(1), float32(0), float32(0)}, 1.0},
		{"parallel", []float64{1, 0, 0}, []any{float32(2), float32(0), float32(0)}, 1.0},
		{"orthogonal", []float64{1, 0, 0}, []any{float32(0), float32(1), float32(0)}, 0.0},
		{"opposite", []float64{1, 0, 0}, []any{float32(-1), float32(0), float32(0)}, -1.0},
		{"mid", []float64{1, 0}, []any{float32(0.5), float32(0.5)}, math.Sqrt(2) / 2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := cosineToQuery(c.q, c.e)
			if math.Abs(got-c.want) > 1e-6 {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

// TestCosineToQueryZeroNorm ensures zero-length / all-zero embeddings do not
// produce NaN and are ranked worst (0 similarity).
func TestCosineToQueryZeroNorm(t *testing.T) {
	if got := cosineToQuery([]float64{1, 0}, []any{float32(0), float32(0)}); got != 0 {
		t.Fatalf("zero-norm embedding should score 0, got %v", got)
	}
	if got := cosineToQuery([]float64{}, []any{float32(1)}); got != 0 {
		t.Fatalf("empty query should score 0, got %v", got)
	}
}

// TestCosineToQueryFloat64Elem guards against the stored embedding coming back
// as float64 instead of float32 (liblbug LBUG_FLOAT surfaces float32 today).
func TestCosineToQueryFloat64Elem(t *testing.T) {
	got := cosineToQuery([]float64{1, 0}, []any{float64(1), float64(0)})
	if math.Abs(got-1.0) > 1e-6 {
		t.Fatalf("float64 element should score 1, got %v", got)
	}
}

// TestCosineToQueryTruncates guards the case where stored embedding length
// differs from the query (must iterate the shorter and not panic).
func TestCosineToQueryTruncates(t *testing.T) {
	got := cosineToQuery([]float64{1, 0, 0}, []any{float32(1), float32(0)})
	if math.Abs(got-1.0) > 1e-6 {
		t.Fatalf("truncated cosine should score 1, got %v", got)
	}
}
