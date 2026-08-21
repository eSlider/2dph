package brain

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// findSelf reports whether LiveHolders flags this test process holding path.
func findSelf(t *testing.T, path string) []string {
	t.Helper()
	out, err := LiveHolders(path)
	if err != nil {
		t.Fatal(err)
	}
	self := "pid " + strconv.Itoa(os.Getpid()) + " ("
	var mine []string
	for _, h := range out {
		if strings.HasPrefix(h, self) {
			mine = append(mine, h)
		}
	}
	return mine
}

func TestLiveHoldersDetectsOpenFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kb.lbug")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	mine := findSelf(t, path)
	if len(mine) != 1 {
		t.Fatalf("want exactly 1 self holder, got %v", mine)
	}

	// Closed fd must clear the detection.
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if got := findSelf(t, path); len(got) != 0 {
		t.Fatalf("holder still flagged after close: %v", got)
	}
}

func TestLiveHoldersDetectsDeletedInodeHolder(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "kb.lbug")
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}

	if mine := findSelf(t, path); len(mine) != 1 {
		t.Fatalf("deleted-inode holder not flagged, got %v", mine)
	}
}

func TestLiveHoldersMissingFileClean(t *testing.T) {
	out, err := LiveHolders(filepath.Join(t.TempDir(), "nope.lbug"))
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 0 {
		t.Fatalf("want no holders for missing file, got %v", out)
	}
}

func TestBrainAPIAlive(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/stats" {
			http.NotFound(w, r)
			return
		}
		fmt.Fprint(w, `{"by_root":{"facts":3},"db":"/data/var/kb.lbug","total":3}`)
	}))
	defer srv.Close()

	addr := strings.TrimPrefix(srv.URL, "http://")
	if !BrainAPIAlive(addr) {
		t.Fatal("want alive for serving /stats with total")
	}
	if BrainAPIAlive("127.0.0.1:1") { // nothing listens on port 1
		t.Fatal("want dead when nothing listens")
	}
}
