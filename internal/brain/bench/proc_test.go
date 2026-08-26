package bench

import (
	"testing"
	"time"
)

func TestSampleProcSelf(t *testing.T) {
	s, err := SampleProc(0)
	if err != nil {
		t.Fatal(err)
	}
	// A freshly-started process may not have consumed a full clock tick yet,
	// so CPU may legitimately be 0; RSS must be present, though.
	if s.RSSKB <= 0 {
		t.Errorf("RSS=%dKB, want > 0", s.RSSKB)
	}
}

func TestSampleProcDelta(t *testing.T) {
	before, err := SampleProc(0)
	if err != nil {
		t.Fatal(err)
	}
	// burn CPU so the delta is measurable
	deadline := time.Now().Add(50 * time.Millisecond)
	for time.Now().Before(deadline) {
	}
	after, err := SampleProc(0)
	if err != nil {
		t.Fatal(err)
	}
	res := before.Resources(after)
	if res.CPUUserSec <= 0 {
		t.Errorf("CPU delta %+v, want > 0 after a busy loop", res)
	}
	if res.RSSBeforeKB <= 0 || res.RSSAfterKB <= 0 {
		t.Errorf("bad RSS: %+v", res)
	}
	if res.RSSPeakKB < res.RSSAfterKB {
		t.Errorf("peak %d < after %d", res.RSSPeakKB, res.RSSAfterKB)
	}
}

func TestSampleProcBadPid(t *testing.T) {
	if _, err := SampleProc(1 << 30); err == nil {
		t.Error("nonexistent pid must error")
	}
}
