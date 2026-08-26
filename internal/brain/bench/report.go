package bench

import (
	"encoding/json"
	"fmt"
	"strings"
)

// GateResult is one pass/fail gate with its measured value.
type GateResult struct {
	Name      string  `json:"name"`
	Threshold float64 `json:"threshold"`
	Value     float64 `json:"value"`
	Passed    bool    `json:"passed"`
}

// Gates are the acceptance gates (issue #202 / #201).
type Gates struct {
	// Recall5 is the fragment recall@5 of the baseline (CI gate >= 0.95).
	Recall5 GateResult `json:"recall5"`
	// CandidateRecall is recall@5 of the candidate vs baseline hits; set
	// only when a candidate ran.
	CandidateRecall *GateResult `json:"candidate_recall,omitempty"`
	// LatencyRatio is p50(candidate)/p50(baseline); must stay <= 1.5.
	LatencyRatio *GateResult `json:"latency_ratio,omitempty"`
}

// Report is the full bench output: one pass per searcher plus gates.
type Report struct {
	Tool      string     `json:"tool"`
	Golden    string     `json:"golden"`
	Workers   int        `json:"workers"`
	Limit     int        `json:"limit"`
	Baseline  *RunReport `json:"baseline"`
	Candidate *RunReport `json:"candidate,omitempty"`
	Gates     Gates      `json:"gates"`
}

// JSON renders the machine-readable report.
func (rep *Report) JSON() ([]byte, error) {
	enc, err := json.MarshalIndent(rep, "", "  ")
	if err != nil {
		return nil, err
	}
	return enc, nil
}

// Table renders the human-readable report (runbook "Бенчмарки" format).
func (rep *Report) Table() string {
	var b strings.Builder
	fmt.Fprintf(&b, "bench: golden=%s queries=%d workers=%d limit=%d\n",
		rep.Golden, rep.Baseline.Queries, rep.Workers, rep.Limit)
	writePass(&b, rep.Baseline)
	if rep.Candidate != nil {
		writePass(&b, rep.Candidate)
	}
	fmt.Fprintf(&b, "gates: recall@5=%.3f >= %.3f %s\n",
		rep.Gates.Recall5.Value, rep.Gates.Recall5.Threshold, passWord(rep.Gates.Recall5.Passed))
	if rep.Gates.CandidateRecall != nil {
		fmt.Fprintf(&b, "gates: candidate recall@5 vs baseline=%.3f >= %.3f %s\n",
			rep.Gates.CandidateRecall.Value, rep.Gates.CandidateRecall.Threshold, passWord(rep.Gates.CandidateRecall.Passed))
	}
	if rep.Gates.LatencyRatio != nil {
		fmt.Fprintf(&b, "gates: latency p50 ratio=%.2f <= %.2f %s\n",
			rep.Gates.LatencyRatio.Value, rep.Gates.LatencyRatio.Threshold, passWord(rep.Gates.LatencyRatio.Passed))
	}
	return b.String()
}

func writePass(b *strings.Builder, p *RunReport) {
	fmt.Fprintf(b, "pass=%s searcher=%s failed=%d\n", p.Pass, p.Searcher, p.Failed)
	fmt.Fprintf(b, "  latency ms   p50=%.1f p95=%.1f mean=%.1f min=%.1f max=%.1f n=%d\n",
		p.Latency.P50, p.Latency.P95, p.Latency.Mean, p.Latency.Min, p.Latency.Max, p.Latency.N)
	fmt.Fprintf(b, "  recall@5     %.3f (%d/%d)    recall@10 %.3f\n",
		p.Recall.Score, p.Recall.Recalled, p.Recall.Total, p.Recall10.Score)
	fmt.Fprintf(b, "  resources    cpu_user=%.1fs cpu_sys=%.1fs rss_before=%dKB rss_after=%dKB rss_peak=%dKB\n",
		p.Resources.CPUUserSec, p.Resources.CPUSysSec,
		p.Resources.RSSBeforeKB, p.Resources.RSSAfterKB, p.Resources.RSSPeakKB)
}

func passWord(p bool) string {
	if p {
		return "PASS"
	}
	return "FAIL"
}
