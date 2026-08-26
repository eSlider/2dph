package bench

import (
	"math"
	"sort"
)

// Latency summarizes the per-query latency distribution in milliseconds.
type Latency struct {
	P50  float64 `json:"p50"`
	P95  float64 `json:"p95"`
	Mean float64 `json:"mean"`
	Min  float64 `json:"min"`
	Max  float64 `json:"max"`
	N    int     `json:"n"`
}

// ComputeLatency derives the distribution from per-query durations in
// milliseconds. An empty input yields a zero Latency with N=0.
func ComputeLatency(ms []float64) Latency {
	if len(ms) == 0 {
		return Latency{}
	}
	sorted := append([]float64(nil), ms...)
	sort.Float64s(sorted)
	var sum float64
	for _, v := range ms {
		sum += v
	}
	return Latency{
		P50:  percentile(sorted, 50),
		P95:  percentile(sorted, 95),
		Mean: round3(sum / float64(len(ms))),
		Min:  sorted[0],
		Max:  sorted[len(sorted)-1],
		N:    len(ms),
	}
}

// percentile returns the nearest-rank percentile (0-100) of a sorted slice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := int(math.Ceil(p/100*float64(len(sorted)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(sorted) {
		idx = len(sorted) - 1
	}
	return round3(sorted[idx])
}

func round3(f float64) float64 {
	return float64(int(f*1000+0.5)) / 1000
}
