package incubator

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/eSlider/2dph/pkg/utils"
)

// ErrRejected marks a per-message save rejection by the mail server (e.g.
// Dovecot quota_max_mail_size: the message exceeds the server's maximum mail
// size). The message is NOT recorded in the manifest (it was never imported)
// and the run continues past it — one oversized message must not block the
// rest of the corpus. Re-runs retry it and keep reporting it until the
// server-side constraint is lifted or the message is handled (decision for
// the operator / epic B, never silently dropped).
var ErrRejected = errors.New("incubator: message rejected by the mail server")

// SaveFunc pushes one raw message into the owner's mailbox. The production
// transport is `docker exec -i <container> doveadm save -u <user> -m <mb>`
// with the .eml on stdin (no bind-mount of the legacy corpus needed, epic
// #250 open question 3 — doveadm save reads the message over stdin).
type SaveFunc func(ctx context.Context, mailbox string, raw []byte) error

// EnsureFunc makes sure a target mailbox exists (doveadm mailbox create);
// folder mode needs it because doveadm save does NOT autocreate the target
// mailbox (verified live on mail-server, 2026-09-02).
type EnsureFunc func(ctx context.Context, mailbox string) error

// Options configure one incubator import run (issue #252).
type Options struct {
	// Root is the source profile root (machine-local path from config.local.yml).
	Root string
	// User is the doveadm mailbox owner (the incubator account, e.g.
	// andriy.oblivantsev@wheregroup.com).
	User string
	// Owner is the historical address of the owner matched in message headers
	// (decision 2026-09-02, #252: the incubator mailbox IS that address).
	// When set, routing is by recipient: owner in From → Sent, owner in
	// To/CC/Delivered-To → INBOX, owner nowhere → INBOX/Unmatched. Empty =
	// legacy flat/folder routing (Mailbox / Folders).
	Owner string
	// OwnerStrict tightens recipient routing for multi-owner corpora (gator
	// #101, defacto/Local_Folders): a message whose owner appears in NONE of
	// From/To/Cc/Delivered-To is NOT quarantined into INBOX/Unmatched — it is
	// skipped (Stats.Foreign), because the sibling owner's pass imports it.
	// Requires Owner. Without it the layout behaves exactly as before.
	OwnerStrict bool
	// SkipState lists additional read-only manifests whose keys count as
	// already imported — the global dedup of gator #101. Each defacto import
	// lists the manifests of the already-imported channels (гдеgroup/gmail)
	// plus the sibling slices of the same multi-owner corpus, so a canonical
	// Message-ID already present in gator is never imported twice. Read-only:
	// entries are never appended here (the run writes only Options.State).
	SkipState []string
	// SkipFrom lists sender addresses (From header, case-insensitive
	// addr-spec) whose messages are dropped before dedup and import — the
	// marketing filter of gator #101 (viscreation@gmx_de is 92%
	// gewinnspiel@loewe.de). Filtered messages are not imported, never
	// written to the manifest and counted in Stats.Filtered.
	SkipFrom []string
	// State is the manifest path (var/state/incubator-<label>.json).
	State string
	// Docker is the docker binary; empty = PATH lookup.
	Docker string
	// Container is the docker-mailserver container name.
	Container string
	// Limit caps the per-run window (pilot = 1000); 0 = no limit.
	Limit int
	// Folders replicates the source folder tree into the mailbox (issue #252
	// п.3); false = flat import into Mailbox (default INBOX).
	Folders bool
	// Mailbox is the flat-mode target; empty = INBOX.
	Mailbox string
	// Force ignores the manifest and re-imports the window (only after the
	// target mailbox was wiped — doveadm save does not dedup).
	Force bool
	// Dry scans + plans only: no transport calls, no manifest write.
	Dry bool
	// Save/Ensure are transport seams; nil defaults to the docker transport.
	Save   SaveFunc
	Ensure EnsureFunc
}

// Stats report what one run did (issue #252 acceptance numbers).
type Stats struct {
	Scanned      int            // messages found in the tree (attachments excluded)
	Unique       int            // distinct dedup keys in the scan (after the sender filter)
	NoID         int            // id-less messages (body-hash fallback keys)
	Filtered     int            // dropped by Options.SkipFrom before dedup (marketing, gator #101)
	Window       int            // messages considered this run (limit window)
	Already      int            // in window, key already in this source's manifest
	AlreadyOther int            // in window, key already imported by another source (Options.SkipState)
	DupInRun     int            // in window, key already imported earlier this run
	Foreign      int            // dropped by strict-owner routing: another pass's message (gator #101)
	Imported     int            // saved to the mailbox (or planned, in dry-run)
	ByMailbox    map[string]int // window size per source folder (issue п.3 карта)
	// Targets counts the save targets of imported messages — the routing
	// result of layout mode (Sent/INBOX/INBOX/Unmatched, #252).
	Targets map[string]int
	// Rejected counts messages the mail server refused to save (ErrRejected,
	// e.g. oversized > quota_max_mail_size). The run continues past them; they
	// are never recorded in the manifest, so a re-run retries them.
	Rejected int
	// RejectedPaths lists the source paths of rejected messages, so the
	// operator can act on them (lift the server constraint / handle the
	// message) — never silently dropped (epic B decision).
	RejectedPaths []string
}

// manifestSaveEvery is the crash-resume checkpoint cadence: the manifest is
// written atomically every N imports, so a mid-run failure loses at most N
// entries (a re-run would re-import those — the only duplication window).
const manifestSaveEvery = 25

// Run executes one incubator import: scan → manifest dedup → doveadm save.
// Dry runs (Options.Dry) only scan + count, never touch the transport or the
// manifest. The run window is path-ascending, so a re-run with the same limit
// sees the same files and imports 0 new (idempotency by manifest, issue #252).
func Run(ctx context.Context, o Options) (Stats, error) {
	var st Stats
	if o.Root == "" {
		return st, errors.New("incubator: Options.Root is required")
	}
	if o.User == "" {
		return st, errors.New("incubator: Options.User is required")
	}
	if o.State == "" {
		return st, errors.New("incubator: Options.State is required")
	}
	if o.Folders && o.Owner != "" {
		return st, errors.New("incubator: recipient layout (Owner) and Folders are mutually exclusive")
	}
	if o.OwnerStrict && o.Owner == "" {
		return st, errors.New("incubator: Options.OwnerStrict requires Options.Owner")
	}
	layoutMode := o.Owner != ""

	manifest, err := LoadManifest(o.State)
	if err != nil {
		return st, err
	}
	if o.Force {
		manifest = Manifest{Version: manifestVersion}
	}

	save, ensure, err := transports(ctx, o)
	if err != nil {
		return st, err
	}

	msgs, err := Scan(o.Root)
	if err != nil {
		return st, err
	}
	st.Scanned = len(msgs)

	// Sender filter (SkipFrom, gator #101): marketing mailings are dropped
	// BEFORE dedup counting — they are not corpus for this import at all
	// (never imported, never manifested). Filtered messages stay out of
	// Unique/NoID/ByMailbox and of the limit window.
	if len(o.SkipFrom) > 0 {
		kept := make([]Message, 0, len(msgs))
		for _, m := range msgs {
			raw, err := os.ReadFile(filepath.Join(o.Root, filepath.FromSlash(m.Rel)))
			if err != nil {
				return st, fmt.Errorf("incubator: read %s: %w", m.Rel, err)
			}
			drop, err := FromMatches(raw, o.SkipFrom)
			if err != nil {
				return st, fmt.Errorf("incubator: filter %s: %w", m.Rel, err)
			}
			if drop {
				st.Filtered++
				continue
			}
			kept = append(kept, m)
		}
		msgs = kept
	}

	st.ByMailbox = make(map[string]int, 16)
	uniq := make(map[string]struct{}, len(msgs))
	for _, m := range msgs {
		st.ByMailbox[m.Mailbox]++
		uniq[m.Key] = struct{}{}
		if !m.HasID {
			st.NoID++
		}
	}
	st.Unique = len(uniq)
	// seen = this source's own manifest (re-run idempotency, cleared by
	// Force — the operator wiped the target mailbox).
	seen := make(map[string]struct{}, len(manifest.Entries))
	for _, e := range manifest.Entries {
		seen[e.Key] = struct{}{}
	}
	// otherSeen = the global key-store of gator #101: manifests of the
	// already-imported sources (Options.SkipState). Never cleared by Force —
	// a forced re-import of one slice must not duplicate a letter that is
	// already in gator from another channel.
	otherSeen := make(map[string]struct{})
	for _, p := range o.SkipState {
		sm, err := LoadManifest(p) // missing file = not yet imported (empty)
		if err != nil {
			return st, fmt.Errorf("incubator: skip-state %s: %w", p, err)
		}
		for _, e := range sm.Entries {
			otherSeen[e.Key] = struct{}{}
		}
	}

	window := msgs
	if o.Limit > 0 && len(window) > o.Limit {
		window = window[:o.Limit]
	}
	st.Window = len(window)

	target := o.Mailbox
	if !o.Folders && !layoutMode && target == "" {
		target = "INBOX"
	}
	if layoutMode {
		st.Targets = make(map[string]int, 4)
	}
	ensured := make(map[string]bool, 16)
	inRun := make(map[string]struct{}, st.Window)
	for _, m := range window {
		if err := ctx.Err(); err != nil {
			saveManifestBestEffort(manifest, o.State)
			return st, err
		}
		if !o.Force {
			if _, ok := seen[m.Key]; ok {
				st.Already++
				continue
			}
		}
		if _, ok := otherSeen[m.Key]; ok {
			// already in gator from another channel (global dedup, #101)
			st.AlreadyOther++
			continue
		}
		if _, ok := inRun[m.Key]; ok {
			st.DupInRun++
			continue
		}
		mb := m.Mailbox
		var raw []byte
		if layoutMode {
			// Routing needs the headers, so the file is read even in dry-run.
			raw, err = os.ReadFile(filepath.Join(o.Root, filepath.FromSlash(m.Rel)))
			if err != nil {
				return st, fmt.Errorf("incubator: read %s: %w", m.Rel, err)
			}
			if mb, err = LayoutOf(raw, o.Owner); err != nil {
				return st, fmt.Errorf("incubator: layout %s: %w", m.Rel, err)
			}
			if o.OwnerStrict && mb == LayoutUnmatched {
				// Not this owner's message: the sibling pass of the
				// multi-owner corpus imports it — never quarantined, never
				// imported here (gator #101, defacto/Local_Folders).
				st.Foreign++
				continue
			}
		} else if !o.Folders {
			mb = target
		}
		if !o.Dry {
			if raw == nil {
				raw, err = os.ReadFile(filepath.Join(o.Root, filepath.FromSlash(m.Rel)))
				if err != nil {
					return st, fmt.Errorf("incubator: read %s: %w", m.Rel, err)
				}
			}
			if mb != "INBOX" && !ensured[mb] {
				if err := ensure(ctx, mb); err != nil {
					saveManifestBestEffort(manifest, o.State)
					return st, fmt.Errorf("incubator: ensure mailbox %s: %w", mb, err)
				}
				ensured[mb] = true
			}
			if err := save(ctx, mb, raw); err != nil {
				if errors.Is(err, ErrRejected) {
					// Server refused THIS message (e.g. too large). Not a
					// transport failure: count it, keep the rest of the
					// window going, do not record it in the manifest (a
					// re-run retries it until the constraint is lifted).
					st.Rejected++
					st.RejectedPaths = append(st.RejectedPaths, m.Rel)
					continue
				}
				saveManifestBestEffort(manifest, o.State)
				return st, fmt.Errorf("incubator: %s → %s: %w", m.Rel, mb, err)
			}
			manifest.Add(m.Key, m.Rel, mb)
			if len(manifest.Entries)%manifestSaveEvery == 0 {
				if err := manifest.Save(o.State); err != nil {
					return st, err
				}
			}
		}
		st.Imported++
		if st.Targets != nil {
			st.Targets[mb]++
		}
		inRun[m.Key] = struct{}{}
	}

	if !o.Dry {
		if err := manifest.Save(o.State); err != nil {
			return st, err
		}
	}
	return st, nil
}

// transports wires the Save/Ensure seams: explicit Options funcs win, else
// the docker exec doveadm transport (resolving the docker binary once).
func transports(ctx context.Context, o Options) (SaveFunc, EnsureFunc, error) {
	if o.Save != nil && o.Ensure != nil {
		return o.Save, o.Ensure, nil
	}
	bin := o.Docker
	if bin == "" {
		p, err := exec.LookPath("docker")
		if err != nil {
			return nil, nil, errors.New("incubator: docker not found — install docker or set Options.Docker")
		}
		bin = p
	}
	if o.Container == "" {
		return nil, nil, errors.New("incubator: Options.Container is required (docker-mailserver container)")
	}
	save := func(ctx context.Context, mailbox string, raw []byte) error {
		return DoveadmSave(ctx, bin, o.Container, o.User, mailbox, raw)
	}
	ensure := func(ctx context.Context, mailbox string) error {
		return DoveadmEnsureMailbox(ctx, bin, o.Container, o.User, mailbox)
	}
	return save, ensure, nil
}

func saveManifestBestEffort(m Manifest, path string) {
	_ = m.Save(path) // failure on the error path is not actionable
}

// doveadm runs one doveadm command inside the mailserver container with the
// given stdin payload.
func doveadm(ctx context.Context, bin, container string, args []string, stdin io.Reader) ([]byte, error) {
	cmd := exec.CommandContext(ctx, bin, append([]string{"exec", "-i", container, "doveadm"}, args...)...)
	cmd.Stdin = stdin
	out, err := cmd.CombinedOutput()
	if err != nil {
		return out, err
	}
	return out, nil
}

// DoveadmSave saves one raw message into user's mailbox via
// `doveadm save -u <user> -m <mailbox>` (message over stdin). When doveadm
// refuses the message itself (exit status 65, "Saving failed" — e.g. the
// message exceeds quota_max_mail_size), the error wraps ErrRejected so the
// run can skip that message and continue; transport failures stay fatal.
func DoveadmSave(ctx context.Context, docker, container, user, mailbox string, raw []byte) error {
	out, err := doveadm(ctx, docker, container, []string{"save", "-u", user, "-m", mailbox}, bytes.NewReader(raw))
	if err != nil {
		msg := string(out)
		if bytes.Contains(out, []byte("Saving failed")) || strings.Contains(err.Error(), "Saving failed") {
			return fmt.Errorf("%w: doveadm save %s %s: %s", ErrRejected, user, mailbox, utils.Snippet(msg, 512))
		}
		return fmt.Errorf("doveadm save %s %s: %w: %s", user, mailbox, err, utils.Snippet(msg, 512))
	}
	return nil
}

// DoveadmEnsureMailbox creates a mailbox (`doveadm mailbox create`). An
// "already exists" result is success — user-patches.sh relies on the same
// idempotency.
func DoveadmEnsureMailbox(ctx context.Context, docker, container, user, mailbox string) error {
	out, err := doveadm(ctx, docker, container, []string{"mailbox", "create", "-u", user, mailbox}, nil)
	if err != nil {
		if bytes.Contains(out, []byte("already exists")) {
			return nil
		}
		return fmt.Errorf("doveadm mailbox create %s %s: %w: %s", user, mailbox, err, utils.Snippet(string(out), 512))
	}
	return nil
}
