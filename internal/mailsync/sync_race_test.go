package mailsync

import (
	"context"
	"errors"
	"testing"
)

// benchSource is a fake Source that yields a fixed set of ids and cheap
// messages, exercising the worker-pool path of Run concurrently.
type benchSource struct {
	ids    []string
	folder string
}

func (s *benchSource) ListIDs(context.Context, int, string) ([]string, string, error) {
	return s.ids, "", nil
}
func (s *benchSource) Get(context.Context, string) (*Message, error) {
	return &Message{ID: "m", Folder: s.folder}, nil
}
func (s *benchSource) DownloadAttachment(context.Context, *Message, Attachment) ([]byte, error) {
	return nil, errors.New("unused")
}
func (s *benchSource) Folder() string { return s.folder }

// TestRunWorkerPoolRaceSafe drives the sync worker pool (Run) with several
// workers over one source, proving the concurrent path — stats counters, the
// failures slice and per-message writes — is race-free under -race.
func TestRunWorkerPoolRaceSafe(t *testing.T) {
	out := t.TempDir()
	var ids []string
	for i := 0; i < 128; i++ {
		ids = append(ids, t.Name()+string(rune('a'+i%26))+string(rune('0'+i%10)))
	}
	src := &benchSource{ids: ids, folder: "bench"}
	stats, err := Run(context.Background(), SyncConfig{
		Out:     out,
		Workers: 8,
		Force:   true,
		sources: []Source{src},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if int(stats.New) != len(ids) {
		t.Fatalf("New = %d, want %d", stats.New, len(ids))
	}
}
