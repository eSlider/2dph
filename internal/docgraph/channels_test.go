package docgraph

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

// TestPartitionsDiscoversHivePartitions: the source/channel list comes from the
// actual gator hive partitions (no hardcoded registry); foreign dirs ignored.
// Canon: source=portals, vendor in channel (PR #140).
func TestPartitionsDiscoversHivePartitions(t *testing.T) {
	hive := t.TempDir()
	mk := func(rel string) {
		if err := os.MkdirAll(filepath.Join(hive, rel), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	mk("source=portals/channel=o2/dt=2026-09-13")
	mk("source=portals/channel=dkv/dt=2026-09-13")
	mk("source=portals/channel=diashop/dt=2026-09-12")
	mk("source=portals/dt=2026-09-13") // no channel partition
	mk("not-a-partition/channel=x")    // foreign dir

	got, err := Partitions(hive)
	if err != nil {
		t.Fatal(err)
	}
	want := []Partition{
		{Source: "portals", Channel: "diashop"},
		{Source: "portals", Channel: "dkv"},
		{Source: "portals", Channel: "o2"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Partitions = %v, want %v", got, want)
	}
}

func TestPartitionsMissingHiveIsEmpty(t *testing.T) {
	got, err := Partitions(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatalf("missing document hive must not error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("Partitions = %v, want empty", got)
	}
	if _, err := Partitions(""); err == nil {
		t.Fatal("want error for an empty hive root")
	}
}

func TestHasParquet(t *testing.T) {
	hive := t.TempDir()
	dir := filepath.Join(hive, "source=portals", "channel=o2", "dt=2026-09-13")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	glob := filepath.Join(dir, "*.parquet")
	if HasParquet(glob) {
		t.Fatal("HasParquet = true for an empty partition")
	}
	if err := os.WriteFile(filepath.Join(dir, "data_0.parquet"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasParquet(glob) {
		t.Fatal("HasParquet = false with a packed file")
	}
}

func TestPackMtimeNewestParquet(t *testing.T) {
	hive := t.TempDir()
	dir := filepath.Join(hive, "source=portals", "channel=o2", "dt=2026-09-13")
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

func TestPackMtimeMissingHiveZero(t *testing.T) {
	got, err := PackMtime(filepath.Join(t.TempDir(), "absent"))
	if err != nil {
		t.Fatal(err)
	}
	if got != 0 {
		t.Fatalf("PackMtime = %d, want 0", got)
	}
	if got, _ := PackMtime(""); got != 0 {
		t.Fatalf("PackMtime(empty) = %d, want 0", got)
	}
}
