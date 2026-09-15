//go:build mail_graph

package corpus

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/internal/docgraph"
	"github.com/eSlider/2dph/pkg/duckdb"
)

// GatorDoc streams searchable Leaf records from the gator kind=document parquet
// hive (C1 #117, ADR-0013). Mirror of GatorMail for the parquet/documents tree:
// document references (title/oo_path/sha256). Body (T12 #310): when Body is
// set, the PDF body text is fetched/extracted and appended to leaf.Text
// (BodyLimit bytes). Built only into mail-leaf/doc-leaf (mail_graph tag) —
// brain-index must not link DuckDB.
type GatorDoc struct {
	Hive string // parquet/documents hive root (…/parquet/documents)
	// Body, if non-nil, enriches each leaf with extracted PDF body text.
	Body docgraph.BodyProvider
	// BodyLimit caps appended body bytes (<=0 → docgraph.DefaultBodyLimit).
	BodyLimit int
}

func (g GatorDoc) Name() string { return "gator-document" }

func (g GatorDoc) Stream(ctx context.Context, emit func(contract.Leaf) error) error {
	if g.Hive == "" {
		return fmt.Errorf("gatordoc: hive root is empty")
	}
	parts, err := docgraph.Partitions(g.Hive)
	if err != nil {
		return err
	}
	for _, p := range parts {
		if err := g.streamPartition(ctx, p, emit); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (g GatorDoc) streamPartition(ctx context.Context, p docgraph.Partition, emit func(contract.Leaf) error) error {
	glob := filepath.Join(g.Hive, "source="+p.Source, "channel="+p.Channel, "dt=*", "*.parquet")
	if !docgraph.HasParquet(glob) {
		return nil // partition dir exists but no packed file yet
	}
	raw, err := duckdb.QueryRows(ctx, docgraph.ReadSQL(glob, p.Source, p.Channel))
	if err != nil {
		return fmt.Errorf("gatordoc %s: %w", p, err)
	}
	rows, skipped, firstErr := docgraph.MapRows(raw)
	if skipped > 0 && firstErr != nil {
		return fmt.Errorf("gatordoc %s: %d row(s) skipped: %v", p, skipped, firstErr)
	}
	for _, r := range rows {
		lf, ok := docgraph.RowToLeaf(r)
		if !ok {
			continue
		}
		if g.Body != nil {
			// Best-effort: a fetch/extract failure keeps the metadata leaf
			// (document stays findable) instead of dropping it or aborting.
			if body, err := g.Body.Body(ctx, r); err == nil && strings.TrimSpace(body) != "" {
				if enriched, ok := docgraph.RowToLeafBody(r, body, g.BodyLimit); ok {
					lf = enriched
				}
			}
		}
		if err := emit(lf); err != nil {
			return err
		}
	}
	return nil
}
