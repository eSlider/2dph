package mailgraph

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestChannelsDiscoversHivePartitions ensures the channel list comes from the
// actual gator hive partitions (no hardcoded registry): creating a new
// channel= partition surfaces it, foreign dirs are ignored.
func TestChannelsDiscoversHivePartitions(t *testing.T) {
	hive := t.TempDir()
	mk := func(rel string) {
		if err := os.MkdirAll(filepath.Join(hive, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk("source=mail/channel=gmail/dt=2026-09-13")
	mk("source=mail/channel=wheregroup/dt=2026-09-12")
	mk("source=mail/channel=viscreation-gmx/dt=2026-09-12")
	mk("source=mail/dt=2026-09-12")   // no channel partition
	mk("source=hiddenjobs/channel=x") // foreign source

	got, err := Channels(hive)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"gmail", "viscreation-gmx", "wheregroup"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Channels = %v, want %v", got, want)
	}
}

func TestChannelsEmptyHive(t *testing.T) {
	// A hive where gator has not produced source=mail yet is not an error:
	// the periodic cycle must keep running (documents-only) instead of failing.
	got, err := Channels(t.TempDir())
	if err != nil {
		t.Fatalf("missing source=mail: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Channels = %v, want empty", got)
	}
	if _, err := Channels(""); err == nil {
		t.Fatal("want error for an empty hive root")
	}
}

func TestPackMtimeNewestParquet(t *testing.T) {
	hive := t.TempDir()
	dir := filepath.Join(hive, "source=mail", "channel=gmail", "dt=2026-09-13")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	old := filepath.Join(dir, "data_old.parquet")
	newer := filepath.Join(dir, "data_new.parquet")
	for _, p := range []string{old, newer} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	past := time.Now().Add(-48 * time.Hour)
	if err := os.Chtimes(old, past, past); err != nil {
		t.Fatal(err)
	}
	got, err := PackMtime(hive)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(newer)
	if err != nil {
		t.Fatal(err)
	}
	if got != info.ModTime().UnixNano() {
		t.Fatalf("PackMtime = %d, want newest parquet %d", got, info.ModTime().UnixNano())
	}
}
