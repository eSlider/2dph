// Package bench implements the A/B search benchmark harness (issue #202):
// a fixed golden-set of queries is run through a search implementation and
// scored on latency (p50/p95/mean), recall@5/@10 and CPU/RSS. The current
// linear scan is the baseline; every optimization candidate (ANN, SIMD,
// FTS-first) must be measured against the same harness before acceptance.
//
// The package is cgo-free: every searcher (in-process ladybug, HTTP MCP,
// candidate binary) implements the Searcher interface, so the harness logic
// is fully testable offline against synthetic hits.
package bench
