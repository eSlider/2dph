package source

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/eSlider/2dph/internal/etl"
	"github.com/eSlider/2dph/internal/mailconv"
	"github.com/eSlider/2dph/pkg/utils"
)

// PST is the sync-ETL adapter for Outlook .pst archives (#185): it runs
// readpst -e on each configured source into a wiped staging dir and yields one
// Blob per extracted .eml message, copied content-addressed into the corpus
// (<Out>/<label>/<folder>/<sha256:16>/<sha256:16>.eml — the same layout as
// the mbox splitter). It closes the PST import gap of #79.
//
// The cursor never advances (like Disk): Fetch re-runs readpst and re-lists
// staging on every call; the driver's sha256 seen-set supplies idempotency, so
// a re-run converts nothing twice even though readpst overwrites the staging
// files. readpst extraction is deterministic (item order + folder names), so
// content IDs are stable across runs. Folders the #79 policy excludes
// (Drafts/Templates/Trash/Junk/Spam/Unsent, incl. German Outlook names) are
// skipped before the corpus copy.
type PST struct {
	// Sources are the .pst files to convert, one label+path pair each (from
	// config pst.sources, see #79). The label names the corpus subdir.
	Sources []PSTSource
	// Staging is the scratch root readpst writes into; it is wiped per source
	// before every extraction (var/tmp/pst). Never point it at the corpus.
	Staging string
	// Out is the corpus root for extracted mail (var/corpus/mail/pst).
	Out string
	// ReadPST is the readpst binary path override; empty = PATH lookup, then
	// the repo-local var/dist toolchain dir, then an explicit config error.
	ReadPST string
}

// PSTSource is one .pst file to import: Label (corpus subdir / source tag) +
// Path (absolute .pst path from config, see #79).
type PSTSource struct {
	Label string
	Path  string
}

// Name is the checkpoint file stem: var/state/pst.json.
func (p *PST) Name() string { return "pst" }

// Fetch extracts every configured source and returns one Blob per extracted
// .eml message (policy-excluded folders skipped). Blob IDs are content hashes,
// so the driver seen-set skips unchanged messages on re-runs.
func (p *PST) Fetch(ctx context.Context, _ Cursor) ([]Blob, Cursor, error) {
	if len(p.Sources) == 0 {
		return nil, "", errors.New("source: pst Sources is empty — configure pst.sources in etc/brain/config.yml (see #79)")
	}
	if p.Staging == "" {
		return nil, "", errors.New("source: pst Staging is empty — set pst.staging in etc/brain/config.yml")
	}
	if p.Out == "" {
		return nil, "", errors.New("source: pst Out is empty — set pst.out in etc/brain/config.yml")
	}
	for _, s := range p.Sources {
		if s.Path == "" {
			return nil, "", fmt.Errorf("source: pst source %q has an empty path — see pst.sources in etc/brain/config.yml (#79)", s.Label)
		}
		dir := filepath.Join(p.Staging, mailconv.SanitizeSegment(s.Label))
		// readpst is not idempotent on a non-empty out dir (it appends "1" to
		// re-used folder names), so each run extracts into a fresh dir.
		if err := os.RemoveAll(dir); err != nil {
			return nil, "", fmt.Errorf("source: wipe pst staging %s: %w", dir, err)
		}
		if err := runReadPST(ctx, p.ReadPST, s.Path, dir); err != nil {
			return nil, "", fmt.Errorf("source: readpst %s: %w", s.Label, err)
		}
	}
	files, err := etl.WalkFiles(p.Staging, etl.WalkOptions{Exts: []string{".eml"}})
	if err != nil {
		return nil, "", err
	}
	blobs := make([]Blob, 0, len(files))
	for _, f := range files {
		if skipPolicyDir(f.Rel) {
			continue
		}
		sum, err := sha256File(f.Path)
		if err != nil {
			return nil, "", err
		}
		target, err := p.corpusTarget(f.Rel, sum)
		if err != nil {
			return nil, "", err
		}
		if _, err := os.Stat(target); err != nil {
			if err := copyFile(f.Path, target); err != nil {
				return nil, "", err
			}
		}
		blobs = append(blobs, Blob{ID: sum, Kind: "mail", Path: target})
	}
	return blobs, "", nil
}

// corpusTarget maps a staging-relative .eml to its content-addressed corpus
// path: Out/<label>/<sanitized folders>/<sha256:16>/<sha256:16>.eml (mirrors
// the mbox layout, mailconv.SplitMboxDir).
func (p *PST) corpusTarget(rel, sum string) (string, error) {
	segs := strings.Split(filepath.ToSlash(rel), "/")
	if len(segs) < 2 {
		return "", fmt.Errorf("source: unexpected pst staging path %q", rel)
	}
	base := []string{p.Out, mailconv.SanitizeSegment(segs[0])}
	for _, seg := range segs[1 : len(segs)-1] {
		base = append(base, mailconv.SanitizeSegment(seg))
	}
	id := sum[:16]
	return filepath.Join(append(base, id, id+".eml")...), nil
}

// skipPolicyDir reports whether a staging-relative path sits under a mail
// folder the import policy excludes (#79): Drafts/Templates/Trash/Junk/Spam/
// Unsent, incl. the German Outlook folder names readpst emits
// (mailconv.SkipFolder is the single policy implementation).
func skipPolicyDir(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if mailconv.SkipFolder(seg) {
			return true
		}
	}
	return false
}

// copyFile copies one extracted .eml into its content-addressed corpus target
// (mkdir -p the parent; small MIME files, no streaming needed).
func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

// runReadPST runs `readpst -e <pst> -o <out>` (one .eml per message, folders
// as subdirs). It is a package-level variable so tests substitute a fake
// converter (test seam): offline tests never invoke a real readpst.
var runReadPST = func(ctx context.Context, bin, pstPath, out string) error {
	bin, err := resolveReadPST(bin)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	cmd := exec.CommandContext(ctx, bin, "-e", "-o", out, pstPath)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("readpst %s: %w: %s", pstPath, err, utils.Snippet(string(output), 512))
	}
	return nil
}

// resolveReadPST locates the readpst binary: an explicit path wins, then PATH,
// then the repo-local toolchain dir (var/dist/readpst — the no-root install
// path for hosts without libpst-utils). The final error points at the config.
func resolveReadPST(bin string) (string, error) {
	if bin != "" {
		return bin, nil
	}
	if p, err := exec.LookPath("readpst"); err == nil {
		return p, nil
	}
	if root := utils.Root(); root != "" && root != "." {
		local := filepath.Join(root, "var", "dist", "readpst", "usr", "bin", "readpst")
		if _, err := os.Stat(local); err == nil {
			return local, nil
		}
	}
	return "", errors.New("readpst not found: install libpst-utils (Ubuntu 24.04: pst-utils) or set pst.readpst in etc/brain/config.yml")
}

// ImportOptions wires one PST import run (bin/mail/import-pst.go): the sources
// plus the staging/out/state dirs derived from config by the CLI.
type ImportOptions struct {
	Sources   []PSTSource
	Staging   string
	Out       string
	ReadPST   string
	StatePath string
}

// ConvStats reports the mailconv.FromEML conversion pass of one run.
type ConvStats struct {
	OK   int
	Skip int
	Fail int
}

// PlanPST returns the --dry-run lines for an import: every configured source
// with its corpus target, the converter resolution and the scratch/state
// paths. No filesystem writes happen.
func PlanPST(o ImportOptions) []string {
	lines := make([]string, 0, len(o.Sources)+3)
	for _, s := range o.Sources {
		lines = append(lines, fmt.Sprintf("pst %q: %s → %s", s.Label, s.Path,
			filepath.Join(o.Out, mailconv.SanitizeSegment(s.Label))))
	}
	lines = append(lines,
		fmt.Sprintf("readpst: %s", utils.Or(o.ReadPST, "PATH lookup")),
		fmt.Sprintf("scratch: %s", o.Staging),
		fmt.Sprintf("state: %s", o.StatePath),
	)
	return lines
}

// ImportPST runs one PST import: source.Sync (extract via readpst, sha256
// seen-set dedup, atomic checkpoint) and then the conversion of the extracted
// corpus root through the shared mailconv.FromEML path (folder = PST folder,
// e.g. "Posteingang"). Re-runs convert nothing twice.
func ImportPST(ctx context.Context, o ImportOptions) (Stats, ConvStats, error) {
	st, err := Sync(ctx, &PST{
		Sources: o.Sources,
		Staging: o.Staging,
		Out:     o.Out,
		ReadPST: o.ReadPST,
	}, func(context.Context, Blob) error { return nil }, Options{StatePath: o.StatePath})
	if err != nil {
		return st, ConvStats{}, err
	}
	ok, skip, fail, err := mailconv.FromEML(o.Out, false, false, false)
	if err != nil {
		return st, ConvStats{}, err
	}
	return st, ConvStats{OK: ok, Skip: skip, Fail: fail}, nil
}
