package source

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
)

// checkpoint is the on-disk resume state for one source. Seen is the sorted
// sha256 seen-set (deterministic JSON); Cursor is the last persisted resume
// point.
type checkpoint struct {
	Cursor Cursor   `json:"cursor"`
	Seen   []string `json:"seen"`
}

// loadCheckpoint reads the checkpoint file, returning an empty checkpoint when
// it does not exist yet (first run). A corrupt file is an error — never a
// silent reset.
func loadCheckpoint(path string) (checkpoint, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return checkpoint{}, nil
	}
	if err != nil {
		return checkpoint{}, err
	}
	var cp checkpoint
	if err := json.Unmarshal(b, &cp); err != nil {
		return checkpoint{}, fmt.Errorf("source: corrupt checkpoint %s: %w", path, err)
	}
	return cp, nil
}

// saveCheckpoint writes the checkpoint atomically: temp file in the same
// directory, fsync, rename over the target. A reader never observes a partial
// checkpoint, and a failed write leaves the previous state intact.
func saveCheckpoint(path string, cursor Cursor, seen map[string]struct{}) error {
	cp := checkpoint{Cursor: cursor, Seen: make([]string, 0, len(seen))}
	for k := range seen {
		cp.Seen = append(cp.Seen, k)
	}
	sort.Strings(cp.Seen)
	b, err := json.MarshalIndent(cp, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op after successful rename
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
