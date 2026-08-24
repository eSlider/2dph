package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"

	"github.com/eSlider/2dph/internal/etl"
)

// Disk lists .eml files under Root and yields one Blob per file. It is the
// sync-ETL adapter for on-disk mail corpora (var/corpus/* directories).
//
// The cursor never advances: Fetch re-lists the tree on every call and the
// driver's sha256 seen-set supplies idempotency (the seen key is derived from
// the file content, so a changed .eml is re-emitted once). This keeps the
// adapter stateless — the source scans, the driver dedups.
type Disk struct {
	Root string
}

func (d *Disk) Name() string { return "disk" }

// Fetch returns one Blob per .eml under Root, in deterministic order. Each
// blob's ID is the hex sha256 of the file content (content-addressed identity).
func (d *Disk) Fetch(_ context.Context, _ Cursor) ([]Blob, Cursor, error) {
	if d.Root == "" {
		return nil, "", fmt.Errorf("source: disk Root is empty")
	}
	files, err := etl.WalkFiles(d.Root, etl.WalkOptions{Exts: []string{".eml"}})
	if err != nil {
		return nil, "", err
	}
	blobs := make([]Blob, 0, len(files))
	for _, f := range files {
		// Content identity: the same path with different bytes is a new item,
		// so edits are re-ingested once.
		sum, err := sha256File(f.Path)
		if err != nil {
			return nil, "", err
		}
		blobs = append(blobs, Blob{
			ID:   sum,
			Kind: "mail",
			Path: f.Path,
		})
	}
	return blobs, "", nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
