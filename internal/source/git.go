package source

import (
	"context"
	"fmt"

	"github.com/eSlider/2dph/internal/gitlog"
)

// Git yields one Blob per commit in a repository's history (go-git, no git
// binary), newest first. It is the sync-ETL adapter for git-history sources.
//
// The cursor is the newest commit already consumed (the HEAD of the previous
// run): Fetch returns every commit strictly newer than it, then advances the
// cursor to the newest commit of the returned batch. New commits pushed on top
// are picked up by the next run; a repeat Fetch with no new commits yields 0
// blobs, so the adapter is idempotent on its own.
type Git struct {
	Repo string
}

func (g *Git) Name() string { return "git" }

// Fetch returns commits strictly newer than cursor (or all commits on a first
// run). next is the newest commit of the batch — the next resume point.
func (g *Git) Fetch(_ context.Context, cursor Cursor) ([]Blob, Cursor, error) {
	if g.Repo == "" {
		return nil, "", fmt.Errorf("source: git Repo is empty")
	}
	commits, err := gitlog.Log(g.Repo, gitlog.Options{})
	if err != nil {
		return nil, "", err
	}
	// commits are newest-first; cursor is the newest commit already consumed.
	// New commits are those strictly newer than it, i.e. the prefix before it.
	idx := len(commits)
	if cursor != "" {
		idx = -1
		for i, c := range commits {
			if c.SHA == string(cursor) {
				idx = i
				break
			}
		}
		if idx == -1 {
			// Cursor not in history (rewritten / shallow). Fall back to the
			// seen-set in the driver for dedup rather than dropping commits.
			idx = len(commits)
		}
	}
	newCommits := commits[:idx]
	blobs := make([]Blob, 0, len(newCommits))
	for _, c := range newCommits {
		blobs = append(blobs, Blob{
			ID:   c.SHA,
			Kind: "git",
			Data: []byte(c.Subject),
		})
	}
	if len(newCommits) == 0 {
		return nil, cursor, nil
	}
	// The newest commit of the batch is the next resume point.
	return blobs, Cursor(newCommits[0].SHA), nil
}
