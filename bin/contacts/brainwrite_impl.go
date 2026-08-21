//go:build cgo && system_ladybug

package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/eSlider/2dph/internal/brain"
)

func init() {
	brainWriteFunc = writeContactsToBrain
}

// writeContactsToBrain upserts each contact as an info-root leaf. Requires the
// cgo + system_ladybug tags (internal/brain is behind those tags).
func writeContactsToBrain(cs []Contact, dbPath string) error {
	if dbPath == "" {
		dbPath = filepath.Join(brain.RepoRoot(), "var", "kb.lbug")
	}
	db, conn, err := brain.OpenWritable(dbPath)
	if err != nil {
		return fmt.Errorf("open brain: %w", err)
	}
	defer db.Close()
	defer conn.Close()
	if err := brain.InitSchema(conn); err != nil {
		return fmt.Errorf("brain schema: %w", err)
	}
	leafs := make([]brain.LeafInput, 0, len(cs))
	for _, ct := range cs {
		leafs = append(leafs, brain.LeafInput{
			Text:   ct.Markdown(),
			Root:   "info",
			Type:   "contact",
			How:    "contacts/import",
			Source: ct.Source,
			Loc:    ct.Source,
		})
	}
	ids, err := brain.AddLeafs(conn, leafs)
	if err != nil {
		return fmt.Errorf("brain write: %w", err)
	}
	if err := brain.EnsureIndexes(conn); err != nil {
		return fmt.Errorf("brain indexes: %w", err)
	}
	fmt.Fprintf(os.Stderr, "brain: wrote %d leaf(s) to %s\n", len(ids), dbPath)
	return nil
}
