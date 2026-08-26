package ann

// Realistic synthetic data for the recall gate: points on a smooth manifold
// of the unit sphere (blends of nearby anchors), like real text embeddings —
// not tight near-orthogonal balls.
import (
	"math/rand"
	"testing"
)

func manifoldRows(n, nAnchors int, seed int64) []Row {
	rng := rand.New(rand.NewSource(seed))
	const dim = 256
	anchors := make([][]float32, nAnchors)
	for a := range anchors {
		v := make([]float32, dim)
		for i := range v {
			v[i] = rng.Float32()*2 - 1
		}
		anchors[a] = unit(v)
	}
	rows := make([]Row, n)
	for i := range rows {
		a := rng.Intn(nAnchors)
		b := rng.Intn(nAnchors)
		w := rng.Float32()
		v := make([]float32, dim)
		for d := range v {
			v[d] = anchors[a][d]*(1-w) + anchors[b][d]*w + (rng.Float32()-0.5)*0.05
		}
		rows[i] = Row{ID: "leaf-" + itoa(i), Vec: unit(v)}
	}
	return rows
}

func unit(v []float32) []float32 {
	var n float64
	for _, f := range v {
		n += float64(f) * float64(f)
	}
	inv := float32(1 / sqrt64(n))
	for i := range v {
		v[i] *= inv
	}
	return v
}

func TestANNRecallGateManifold(t *testing.T) {
	// Realistic corpus + default params: the A/B recall gate of #204
	// (recall@5 >= 0.95 vs exact brute-force ranking).
	rows := manifoldRows(2000, 20, 42)
	ix := New(Params{RngSeed: 7})
	if err := ix.Build(rows); err != nil {
		t.Fatalf("build: %v", err)
	}
	total, hits := 0, 0
	for q := 0; q < 40; q++ {
		qv := manifoldRows(1, 20, int64(q)+99)[0].Vec
		got := ix.Search(qv, 5)
		truth := ExactRank(qv, rows, 5)
		total += 5
		hits += recall5(got, truth)
	}
	score := float64(hits) / float64(total)
	t.Logf("recall@5 = %.3f (%d/%d)", score, hits, total)
	if score < 0.95 {
		t.Fatalf("recall@5 = %.3f < 0.95", score)
	}
}
