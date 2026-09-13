package corpus

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/eSlider/2dph/internal/contract"
	"github.com/eSlider/2dph/internal/mailgraph"
	"github.com/eSlider/2dph/pkg/duckdb"
)

// GatorMail streams searchable Leaf records from the gator kind=mail parquet
// hive (ADR-0013). Unlike corpus.Mail (legacy var/mail filesystem), this reads
// the live gator canon that autosync updates.
type GatorMail struct {
	Hive  string // parquet/mail hive root (…/var/gator/parquet/mail)
	Since string // YYYY-MM-DD: only messages on/after this day
}

func (g GatorMail) Name() string { return "gator-mail" }

func (g GatorMail) Stream(ctx context.Context, emit func(contract.Leaf) error) error {
	if g.Hive == "" {
		return fmt.Errorf("gatormail: hive root is empty")
	}
	channels, err := mailgraph.Channels(g.Hive)
	if err != nil {
		return err
	}
	for _, ch := range channels {
		if err := g.streamChannel(ctx, ch, emit); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
	}
	return nil
}

func (g GatorMail) streamChannel(ctx context.Context, channel string, emit func(contract.Leaf) error) error {
	glob := filepath.Join(g.Hive, "source=mail", "channel="+channel, "dt=*", "*.parquet")
	raw, err := duckdb.QueryRows(ctx, mailgraph.ReadSQL(glob, channel))
	if err != nil {
		return fmt.Errorf("gatormail channel %s: %w", channel, err)
	}
	rows, skipped, firstErr := mailgraph.MapRows(raw)
	if skipped > 0 && firstErr != nil {
		return fmt.Errorf("gatormail channel %s: %d row(s) skipped: %v", channel, skipped, firstErr)
	}
	for _, r := range rows {
		if g.Since != "" && !r.Date.IsZero() && r.Date.Format("2006-01-02") < g.Since {
			continue
		}
		lf, ok := mailgraph.RowToLeaf(r)
		if !ok {
			continue
		}
		if err := emit(lf); err != nil {
			return err
		}
	}
	return nil
}
