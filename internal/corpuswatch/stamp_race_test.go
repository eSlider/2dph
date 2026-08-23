package corpuswatch

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

// TestStampConcurrentSafe drives the poll-loop primitive (Stamp) from many
// goroutines over the same dirs. Run's loop calls Stamp from one goroutine, but
// Stamp is exported and may be shared by concurrent callers (e.g. a health
// probe poking the same corpus). It walks the filesystem only and keeps no
// package state, so under -race this must be clean.
func TestStampConcurrentSafe(t *testing.T) {
	dir := t.TempDir()
	for i := 0; i < 8; i++ {
		p := filepath.Join(dir, string(rune('a'+i))+".md")
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var wg sync.WaitGroup
	for w := 0; w < 16; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 40; i++ {
				if Stamp([]string{dir}) == "" {
					t.Error("stamp empty for a populated dir")
					return
				}
			}
		}()
	}
	wg.Wait()
}
