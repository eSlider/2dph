package incubator

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeMsg writes one synthetic .eml fixture (headers + blank line + body).
// Synthetic addresses only (Alice/Bob/example.com rule); test data offline.
func writeMsg(t *testing.T, root, rel string, hdrs ...string) {
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

// TestRunGlobalDedupAgainstOtherManifest proves the cross-source dedup of
// gator #101: an import skips canonical keys already imported by ANOTHER
// source — the other manifest is listed read-only in Options.SkipState (the
// гдеgroup/gmail key-store vs a defacto pass). The skipped key is counted in
// Stats.AlreadyOther (not Already — that stays the own-manifest re-run
// counter) and is never written into the own manifest.
func TestRunGlobalDedupAgainstOtherManifest(t *testing.T) {
	root := t.TempDir()
	writeMsg(t, root, "d0001/0000001.eml",
		"From: alice@example.com", "To: bob@example.com", "Message-Id: <shared@example.com>")
	writeMsg(t, root, "d0002/0000002.eml",
		"From: alice@example.com", "To: bob@example.com", "Message-Id: <fresh@example.com>")

	other := filepath.Join(t.TempDir(), "incubator-gmail.json")
	om := Manifest{Version: manifestVersion, Entries: []Entry{
		{Key: "shared@example.com", Path: "gmail/1.eml", Mailbox: "INBOX"},
	}}
	if err := om.Save(other); err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(t.TempDir(), "incubator-defacto.json")
	fake := &fakeTransport{}
	o := Options{Root: root, User: "u@example.com", State: state,
		SkipState: []string{other}, Save: fake.save, Ensure: fake.ensure}
	st, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if st.Scanned != 2 || st.Unique != 2 {
		t.Fatalf("scan stats wrong: %+v", st)
	}
	if st.AlreadyOther != 1 || st.Already != 0 || st.DupInRun != 0 || st.Imported != 1 {
		t.Fatalf("global-dedup stats wrong (want already-other=1 imported=1): %+v", st)
	}
	if fake.saveCalls != 1 {
		t.Fatalf("saves = %d, want 1 (only the fresh message)", fake.saveCalls)
	}
	m, err := LoadManifest(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entries) != 1 || m.Entries[0].Key != "fresh@example.com" {
		t.Fatalf("manifest must record only the fresh key, got %+v", m.Entries)
	}

	// Re-run: own manifest blocks fresh (Already), the other source still
	// blocks shared (AlreadyOther) — nothing imports twice.
	fake2 := &fakeTransport{}
	o.Save, o.Ensure = fake2.save, fake2.ensure
	st, err = Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run(re-run): %v", err)
	}
	if st.Already != 1 || st.AlreadyOther != 1 || st.Imported != 0 {
		t.Fatalf("re-run stats wrong (want already=1 already-other=1): %+v", st)
	}
	if fake2.saveCalls != 0 {
		t.Fatalf("re-run must not save, got %d calls", fake2.saveCalls)
	}
}

// TestRunSkipStateMissingFileIsEmpty: a not-yet-existing skip-state manifest
// (first real import of another source, or the second Local_Folders pass
// whose sibling manifest was never written) is an empty key-store, not an
// error — same semantics as the own manifest on a first run.
func TestRunSkipStateMissingFileIsEmpty(t *testing.T) {
	root := t.TempDir()
	writeMsg(t, root, "d0001/0000001.eml",
		"From: alice@example.com", "To: bob@example.com", "Message-Id: <fresh@example.com>")
	fake := &fakeTransport{}
	o := Options{Root: root, User: "u@example.com", State: filepath.Join(t.TempDir(), "state.json"),
		SkipState: []string{filepath.Join(t.TempDir(), "not-yet.json")},
		Save:      fake.save, Ensure: fake.ensure}
	st, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run with missing skip-state manifest: %v", err)
	}
	if st.Imported != 1 || st.AlreadyOther != 0 {
		t.Fatalf("stats wrong: %+v", st)
	}
}

// TestRunSkipFromFilterDropsMarketing proves the sender filter of gator #101:
// messages From a configured marketing address (Loewe gewinnspiel@loewe.de)
// are dropped before dedup — not imported, never recorded in the manifest,
// counted in Stats.Filtered and excluded from Unique (they are not corpus for
// this import at all). Matching is case-insensitive on the addr-spec.
func TestRunSkipFromFilterDropsMarketing(t *testing.T) {
	root := t.TempDir()
	// Loewe marketing with uppercase From — filter must be case-insensitive.
	writeMsg(t, root, "d0001/0000001.eml",
		"From: GEWINNSPIEL@LOEWE.DE", "To: prizes@example.com", "Message-Id: <loewe-1@example.com>")
	// a normal letter of the slice owner
	writeMsg(t, root, "d0002/0000002.eml",
		"From: boss@example.com", "To: viscreation@gmx.de", "Message-Id: <real-1@example.com>")

	state := filepath.Join(t.TempDir(), "state.json")
	fake := &fakeTransport{}
	o := Options{Root: root, User: "viscreation@gmx.de", Owner: "viscreation@gmx.de", State: state,
		SkipFrom: []string{"gewinnspiel@loewe.de"},
		Save:     fake.save, Ensure: fake.ensure}
	st, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if st.Scanned != 2 || st.Filtered != 1 || st.Unique != 1 || st.NoID != 0 {
		t.Fatalf("scan/filter stats wrong: %+v", st)
	}
	if st.Imported != 1 || st.Already != 0 {
		t.Fatalf("import stats wrong: %+v", st)
	}
	if fake.saveCalls != 1 {
		t.Fatalf("saves = %d, want 1 (the marketing message must not reach the transport)", fake.saveCalls)
	}
	m, err := LoadManifest(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entries) != 1 || m.Entries[0].Key != "real-1@example.com" {
		t.Fatalf("marketing key must never reach the manifest, got %+v", m.Entries)
	}
	// dry-run reports the same plan without any write
	if _, err := os.Stat(state); err != nil {
		t.Fatalf("real run must write the manifest: %v", err)
	}
}

// TestRunOwnerStrictSplitsMixedCorpus proves the multi-owner pass model of
// gator #101 (defacto/Local_Folders): the same tree is imported once per
// owner in strict mode, and every message belongs to exactly one pass —
// a message whose owner appears in none of From/To/Cc/Delivered-To is
// skipped (counted Stats.Foreign), never quarantined into INBOX/Unmatched,
// because the sibling owner's pass handles it.
func TestRunOwnerStrictSplitsMixedCorpus(t *testing.T) {
	root := t.TempDir()
	// eslider-owned: incoming (To) + outgoing (From)
	writeMsg(t, root, "d0001/0000001.eml",
		"From: boss@example.com", "To: eslider@gmail.com", "Delivered-To: eslider@gmail.com",
		"Message-Id: <eslider-in@example.com>")
	writeMsg(t, root, "d0002/0000002.eml",
		"From: eslider@gmail.com", "To: friend@example.com", "Message-Id: <eslider-out@example.com>")
	// viscreation-owned: incoming + outgoing
	writeMsg(t, root, "d0003/0000003.eml",
		"From: boss@example.com", "To: viscreation@gmail.com", "Delivered-To: viscreation@gmail.com",
		"Message-Id: <vis-in@example.com>")
	writeMsg(t, root, "d0004/0000004.eml",
		"From: viscreation@gmail.com", "To: friend@example.com", "Message-Id: <vis-out@example.com>")
	// foreign to BOTH passes (another period's owner) — skipped by each
	writeMsg(t, root, "d0005/0000005.eml",
		"From: boss@example.com", "To: other@example.com", "Message-Id: <foreign@example.com>")

	esliderState := filepath.Join(t.TempDir(), "local-eslider.json")
	fakeE := &fakeTransport{}
	stE, err := Run(context.Background(), Options{
		Root: root, User: "eslider@gmail.com", Owner: "eslider@gmail.com", OwnerStrict: true,
		State: esliderState, Save: fakeE.save, Ensure: fakeE.ensure,
	})
	if err != nil {
		t.Fatalf("Run(eslider pass): %v", err)
	}
	if stE.Imported != 2 || stE.Foreign != 3 {
		t.Fatalf("eslider pass stats wrong (want imported=2 foreign=3): %+v", stE)
	}
	if stE.Targets[LayoutUnmatched] != 0 || stE.Targets[LayoutInbox] != 1 || stE.Targets[LayoutSent] != 1 {
		t.Fatalf("eslider pass targets wrong (no Unmatched in strict mode): %+v", stE.Targets)
	}
	eByKey := map[string]string{}
	for _, r := range fakeE.saved {
		eByKey[r.key] = r.mailbox
	}
	if eByKey["eslider-in@example.com"] != LayoutInbox || eByKey["eslider-out@example.com"] != LayoutSent {
		t.Fatalf("eslider pass routing wrong: %v", eByKey)
	}

	visState := filepath.Join(t.TempDir(), "local-viscreation.json")
	fakeV := &fakeTransport{}
	stV, err := Run(context.Background(), Options{
		Root: root, User: "viscreation@gmail.com", Owner: "viscreation@gmail.com", OwnerStrict: true,
		State: visState, Save: fakeV.save, Ensure: fakeV.ensure,
	})
	if err != nil {
		t.Fatalf("Run(viscreation pass): %v", err)
	}
	if stV.Imported != 2 || stV.Foreign != 3 {
		t.Fatalf("viscreation pass stats wrong (want imported=2 foreign=3): %+v", stV)
	}

	// every owned message imported exactly once across the two passes; the
	// foreign message by neither.
	got := map[string]bool{}
	for _, r := range fakeE.saved {
		got[r.key] = true
	}
	for _, r := range fakeV.saved {
		got[r.key] = true
	}
	want := []string{"eslider-in@example.com", "eslider-out@example.com", "vis-in@example.com", "vis-out@example.com"}
	if len(got) != len(want) {
		t.Fatalf("union of saved keys = %v, want %v (no double import)", got, want)
	}
	for _, k := range want {
		if !got[k] {
			t.Errorf("key %s must be imported exactly once, got %v", k, got)
		}
	}
	if got["foreign@example.com"] {
		t.Fatal("foreign message must not be imported by either strict pass")
	}
	for _, p := range []string{esliderState, visState} {
		m, err := LoadManifest(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range m.Entries {
			if e.Key == "foreign@example.com" {
				t.Fatalf("foreign key must not be recorded in %s: %+v", p, m.Entries)
			}
		}
	}

	// regression: without strict mode the same pass keeps the wheregroup/gmail
	// semantics — the foreign message is quarantined, not dropped.
	stL, err := Run(context.Background(), Options{
		Root: root, User: "eslider@gmail.com", Owner: "eslider@gmail.com",
		State: filepath.Join(t.TempDir(), "loose.json"), Save: fakeE.save, Ensure: fakeE.ensure,
	})
	if err != nil {
		t.Fatalf("Run(layout non-strict): %v", err)
	}
	if stL.Targets[LayoutUnmatched] != 3 {
		t.Fatalf("non-strict layout must quarantine foreign mail into Unmatched, got %+v", stL.Targets)
	}
}
