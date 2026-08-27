// Package source implements the unified sync-ETL Source adapter layer (#97).
//
// Pipeline contract (epic #88):
//
//	Source.Fetch(ctx, cursor) → []Blob, Cursor, error
//
// A Source yields new items since the opaque cursor and returns the cursor that
// resumes after the batch. The driver (Sync) layers durable checkpointing on
// top: a sha256 seen-set guarantees each item is emitted exactly once, and the
// checkpoint (cursor + seen-set) is persisted atomically to
// var/state/<Name>.json (path supplied by the caller from config). A mid-batch
// failure persists the seen-set so the next run resumes without re-processing
// already-consumed items.
package source

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
)

// Cursor is opaque, source-specific resume state. The empty Cursor signals a
// first run. Sources are free to encode a page token, a delta link, or the last
// processed id.
type Cursor string

// Blob is one unit yielded by a Source. ID is the stable, content-derived
// identity; the driver stores sha256(ID) in the seen-set, so two blobs with the
// same ID across runs are the same item and are emitted at most once. Kind
// selects the downstream ETL handler (mail/git/chat/...); Path locates the item
// for handlers that read from disk (Data stays nil — lazy loading, ETL #9).
type Blob struct {
	ID   string `json:"id"`
	Kind string `json:"kind,omitempty"`
	Path string `json:"path,omitempty"`
	Data []byte `json:"-"`
}

// Source is the unified sync-ETL adapter contract (#97). Fetch returns the
// batch of blobs new since cursor plus the cursor that resumes after this
// batch; an empty batch with an unchanged cursor signals no new data. The
// driver drives Fetch sequentially, so implementations need not be internally
// concurrency-safe.
type Source interface {
	// Name is the checkpoint file stem: var/state/<Name>.json. It must be
	// stable per source instance and safe as a file name.
	Name() string
	Fetch(ctx context.Context, cursor Cursor) ([]Blob, Cursor, error)
}

// hashID returns the hex sha256 of a blob identity — the seen-set key.
func hashID(id string) string {
	h := sha256.Sum256([]byte(id))
	return hex.EncodeToString(h[:])
}
