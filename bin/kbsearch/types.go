// Common types and helpers for kbsearch.
package main

import "os"

func eps() string { return os.Getenv("KBTEST_EPS") }

// Hit is one search result, mirroring the python script's dict shape.
type Hit struct {
	ID      string  `json:"id"`
	Text    string  `json:"text"`
	Root    string  `json:"root"`
	Source  string  `json:"-"` // for repo filtering, not in output
	Score   float64 `json:"score"`
	Snippet string  `json:"snippet,omitempty"`
	Hop     int     `json:"hop,omitempty"` // 0 = ranked hit, N = reached in N graph hops
}