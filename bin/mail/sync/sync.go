package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// RetryPolicy is the exponential-backoff strategy applied to transient HTTP
// failures (5xx, timeouts, network errors). Callers wrap transient errors with
// retryWrap; everything else aborts immediately.
type RetryPolicy struct {
	MaxAttempts int           // total attempts (>=1); 0 => 5
	BaseDelay   time.Duration // first backoff; 0 => 250ms
	MaxDelay    time.Duration // cap; 0 => 15s
	Jitter      float64       // 0..1 multiplier; 0 => 0.2
}

func (p RetryPolicy) withDefaults() RetryPolicy {
	if p.MaxAttempts <= 0 {
		p.MaxAttempts = 5
	}
	if p.BaseDelay <= 0 {
		p.BaseDelay = 250 * time.Millisecond
	}
	if p.MaxDelay <= 0 {
		p.MaxDelay = 15 * time.Second
	}
	if p.Jitter <= 0 {
		p.Jitter = 0.2
	}
	return p
}

// delay returns the wait before attempt n (1-based): base * 2^(n-2) + jitter,
// capped at MaxDelay. Attempt 1 waits 0, attempt 2 waits base, then doubles.
func (p RetryPolicy) delay(attempt int) time.Duration {
	if attempt <= 1 {
		return 0
	}
	exp := math.Min(float64(attempt-2), 10)
	d := float64(p.BaseDelay) * math.Pow(2, exp)
	if p.Jitter > 0 {
		d *= 1 - p.Jitter + 2*p.Jitter*rand.Float64()
	}
	if d > float64(p.MaxDelay) {
		d = float64(p.MaxDelay)
	}
	return time.Duration(d)
}

type errRetry struct{ err error }

func (e *errRetry) Error() string { return e.err.Error() }
func (e *errRetry) Unwrap() error { return e.err }

func isRetriable(err error) bool {
	var r *errRetry
	return errors.As(err, &r)
}

func retryWrap(err error) error {
	if err == nil {
		return nil
	}
	if isRetriable(err) {
		return err
	}
	return &errRetry{err: err}
}

// Retry runs fn up to MaxAttempts times with exponential backoff between
// attempts. Non-retriable errors abort immediately. Returns the last error.
func Retry(ctx context.Context, policy RetryPolicy, fn func() error) error {
	policy = policy.withDefaults()
	var err error
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if err = fn(); err == nil {
			return nil
		}
		if !isRetriable(err) {
			return err
		}
		if attempt == policy.MaxAttempts {
			return fmt.Errorf("after %d attempts: %w", policy.MaxAttempts, err)
		}
		select {
		case <-time.After(policy.delay(attempt)):
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return err
}

// SyncConfig wires up a sync run.
type SyncConfig struct {
	OO      *OOConfig         // OnlyOffice source (optional)
	Gmail   *GmailCredentials // Gmail source (optional)
	M365    *M365Credentials  // Microsoft 365 Graph source (optional)
	Out     string            // var/mail root; default <repo>/var/mail
	Workers int               // concurrency; default 4
	Limit   int               // max messages per source (0 = all)
	Offset  int               // skip first N messages per source
	Force   bool              // overwrite existing message.json + attachments
	DryRun  bool              // list without writing
	Query   string            // Gmail search query; default in:inbox
	Policy  RetryPolicy
}

// SyncStats is returned by Run.
type SyncStats struct {
	Checked int
	New     int32
	Failed  int32
	Skipped int32
}

// Source abstracts the two backends for the worker pool.
type Source interface {
	// ListIDs yields ids (string form) to fetch. cursor resumes pagination.
	ListIDs(ctx context.Context, limit int, cursor string) (ids []string, next string, err error)
	Get(ctx context.Context, id string) (*Message, error)
	DownloadAttachment(ctx context.Context, msg *Message, att Attachment) ([]byte, error)
	Folder() string
}

// Committer is an optional Source capability: Commit is called after all listed
// ids have been downloaded successfully. Sources that only advance durable state
// on success (e.g. a Graph delta link) implement this so a killed or failed run
// stays retryable without gaps.
type Committer interface {
	Commit() error
}

type ooSource struct {
	c    ooMailAPI
	page int
}

type ooMailAPI interface {
	ListIDs(ctx context.Context, maxIDs int, page int) (ids []int, next int, err error)
	GetMessage(ctx context.Context, id int) (*Message, error)
	DownloadAttachment(ctx context.Context, fileID string) ([]byte, error)
}

// gmailAPI is the Gmail client surface gmailSource needs. *GmailClient implements it.
type gmailAPI interface {
	ListIDs(ctx context.Context, q string, maxIDs int, pageToken string) ([]string, string, error)
	GetMessage(ctx context.Context, id string) (*Message, error)
	DownloadAttachment(ctx context.Context, msgID, attID string) ([]byte, error)
}

type gmailSource struct {
	c     gmailAPI
	cur   string
	query string
}

func (s *ooSource) Folder() string    { return "inbox" }
func (s *gmailSource) Folder() string { return "gmail" }

func (s *ooSource) ListIDs(ctx context.Context, limit int, cursor string) ([]string, string, error) {
	page := s.page
	if page == 0 {
		page = 1
	}
	ids, next, err := s.c.ListIDs(ctx, limit, page)
	s.page = next
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = fmt.Sprintf("%d", id)
	}
	return strs, "", err
}

func (s *ooSource) Get(ctx context.Context, id string) (*Message, error) {
	var mid int
	if _, err := fmt.Sscanf(id, "%d", &mid); err != nil {
		return nil, fmt.Errorf("oo id %q: %w", id, err)
	}
	return s.c.GetMessage(ctx, mid)
}

func (s *ooSource) DownloadAttachment(ctx context.Context, msg *Message, att Attachment) ([]byte, error) {
	return s.c.DownloadAttachment(ctx, att.FileID)
}

func (s *gmailSource) ListIDs(ctx context.Context, limit int, cursor string) ([]string, string, error) {
	q := s.query
	if q == "" {
		q = "in:inbox"
	}
	ids, next, err := s.c.ListIDs(ctx, q, limit, cursor)
	return ids, next, err
}

func (s *gmailSource) Get(ctx context.Context, id string) (*Message, error) {
	return s.c.GetMessage(ctx, id)
}

func (s *gmailSource) DownloadAttachment(ctx context.Context, msg *Message, att Attachment) ([]byte, error) {
	return s.c.DownloadAttachment(ctx, msg.ID, att.FileID)
}

// Run executes the sync across the configured sources with a worker pool.
func Run(ctx context.Context, cfg SyncConfig) (*SyncStats, error) {
	if cfg.Out == "" {
		cfg.Out = "var/mail"
	}
	if cfg.Workers <= 0 {
		cfg.Workers = 4
	}
	if err := os.MkdirAll(cfg.Out, 0o755); err != nil {
		return nil, err
	}
	var sources []Source
	if cfg.OO != nil {
		oo, err := newOOAPI(*cfg.OO, 1) // folder inbox
		if err != nil {
			return nil, fmt.Errorf("onlyoffice auth: %w", err)
		}
		sources = append(sources, &ooSource{c: oo})
	}
	if cfg.Gmail != nil {
		gm, err := NewGmailClient(*cfg.Gmail)
		if err != nil {
			return nil, fmt.Errorf("gmail init: %w", err)
		}
		sources = append(sources, &gmailSource{c: gm, query: cfg.Query})
	}
	if cfg.M365 != nil {
		stateDir := filepath.Join(cfg.Out, ".m365")
		for _, mb := range cfg.M365.Users {
			if !strings.Contains(mb, "@") {
				return nil, fmt.Errorf("m365 user %q is not an email address", mb)
			}
			c, err := NewM365Client(*cfg.M365)
			if err != nil {
				return nil, fmt.Errorf("m365 init for %s: %w", mb, err)
			}
			local := strings.SplitN(mb, "@", 2)[0]
			sources = append(sources, &m365Source{c: c, mailbox: mb, localpart: strings.ToLower(local), stateDir: stateDir, exclude: cfg.M365.excludeSet()})
		}
	}
	if len(sources) == 0 {
		return nil, errors.New("sync: no source configured (need OO, Gmail, M365, or a combination)")
	}

	stats := &SyncStats{}
	var jobs []struct {
		src Source
		id  string
	}
	for _, src := range sources {
		ids, _, err := src.ListIDs(ctx, cfg.Offset+cfg.Limit, "")
		if err != nil {
			return nil, fmt.Errorf("list %s: %w", src.Folder(), err)
		}
		if cfg.Offset > 0 {
			if cfg.Offset >= len(ids) {
				ids = nil
			} else {
				ids = ids[cfg.Offset:]
			}
		}
		if cfg.Limit > 0 && len(ids) > cfg.Limit {
			ids = ids[:cfg.Limit]
		}
		stats.Checked += len(ids)
		for _, id := range ids {
			jobs = append(jobs, struct {
				src Source
				id  string
			}{src: src, id: id})
		}
	}

	var (
		wg       sync.WaitGroup
		mu       sync.Mutex
		failures []string
	)
	jobsCh := make(chan struct {
		src Source
		id  string
	})
	for i := 0; i < cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobsCh {
				status, err := processOne(ctx, j.src, j.id, cfg)
				switch status {
				case statusFailed:
					mu.Lock()
					failures = append(failures, j.src.Folder()+"/"+j.id+": "+err.Error())
					mu.Unlock()
					atomic.AddInt32(&stats.Failed, 1)
				case statusNew:
					atomic.AddInt32(&stats.New, 1)
				case statusSkipped:
					atomic.AddInt32(&stats.Skipped, 1)
				}
			}
		}()
	}
	for _, j := range jobs {
		select {
		case jobsCh <- j:
		case <-ctx.Done():
			close(jobsCh)
			wg.Wait()
			return stats, ctx.Err()
		}
	}
	close(jobsCh)
	wg.Wait()

	// Only advance durable source state (e.g. delta links) when everything
	// downloaded. A killed or failed run must be retryable without gaps.
	if len(failures) == 0 {
		seen := map[Source]bool{}
		for _, j := range jobs {
			if seen[j.src] {
				continue
			}
			seen[j.src] = true
			if c, ok := j.src.(Committer); ok {
				if err := c.Commit(); err != nil {
					mu.Lock()
					failures = append(failures, j.src.Folder()+"/commit: "+err.Error())
					mu.Unlock()
				}
			}
		}
	}

	if len(failures) > 0 {
		fmt.Fprintf(os.Stderr, "sync: %d failures:\n  %s\n", len(failures), strings.Join(failures, "\n  "))
	}
	return stats, nil
}

type status int

const (
	statusNew status = iota
	statusSkipped
	statusFailed
)

func processOne(ctx context.Context, src Source, id string, cfg SyncConfig) (status, error) {
	if cfg.DryRun {
		return statusNew, nil
	}
	dir := filepath.Join(cfg.Out, src.Folder(), id)
	jsonPath := filepath.Join(dir, "message.json")
	if !cfg.Force {
		if _, err := os.Stat(jsonPath); err == nil {
			return statusSkipped, nil
		}
	}
	var msg *Message
	err := Retry(ctx, cfg.Policy, func() error {
		m, err := src.Get(ctx, id)
		if err != nil {
			return retryWrap(err)
		}
		m.Folder = src.Folder() // directory layout is authoritative
		if err := writeMessage(jsonPath, m); err != nil {
			return err
		}
		msg = m
		return nil
	})
	if err != nil {
		return statusFailed, err
	}
	for _, att := range msg.Attachments {
		attDir := filepath.Join(dir, "attachments")
		if err := os.MkdirAll(attDir, 0o755); err != nil {
			return statusFailed, err
		}
		attPath := filepath.Join(attDir, sanitize(att.StoredName))
		if _, err := os.Stat(attPath); err == nil && !cfg.Force {
			continue
		}
		var data []byte
		err := Retry(ctx, cfg.Policy, func() error {
			b, err := src.DownloadAttachment(ctx, msg, att)
			if err != nil {
				return retryWrap(err)
			}
			data = b
			return os.WriteFile(attPath, b, 0o644)
		})
		if err != nil {
			return statusFailed, fmt.Errorf("attachment %s: %w", att.FileName, err)
		}
		// ICS attachments get structured markdown immediately (same name the
		// Go converter uses: <display stem>.md).
		if isICS(att.FileName) {
			stem := att.FileName
			if i := strings.LastIndex(stem, "."); i >= 0 {
				stem = stem[:i]
			}
			mdPath := filepath.Join(attDir, sanitize(stem)+".md")
			if err := os.WriteFile(mdPath, []byte(ICSToMarkdown(data)), 0o644); err != nil {
				return statusFailed, err
			}
		}
	}
	return statusNew, nil
}

func writeMessage(path string, m *Message) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

func sanitize(name string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_", "\"", "_",
		"<", "_", ">", "_", "|", "_", " ", "_")
	return r.Replace(name)
}

func isICS(name string) bool {
	n := strings.ToLower(name)
	return strings.HasSuffix(n, ".ics") || strings.HasSuffix(n, ".ical")
}
