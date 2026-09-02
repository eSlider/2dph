package incubator

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// fakeTransport records doveadm saves; tests run fully offline.
type fakeTransport struct {
	saved       []saveRec // one per doveadm save
	ensured     []string  // mailboxes created
	saveCalls   int
	ensureCalls int
}

type saveRec struct {
	mailbox string
	key     string
}

func (f *fakeTransport) save(_ context.Context, mailbox string, raw []byte) error {
	f.saveCalls++
	k, _, err := Key(raw)
	if err != nil {
		return err
	}
	f.saved = append(f.saved, saveRec{mailbox: mailbox, key: k})
	return nil
}

func (f *fakeTransport) ensure(_ context.Context, mailbox string) error {
	f.ensureCalls++
	f.ensured = append(f.ensured, mailbox)
	return nil
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// savedMailboxes returns the distinct mailboxes the fake saved into.
func (f *fakeTransport) savedMailboxes() []string {
	seen := map[string]bool{}
	var out []string
	for _, r := range f.saved {
		if !seen[r.mailbox] {
			seen[r.mailbox] = true
			out = append(out, r.mailbox)
		}
	}
	return out
}

func TestRunDryRun(t *testing.T) {
	root := mkTree(t)
	state := filepath.Join(t.TempDir(), "state.json")
	fake := &fakeTransport{}
	o := Options{Root: root, User: "u@example.com", State: state, Dry: true,
		Save: fake.save, Ensure: fake.ensure}
	st, err := Run(context.Background(), o) // limit 0 = all
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if st.Scanned != 8 || st.Imported != 8 || st.Already != 0 {
		t.Fatalf("dry-run stats wrong: %+v", st)
	}
	if fake.saveCalls != 0 || fake.ensureCalls != 0 {
		t.Fatalf("dry-run must not touch the transport: saves=%d ensures=%d", fake.saveCalls, fake.ensureCalls)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not write the manifest: %v", err)
	}
}

func TestRunLimitAndIdempotency(t *testing.T) {
	root := mkTree(t)
	state := filepath.Join(t.TempDir(), "state.json")

	// First run: limit 3 → exactly 3 imported (window = first 3 paths).
	fake := &fakeTransport{}
	o := Options{Root: root, User: "u@example.com", State: state, Limit: 3,
		Save: fake.save, Ensure: fake.ensure}
	st, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run(limit 3): %v", err)
	}
	if st.Window != 3 || st.Imported != 3 || st.Already != 0 {
		t.Fatalf("limit stats wrong: %+v", st)
	}
	if fake.saveCalls != 3 {
		t.Fatalf("expected 3 saves, got %d", fake.saveCalls)
	}
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("manifest must be written after a real run: %v", err)
	}

	// Re-run with the same limit → window fully in the manifest → 0 new.
	fake2 := &fakeTransport{}
	o.Save, o.Ensure = fake2.save, fake2.ensure
	st, err = Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run(re-run): %v", err)
	}
	if st.Already != 3 || st.Imported != 0 {
		t.Fatalf("re-run stats wrong (want already=3 imported=0): %+v", st)
	}
	if fake2.saveCalls != 0 {
		t.Fatalf("re-run must not save again, got %d calls", fake2.saveCalls)
	}

	// Widening the limit imports the next batch only.
	fake3 := &fakeTransport{}
	o.Limit, o.Save, o.Ensure = 8, fake3.save, fake3.ensure
	st, err = Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run(limit 8): %v", err)
	}
	if st.Imported != 5 {
		t.Fatalf("expected 5 new on widened limit, got %+v", st)
	}
}

func TestRunForce(t *testing.T) {
	root := mkTree(t)
	state := filepath.Join(t.TempDir(), "state.json")
	fake := &fakeTransport{}
	o := Options{Root: root, User: "u@example.com", State: state, Limit: 2,
		Save: fake.save, Ensure: fake.ensure}
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	fake2 := &fakeTransport{}
	o.Save, o.Ensure = fake2.save, fake2.ensure
	st, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if st.Imported != 0 {
		t.Fatalf("without force nothing re-imports, got %+v", st)
	}
	o.Force = true
	st, err = Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if st.Imported != 2 {
		t.Fatalf("--force re-imports the window, got %+v", st)
	}
}

func TestRunInRunDuplicate(t *testing.T) {
	root := t.TempDir()
	// two messages with the same Message-ID in the same window
	for _, dir := range []string{"0000001", "0000002"} {
		p := filepath.Join(root, dir, dir+".eml")
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		eml := "From: a@example.com\nMessage-Id: <dup@example.com>\nSubject: x\n\nbody\n"
		if err := os.WriteFile(p, []byte(eml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	state := filepath.Join(t.TempDir(), "state.json")
	fake := &fakeTransport{}
	o := Options{Root: root, User: "u@example.com", State: state,
		Save: fake.save, Ensure: fake.ensure}
	st, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if st.Imported != 1 || st.DupInRun != 1 {
		t.Fatalf("dup-in-run stats wrong: %+v", st)
	}
	if fake.saveCalls != 1 {
		t.Fatalf("expected 1 save for the dup pair, got %d", fake.saveCalls)
	}
}

func TestRunFoldersTargetsMailboxes(t *testing.T) {
	root := mkTree(t)
	state := filepath.Join(t.TempDir(), "state.json")
	fake := &fakeTransport{}
	o := Options{Root: root, User: "u@example.com", State: state, Limit: 8, Folders: true,
		Save: fake.save, Ensure: fake.ensure}
	st, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run(folders): %v", err)
	}
	if st.Imported != 8 {
		t.Fatalf("folders run stats: %+v", st)
	}
	// nested messages land in their mapped mailboxes; ensure runs per mailbox
	if !contains(fake.ensured, "INBOX/Archives") || !contains(fake.ensured, "INBOX/Projekte/Wesseling_Stadt") {
		t.Fatalf("ensure not called for folder mailboxes: %v", fake.ensured)
	}
	if contains(fake.ensured, "INBOX") {
		t.Fatalf("INBOX already exists — ensure must not be called for it: %v", fake.ensured)
	}
	for _, mb := range fake.savedMailboxes() {
		if mb != "INBOX" && !contains(fake.ensured, mb) {
			t.Fatalf("saved into %q without ensure (ensured=%v)", mb, fake.ensured)
		}
	}
	// per-mailbox stats match the scan map
	if st.ByMailbox["INBOX"] != 3 || st.ByMailbox["INBOX/Infos"] != 1 || st.ByMailbox["INBOX/Projekte/Wesseling_Stadt"] != 1 {
		t.Fatalf("ByMailbox wrong: %+v", st.ByMailbox)
	}
}

func TestRunFlatTargetsINBOX(t *testing.T) {
	root := mkTree(t)
	state := filepath.Join(t.TempDir(), "state.json")
	fake := &fakeTransport{}
	o := Options{Root: root, User: "u@example.com", State: state,
		Save: fake.save, Ensure: fake.ensure}
	st, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run(flat): %v", err)
	}
	if st.Imported != 8 {
		t.Fatalf("flat run stats: %+v", st)
	}
	if mbs := fake.savedMailboxes(); len(mbs) != 1 || mbs[0] != "INBOX" {
		t.Fatalf("flat run must save to INBOX only, got %v", mbs)
	}
	if fake.ensureCalls != 0 {
		t.Fatalf("flat run must not ensure mailboxes, got %d calls", fake.ensureCalls)
	}
}

func TestRunStatsUniqueNoID(t *testing.T) {
	root := mkTree(t)
	state := filepath.Join(t.TempDir(), "state.json")
	fake := &fakeTransport{}
	o := Options{Root: root, User: "u@example.com", State: state,
		Save: fake.save, Ensure: fake.ensure}
	st, err := Run(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if st.Unique != 8 {
		t.Fatalf("Unique = %d, want 8 (all keys distinct)", st.Unique)
	}
	if st.NoID != 1 {
		t.Fatalf("NoID = %d, want 1 (the id-less fixture message)", st.NoID)
	}
}
