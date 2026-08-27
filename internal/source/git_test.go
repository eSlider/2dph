package source

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/go-git/go-git/v5"
	"github.com/go-git/go-git/v5/plumbing/object"
)

// newTestRepo creates a bare-less temp repo with n linear commits, each adding
// one file, and returns the repo path.
func newTestRepo(t *testing.T, n int) string {
	t.Helper()
	dir := t.TempDir()
	r, err := git.PlainInit(dir, false)
	if err != nil {
		t.Fatalf("git init: %v", err)
	}
	w, err := r.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: "Alice", Email: "alice@example.com", When: time.Now()}
	for i := 0; i < n; i++ {
		f := filepath.Join(dir, "commit_"+string(rune('a'+i))+".txt")
		if err := os.WriteFile(f, []byte("content "+string(rune('a'+i))+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Add("commit_" + string(rune('a'+i)) + ".txt"); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Commit("commit "+string(rune('a'+i)), &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// Git yields one Blob per commit, newest first, and advances the cursor to the
// oldest processed commit.
func TestGitSourceYieldsCommitsNewestFirst(t *testing.T) {
	repo := newTestRepo(t, 3)
	src := &Git{Repo: repo}

	blobs, next, err := src.Fetch(ctx(), "")
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if len(blobs) != 3 {
		t.Fatalf("got %d commits, want 3", len(blobs))
	}
	if blobs[0].ID == blobs[1].ID || blobs[1].ID == blobs[2].ID {
		t.Fatal("commit IDs must be distinct")
	}
	if next == "" {
		t.Fatal("cursor must advance to oldest processed commit")
	}

	// Fetch again from the checkpoint cursor: no new commits.
	again, _, err := src.Fetch(ctx(), next)
	if err != nil {
		t.Fatalf("second Fetch: %v", err)
	}
	if len(again) != 0 {
		t.Fatalf("second Fetch returned %d blobs, want 0", len(again))
	}
}

// A new commit after the checkpoint is picked up on the next run.
func TestGitSourcePicksUpNewCommit(t *testing.T) {
	repo := newTestRepo(t, 1)
	src := &Git{Repo: repo}

	first, next, err := src.Fetch(ctx(), "")
	if err != nil {
		t.Fatal(err)
	}
	if len(first) != 1 {
		t.Fatalf("first Fetch: %d blobs, want 1", len(first))
	}

	// Add one more commit on top.
	w, err := git.PlainOpen(repo)
	if err != nil {
		t.Fatal(err)
	}
	wt, err := w.Worktree()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "new.txt"), []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := wt.Add("new.txt"); err != nil {
		t.Fatal(err)
	}
	sig := &object.Signature{Name: "Alice", Email: "alice@example.com", When: time.Now()}
	if _, err := wt.Commit("new commit", &git.CommitOptions{Author: sig, Committer: sig}); err != nil {
		t.Fatal(err)
	}

	more, _, err := src.Fetch(ctx(), next)
	if err != nil {
		t.Fatal(err)
	}
	if len(more) != 1 {
		t.Fatalf("after new commit Fetch = %d blobs, want 1", len(more))
	}
}
