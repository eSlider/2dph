package source

import (
	"context"
	"errors"
	"fmt"
)

// Options configures a Sync run.
type Options struct {
	// StatePath is the checkpoint file for the source (var/state/<Name>.json).
	// The caller supplies it from config; a relative path is allowed in tests
	// (temp dir). Required.
	StatePath string
}

// Stats reports what one Sync run did.
type Stats struct {
	Fetched int // blobs returned by Fetch (incl. skipped duplicates)
	New     int // blobs handed to handle (not in the seen-set)
	Skipped int // already-seen blobs dropped
}

// Sync drives src: it loops Fetch → dedup via the sha256 seen-set → handle,
// persisting the checkpoint atomically after every successful batch and on any
// mid-batch failure. handle is called once per new blob.
//
// Semantics:
//   - a blob is emitted at most once (seen-set idempotency);
//   - a batch whose cursor does not advance (or that returns no data) ends the
//     run — "no new data" ⇒ 0 new blobs;
//   - on a mid-batch failure the checkpoint keeps cursor at the batch start and
//     the seen-set up to the failed item, so the next Sync resumes exactly where
//     it stopped without re-processing already-consumed items.
func Sync(ctx context.Context, src Source, handle func(context.Context, Blob) error, o Options) (Stats, error) {
	if o.StatePath == "" {
		return Stats{}, errors.New("source: Options.StatePath is required")
	}
	cp, err := loadCheckpoint(o.StatePath)
	if err != nil {
		return Stats{}, err
	}
	cursor := cp.Cursor
	seen := make(map[string]struct{}, len(cp.Seen))
	for _, k := range cp.Seen {
		seen[k] = struct{}{}
	}

	var stats Stats
	for {
		if err := ctx.Err(); err != nil {
			return stats, err
		}
		blobs, next, err := src.Fetch(ctx, cursor)
		if err != nil {
			return stats, err
		}
		if len(blobs) == 0 {
			if err := saveCheckpoint(o.StatePath, cursor, seen); err != nil {
				return stats, err
			}
			return stats, nil
		}
		stats.Fetched += len(blobs)

		for _, b := range blobs {
			key := hashID(b.ID)
			if _, ok := seen[key]; ok {
				stats.Skipped++
				continue
			}
			if err := handle(ctx, b); err != nil {
				// Resume from the start of this batch; already-consumed items
				// stay durable so they are not re-processed next run.
				if serr := saveCheckpoint(o.StatePath, cursor, seen); serr != nil {
					return stats, fmt.Errorf("source: batch %q failed: %w (checkpoint save: %v)", b.ID, err, serr)
				}
				return stats, err
			}
			seen[key] = struct{}{}
			stats.New++
		}

		prev := cursor
		cursor = next
		if err := saveCheckpoint(o.StatePath, cursor, seen); err != nil {
			return stats, err
		}
		// A source that does not advance its cursor (e.g. disk re-scan) has no
		// further distinct batches — stop instead of re-listing forever.
		if cursor == prev {
			return stats, nil
		}
	}
}
