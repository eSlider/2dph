package source

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eSlider/2dph/internal/mailconv"
)

// swapReadPST replaces the readpst invocation seam with fn for one test and
// restores the original afterwards. Offline tests never invoke a real readpst.
func swapReadPST(fn func(context.Context, string, string, string) error) func() {
	prev := runReadPST
	runReadPST = fn
	return func() { runReadPST = prev }
}

// fakeReadPST mimics readpst -e: it writes one .eml per "message" under
// out/<Folder>/, named 1.eml, 2.eml … (readpst naming). Content is synthetic
// (Alice/Bob/example.com) and derived from the pst path so two sources yield
// distinct messages. It reproduces the German Outlook folder layout of the
// real archives, including a Drafts folder the #79 policy must exclude.
func fakeReadPST(_ context.Context, _ string, pstPath, out string) error {
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	base := filepath.Base(pstPath)
	for i, folder := range []string{"Posteingang", "Entwürfe"} {
		dir := filepath.Join(out, "Persönliche Ordner", folder)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
		n := i + 1
		eml := "From: Alice <alice@example.com>\n" +
			"To: bob@example.com\n" +
			"Subject: pst " + base + " #" + string(rune('0'+n)) + "\n" +
			"Date: Tue, 18 Aug 2026 09:00:00 +0000\n" +
			"MIME-Version: 1.0\n" +
			"Content-Type: text/plain; charset=utf-8\n\n" +
			"Message " + string(rune('0'+n)) + " from " + base + ".\n"
		if err := os.WriteFile(filepath.Join(dir, string(rune('0'+n))+".eml"), []byte(eml), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// pstFixture builds a temp PST source (synthetic path) plus staging/out/state
// dirs and returns the ImportOptions for one source.
func pstFixture(t *testing.T) ImportOptions {
	t.Helper()
	tmp := t.TempDir()
	pstPath := filepath.Join(tmp, "fixtures", "andriy.pst")
	if err := os.MkdirAll(filepath.Dir(pstPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pstPath, []byte("fake pst bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	return ImportOptions{
		Sources:   []PSTSource{{Label: "pst-andriy", Path: pstPath}},
		Staging:   filepath.Join(tmp, "staging"),
		Out:       filepath.Join(tmp, "corpus", "mail", "pst"),
		StatePath: filepath.Join(tmp, "state", "pst.json"),
	}
}

// TestImportPSTConvertsEML proves the end-to-end import path: fake readpst
// extract → content-addressed corpus copy → mailconv.FromEML conversion, with
// the #79 policy excluding the Drafts (Entwürfe) folder and the state
// checkpoint recording exactly the converted messages.
func TestImportPSTConvertsEML(t *testing.T) {
	defer swapReadPST(fakeReadPST)()
	o := pstFixture(t)

	st, conv, err := ImportPST(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if st.Fetched != 1 || st.New != 1 || st.Skipped != 0 {
		t.Fatalf("stats = %+v, want Fetched=1 New=1 Skipped=0 (Entwürfe excluded)", st)
	}
	if conv.OK != 1 || conv.Fail != 0 {
		t.Fatalf("conversion = %+v, want OK=1 Fail=0", conv)
	}
	if got := countFiles(o.Out, ".eml"); got != 1 {
		t.Fatalf(".eml in corpus = %d, want 1", got)
	}
	if got := countFiles(o.Out, ".md"); got != 1 {
		t.Fatalf("converted message.md = %d, want 1", got)
	}
	// The converted message keeps its PST folder and subject (canon conversion).
	msg := readMessageJSON(t, o.Out)
	if msg.Folder != "Posteingang" {
		t.Errorf("Folder = %q, want Posteingang (innermost readpst folder)", msg.Folder)
	}
	if !strings.Contains(msg.Subject, "andriy.pst") {
		t.Errorf("Subject = %q, want it to carry the source label", msg.Subject)
	}
	if !strings.Contains(msg.TextBody, "Message 1") {
		t.Errorf("TextBody = %q, want the synthetic body", msg.TextBody)
	}
	// State checkpoint: cursor + exactly one seen id.
	cp, err := loadCheckpoint(o.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cp.Seen) != 1 {
		t.Fatalf("state seen-set = %d ids, want 1", len(cp.Seen))
	}
}

// TestImportPSTIdempotentRerun proves a second run converts nothing: the
// re-extracted .eml (same content) is deduped by the sha256 seen-set, the
// corpus tree does not grow and the checkpoint stays stable.
func TestImportPSTIdempotentRerun(t *testing.T) {
	defer swapReadPST(fakeReadPST)()
	o := pstFixture(t)

	if _, _, err := ImportPST(context.Background(), o); err != nil {
		t.Fatal(err)
	}
	eml1, md1 := countFiles(o.Out, ".eml"), countFiles(o.Out, ".md")

	st, conv, err := ImportPST(context.Background(), o)
	if err != nil {
		t.Fatal(err)
	}
	if st.Fetched != 1 || st.New != 0 || st.Skipped != 1 {
		t.Fatalf("re-run stats = %+v, want Fetched=1 New=0 Skipped=1 (no dupes)", st)
	}
	if conv.OK != 0 || conv.Skip != 1 || conv.Fail != 0 {
		t.Fatalf("re-run conversion = %+v, want OK=0 Skip=1 Fail=0", conv)
	}
	if eml2, md2 := countFiles(o.Out, ".eml"), countFiles(o.Out, ".md"); eml2 != eml1 || md2 != md1 {
		t.Fatalf("corpus grew on re-run: .eml %d→%d, .md %d→%d", eml1, eml2, md1, md2)
	}
	cp, err := loadCheckpoint(o.StatePath)
	if err != nil {
		t.Fatal(err)
	}
	if len(cp.Seen) != 1 {
		t.Fatalf("state seen-set = %d ids, want 1 (stable)", len(cp.Seen))
	}
}

// TestPlanPSTDryRun proves the --dry-run plan lines: sources → targets, the
// converter resolution and the scratch/state paths — and that planning touches
// no filesystem path.
func TestPlanPSTDryRun(t *testing.T) {
	o := ImportOptions{
		Sources: []PSTSource{
			{Label: "pst-andriy", Path: "fixtures/andriy.pst"},
			{Label: "pst-backup-vorlagen", Path: "fixtures/backup-vorlagen.pst"},
		},
		Staging:   "var/tmp/pst",
		Out:       "var/corpus/mail/pst",
		ReadPST:   "var/dist/readpst/usr/bin/readpst",
		StatePath: "var/state/pst.json",
	}
	lines := PlanPST(o)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"pst-andriy", "pst-backup-vorlagen",
		"fixtures/andriy.pst", "fixtures/backup-vorlagen.pst",
		"var/dist/readpst/usr/bin/readpst",
		"var/corpus/mail/pst", "var/state/pst.json", "var/tmp/pst",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("plan missing %q:\n%s", want, joined)
		}
	}
	for _, p := range []string{o.Out, o.Staging, o.StatePath} {
		if _, err := os.Stat(p); err == nil {
			t.Errorf("dry-run plan created %s", p)
		}
	}
}

// TestPSTFetchValidation proves the adapter fails loudly on a misconfigured
// pst.* section (missing keys point at the config, D28).
func TestPSTFetchValidation(t *testing.T) {
	defer swapReadPST(fakeReadPST)()
	cases := []struct {
		name string
		src  *PST
		want string
	}{
		{"no sources", &PST{Staging: "s", Out: "o"}, "Sources is empty"},
		{"no staging", &PST{Sources: []PSTSource{{Label: "a", Path: "p"}}, Out: "o"}, "Staging is empty"},
		{"no out", &PST{Sources: []PSTSource{{Label: "a", Path: "p"}}, Staging: "s"}, "Out is empty"},
		{"empty path", &PST{Sources: []PSTSource{{Label: "a"}}, Staging: "s", Out: "o"}, "empty path"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := tc.src.Fetch(context.Background(), "")
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Fetch error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestPSTReadPSTFailure proves a converter failure aborts the run with the
// source label in the error (no silent partial state).
func TestPSTReadPSTFailure(t *testing.T) {
	defer swapReadPST(func(_ context.Context, _ string, _, _ string) error {
		return errors.New("converter exploded")
	})()
	o := pstFixture(t)
	_, _, err := ImportPST(context.Background(), o)
	if err == nil || !strings.Contains(err.Error(), "pst-andriy") || !strings.Contains(err.Error(), "converter exploded") {
		t.Fatalf("ImportPST error = %v, want source label + converter error", err)
	}
}

// TestPSTResolveReadPST proves the binary resolution order: explicit config
// path → PATH → repo-local toolchain dir (var/dist, the no-root install path)
// → explicit error pointing at the config.
func TestPSTResolveReadPST(t *testing.T) {
	// Explicit override always wins.
	if got, err := resolveReadPST("/opt/bin/readpst"); err != nil || got != "/opt/bin/readpst" {
		t.Fatalf("override = %q, %v; want /opt/bin/readpst", got, err)
	}
	// PATH lookup.
	binDir := t.TempDir()
	fake := filepath.Join(binDir, "readpst")
	if err := os.WriteFile(fake, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)
	if got, err := resolveReadPST(""); err != nil || got != fake {
		t.Fatalf("PATH lookup = %q, %v; want %s", got, err, fake)
	}
	// Repo-local toolchain dir (KB_ROOT points at a fake root).
	root := t.TempDir()
	local := filepath.Join(root, "var", "dist", "readpst", "usr", "bin", "readpst")
	if err := os.MkdirAll(filepath.Dir(local), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(local, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("KB_ROOT", root)
	t.Setenv("PATH", t.TempDir())
	if got, err := resolveReadPST(""); err != nil || got != local {
		t.Fatalf("local toolchain = %q, %v; want %s", got, err, local)
	}
	// Nothing found → error pointing at the config.
	t.Setenv("KB_ROOT", t.TempDir())
	_, err := resolveReadPST("")
	if err == nil || !strings.Contains(err.Error(), "config.yml") {
		t.Fatalf("missing readpst error = %v, want it to point at the config", err)
	}
}

func countFiles(root, ext string) int {
	n := 0
	_ = filepath.WalkDir(root, func(_ string, d os.DirEntry, _ error) error {
		if !d.IsDir() && strings.EqualFold(filepath.Ext(d.Name()), ext) {
			n++
		}
		return nil
	})
	return n
}

func readMessageJSON(t *testing.T, root string) mailconv.Message {
	t.Helper()
	var path string
	_ = filepath.WalkDir(root, func(p string, d os.DirEntry, _ error) error {
		if !d.IsDir() && d.Name() == "message.json" {
			path = p
			return filepath.SkipAll
		}
		return nil
	})
	if path == "" {
		t.Fatal("no message.json in corpus")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var msg mailconv.Message
	if err := json.Unmarshal(raw, &msg); err != nil {
		t.Fatal(err)
	}
	return msg
}
