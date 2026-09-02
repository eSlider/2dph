package incubator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRunLayoutRoutesByOwner proves the recipient-address routing end to end
// (issue #252, model 2026-09-02): owner in From → Sent, owner in To → INBOX,
// owner nowhere → INBOX/Unmatched, all via the manifest + ensure + save seam.
func TestRunLayoutRoutesByOwner(t *testing.T) {
	root := t.TempDir()
	write := func(rel string, hdrs ...string) {
		t.Helper()
		p := filepath.Join(root, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		raw := strings.Join(hdrs, "\n") + "\n\nbody\n"
		if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("0000001/0000001.eml", "From: andriy.oblivantsev@wheregroup.com", "To: alle@wheregroup.com", "Message-Id: <sent-1@example.com>")
	write("0000002/0000002.eml", "From: boss@example.com", "To: andriy.oblivantsev@wheregroup.com", "Message-Id: <inbox-1@example.com>")
	write("0000003/0000003.eml", "From: chiliproject@trac.wheregroup.com", "To: dev@wheregroup.com", "Message-Id: <unmatched-1@example.com>")
	write("0000004/0000004.eml", "From: andriy.oblivantsev@wheregroup.com", "To: foreign@example.com", "Message-Id: <sent-2@example.com>")

	state := filepath.Join(t.TempDir(), "state.json")
	fake := &fakeTransport{}
	o := Options{
		Root: root, User: owner, Owner: owner, State: state,
		Save: fake.save, Ensure: fake.ensure,
	}
	st, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run(layout): %v", err)
	}
	if st.Imported != 4 || st.Already != 0 {
		t.Fatalf("layout run stats: %+v", st)
	}
	wantTargets := map[string]int{LayoutSent: 2, LayoutInbox: 1, LayoutUnmatched: 1}
	if len(st.Targets) != len(wantTargets) {
		t.Fatalf("Targets = %v, want %v", st.Targets, wantTargets)
	}
	for mb, n := range wantTargets {
		if st.Targets[mb] != n {
			t.Errorf("Targets[%q] = %d, want %d (all: %v)", mb, st.Targets[mb], n, st.Targets)
		}
	}
	// every save target was ensured first; INBOX itself never ensured
	for _, rec := range fake.saved {
		if rec.mailbox != "INBOX" && !contains(fake.ensured, rec.mailbox) {
			t.Fatalf("saved into %q without ensure (ensured=%v)", rec.mailbox, fake.ensured)
		}
	}
	if contains(fake.ensured, "INBOX") {
		t.Fatalf("INBOX must not be ensured: %v", fake.ensured)
	}
	// manifest records the routing target per key
	m, err := LoadManifest(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entries) != 4 {
		t.Fatalf("manifest has %d entries, want 4", len(m.Entries))
	}
	byKey := map[string]string{}
	for _, e := range m.Entries {
		byKey[e.Key] = e.Mailbox
	}
	if byKey["sent-1@example.com"] != LayoutSent || byKey["inbox-1@example.com"] != LayoutInbox ||
		byKey["unmatched-1@example.com"] != LayoutUnmatched {
		t.Fatalf("manifest mailboxes wrong: %v", byKey)
	}

	// idempotency: re-run with the same window imports nothing
	fake2 := &fakeTransport{}
	o.Save, o.Ensure = fake2.save, fake2.ensure
	st, err = Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run(layout re-run): %v", err)
	}
	if st.Already != 4 || st.Imported != 0 {
		t.Fatalf("layout re-run stats (want already=4 imported=0): %+v", st)
	}
	if fake2.saveCalls != 0 {
		t.Fatalf("re-run must not save, got %d calls", fake2.saveCalls)
	}
}

// TestRunLayoutDryRun has no side effects and still reports the routing plan.
func TestRunLayoutDryRun(t *testing.T) {
	root := t.TempDir()
	p := filepath.Join(root, "0000001", "0000001.eml")
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	raw := "From: andriy.oblivantsev@wheregroup.com\nTo: x@y.de\nMessage-Id: <s@example.com>\n\nbody\n"
	if err := os.WriteFile(p, []byte(raw), 0o644); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "state.json")
	fake := &fakeTransport{}
	o := Options{Root: root, User: owner, Owner: owner, State: state, Dry: true,
		Save: fake.save, Ensure: fake.ensure}
	st, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run(layout dry-run): %v", err)
	}
	if st.Imported != 1 || st.Targets[LayoutSent] != 1 {
		t.Fatalf("dry-run layout stats: %+v", st)
	}
	if fake.saveCalls != 0 || fake.ensureCalls != 0 {
		t.Fatalf("dry-run must not touch the transport: %+v", fake)
	}
	if _, err := os.Stat(state); !os.IsNotExist(err) {
		t.Fatalf("dry-run must not write the manifest: %v", err)
	}
}
