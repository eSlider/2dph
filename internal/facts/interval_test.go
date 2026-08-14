package facts

import "testing"

func TestActiveAtOpenEnded(t *testing.T) {
	// works at Y from 2025-07-16, no end
	if !ActiveAt("2025-07-16", "", "2025-07-16") {
		t.Fatal("inclusive valid_from")
	}
	if !ActiveAt("2025-07-16", "", "2026-01-01") {
		t.Fatal("open-ended valid_to")
	}
	if ActiveAt("2025-07-16", "", "2025-07-15") {
		t.Fatal("before valid_from must be inactive")
	}
}

func TestActiveAtClosedInterval(t *testing.T) {
	// works at X 2024-03-01 .. 2025-07-15
	if !ActiveAt("2024-03-01", "2025-07-15", "2025-01-01") {
		t.Fatal("mid interval")
	}
	if !ActiveAt("2024-03-01", "2025-07-15", "2024-03-01") {
		t.Fatal("inclusive start")
	}
	if !ActiveAt("2024-03-01", "2025-07-15", "2025-07-15") {
		t.Fatal("inclusive end")
	}
	if ActiveAt("2024-03-01", "2025-07-15", "2025-07-16") {
		t.Fatal("day after end")
	}
	if ActiveAt("2024-03-01", "2025-07-15", "2024-02-28") {
		t.Fatal("day before start")
	}
}

func TestActiveAtEmptyIntervalAlwaysTrue(t *testing.T) {
	// legacy leafs without intervals stay visible for any as-of
	if !ActiveAt("", "", "2025-01-01") {
		t.Fatal("empty interval must remain active")
	}
	if !ActiveAt("", "", "") {
		t.Fatal("no as-of means all active")
	}
}

func TestActiveAtEmptyAsOfKeepsAll(t *testing.T) {
	if !ActiveAt("2099-01-01", "2099-12-31", "") {
		t.Fatal("empty as-of must not filter")
	}
}

func TestAsOfPickXNotY(t *testing.T) {
	// Acceptance from #36: as of 2025-01-01 → X, not Y
	xFrom, xTo := "2024-03-01", "2025-07-15"
	yFrom, yTo := "2025-07-16", ""
	asOf := "2025-01-01"
	if !ActiveAt(xFrom, xTo, asOf) {
		t.Fatal("X must be active as of 2025-01-01")
	}
	if ActiveAt(yFrom, yTo, asOf) {
		t.Fatal("Y must be inactive as of 2025-01-01")
	}
}

func TestNormalizeDayTrimsTime(t *testing.T) {
	if NormalizeDay("2025-01-01T12:00:00Z") != "2025-01-01" {
		t.Fatalf("got %q", NormalizeDay("2025-01-01T12:00:00Z"))
	}
	if NormalizeDay("2025-01-01") != "2025-01-01" {
		t.Fatalf("got %q", NormalizeDay("2025-01-01"))
	}
}
