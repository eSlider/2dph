package bench

import "testing"

func TestComputeLatencyPercentiles(t *testing.T) {
	// 10 samples: sorted = 10..19 → p50 = 14, p95 = 19 (nearest-rank).
	ms := []float64{10, 19, 12, 18, 13, 17, 14, 16, 15, 11}
	l := ComputeLatency(ms)
	if l.P50 != 14 || l.P95 != 19 {
		t.Errorf("p50=%v p95=%v, want 14 19", l.P50, l.P95)
	}
	if l.Min != 10 || l.Max != 19 {
		t.Errorf("min=%v max=%v, want 10 19", l.Min, l.Max)
	}
	if l.N != 10 {
		t.Errorf("N=%d, want 10", l.N)
	}
	// mean = 145/10 = 14.5
	if l.Mean != 14.5 {
		t.Errorf("mean=%v, want 14.5", l.Mean)
	}
}

func TestComputeLatencyEmpty(t *testing.T) {
	l := ComputeLatency(nil)
	if l.N != 0 || l.P50 != 0 || l.P95 != 0 {
		t.Errorf("empty latency = %+v, want zeros", l)
	}
}

func TestComputeLatencySingle(t *testing.T) {
	l := ComputeLatency([]float64{42})
	if l.P50 != 42 || l.P95 != 42 || l.Mean != 42 {
		t.Errorf("single = %+v, want all 42", l)
	}
}
