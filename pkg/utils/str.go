package utils

// Snippet truncates s to at most n runes-equivalent bytes, appending an
// ellipsis when it is cut. It is used for short error contexts.
func Snippet(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// Or returns def when s is empty, otherwise s.
func Or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
