package incubator

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// fakeFailTransport is fakeTransport plus a per-key save failure map — one
// message can be rejected by the "server" (e.g. Dovecot quota_max_mail_size)
// while the others save fine, exactly the live гдеgroup defect 2026-09-02
// (11.28 MB message > 10M Dovecot limit).
type fakeFailTransport struct {
	fakeTransport
	failFor map[string]error // message key → save error
}

func (f *fakeFailTransport) save(ctx context.Context, mailbox string, raw []byte) error {
	k, _, err := Key(raw)
	if err != nil {
		return err
	}
	if e, ok := f.failFor[k]; ok {
		f.saveCalls++
		return e
	}
	return f.fakeTransport.save(ctx, mailbox, raw)
}

// rejectErr mimics the doveadm save rejection error text (exit status 65,
// "Saving failed" — the server refuses THIS message, e.g. oversized).
var rejectErr = fmt.Errorf("%w: doveadm save u@example.com INBOX: exit status 65: doveadm(u@example.com): Error: Saving failed: Mail size is larger than the maximum size allowed by server configuration", ErrRejected)

// writeN writes n distinct messages (keys <prefix>-<i>@example.com) into root.
func writeN(t *testing.T, root, prefix string, n int) {
	t.Helper()
	for i := 1; i <= n; i++ {
		dir := filepath.Join(root, fmt.Sprintf("d%04d", i))
		p := filepath.Join(dir, "m.eml")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		eml := "From: a@example.com\nTo: b@example.com\nMessage-Id: <" + prefix + "-" + fmt.Sprintf("%d", i) + "@example.com>\nSubject: x\n\nbody\n"
		if err := os.WriteFile(p, []byte(eml), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// TestRunContinuesPastRejectedMessage proves the per-message resilience fix:
// one message the server rejects (oversized > quota_max_mail_size) must NOT
// abort the run — it is counted in Stats.Rejected and skipped, the rest of
// the window imports, and the manifest records only the imported ones.
func TestRunContinuesPastRejectedMessage(t *testing.T) {
	root := t.TempDir()
	writeN(t, root, "m", 3)
	state := filepath.Join(t.TempDir(), "state.json")
	fake := &fakeFailTransport{failFor: map[string]error{
		"m-2@example.com": rejectErr,
	}}
	o := Options{Root: root, User: "u@example.com", State: state,
		Save: fake.save, Ensure: fake.ensure}
	st, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("Run must not abort on a rejected message: %v", err)
	}
	if st.Imported != 2 {
		t.Fatalf("Imported = %d, want 2 (the 2 accepted messages)", st.Imported)
	}
	if st.Rejected != 1 {
		t.Fatalf("Rejected = %d, want 1", st.Rejected)
	}
	if fake.saveCalls != 3 {
		t.Fatalf("saveCalls = %d, want 3 (2 ok + 1 rejected attempt)", fake.saveCalls)
	}
	// manifest holds only the imported messages
	m, err := LoadManifest(state)
	if err != nil {
		t.Fatal(err)
	}
	if len(m.Entries) != 2 {
		t.Fatalf("manifest entries = %d, want 2 (rejected message never recorded)", len(m.Entries))
	}
	for _, e := range m.Entries {
		if e.Key == "m-2@example.com" {
			t.Fatalf("rejected key must not be in the manifest: %+v", e)
		}
	}
}

// TestRunReRunRetriesRejectedMessage proves idempotency semantics after a
// rejection: the re-run imports 0 new accepted messages (manifest) but still
// reports the rejected one — nothing is silently lost, the operator sees it
// every run until the server-side constraint is lifted.
func TestRunReRunRetriesRejectedMessage(t *testing.T) {
	root := t.TempDir()
	writeN(t, root, "m", 3)
	state := filepath.Join(t.TempDir(), "state.json")

	fake := &fakeFailTransport{failFor: map[string]error{"m-2@example.com": rejectErr}}
	o := Options{Root: root, User: "u@example.com", State: state,
		Save: fake.save, Ensure: fake.ensure}
	if _, err := Run(context.Background(), o); err != nil {
		t.Fatal(err)
	}

	// Re-run: same rejection still in place.
	fake2 := &fakeFailTransport{failFor: map[string]error{"m-2@example.com": rejectErr}}
	o.Save, o.Ensure = fake2.save, fake2.ensure
	st, err := Run(context.Background(), o)
	if err != nil {
		t.Fatalf("re-run: %v", err)
	}
	if st.Imported != 0 || st.Already != 2 {
		t.Fatalf("re-run stats wrong (want imported=0 already=2): %+v", st)
	}
	if st.Rejected != 1 {
		t.Fatalf("re-run Rejected = %d, want 1 (the oversized message is retried and re-reported)", st.Rejected)
	}
	if fake2.saveCalls != 1 {
		t.Fatalf("re-run must attempt only the rejected message, got %d saves", fake2.saveCalls)
	}
}
