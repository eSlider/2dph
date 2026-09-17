package mailsync

import (
	"context"
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
	"unicode/utf8"
)

func TestRetrySucceedsOnSecondTry(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), RetryPolicy{BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}, func() error {
		attempts++
		if attempts == 1 {
			return retryWrap(errors.New("boom"))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("expected 2 attempts, got %d", attempts)
	}
}

func TestRetryExhaustsAttempts(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond}, func() error {
		attempts++
		return retryWrap(errors.New("nope"))
	})
	if err == nil {
		t.Fatal("expected error after exhaustion")
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
}

func TestRetryNonRetriableAbortsImmediately(t *testing.T) {
	attempts := 0
	err := Retry(context.Background(), RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond}, func() error {
		attempts++
		return errors.New("permanent")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if attempts != 1 {
		t.Fatalf("expected 1 attempt for non-retriable, got %d", attempts)
	}
}

func TestRetryRespectsContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := Retry(ctx, RetryPolicy{MaxAttempts: 5, BaseDelay: time.Millisecond}, func() error {
		return retryWrap(errors.New("x"))
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestDelayGrows(t *testing.T) {
	p := RetryPolicy{BaseDelay: time.Second, MaxDelay: 30 * time.Second, Jitter: 0}
	d1 := p.delay(1) // attempt 1 => 0
	d2 := p.delay(2)
	d3 := p.delay(3)
	if d1 != 0 {
		t.Fatalf("attempt 1 delay should be 0, got %v", d1)
	}
	if d2 != time.Second {
		t.Fatalf("attempt 2 delay should be 1s, got %v", d2)
	}
	if d3 != 2*time.Second {
		t.Fatalf("attempt 3 delay should be 2s, got %v", d3)
	}
}

func TestSanitize(t *testing.T) {
	cases := map[string]string{
		"a/b\\c:d*e": "a_b_c_d_e",
		"normal.txt": "normal.txt",
		"../evil":    ".._evil",
		"a b c.pdf":  "a_b_c.pdf",
	}
	for in, want := range cases {
		if got := sanitize(in); got != want {
			t.Errorf("sanitize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsICS(t *testing.T) {
	if !isICS("reply.ics") || !isICS("x.ICAL") {
		t.Fatal("ics extensions not detected")
	}
	if isICS("invoice.pdf") {
		t.Fatal("pdf misdetected as ics")
	}
}

const fixtureReplyICS = `BEGIN:VCALENDAR
METHOD:REPLY
PRODID:Microsoft Exchange Server 2010
VERSION:2.0
BEGIN:VTIMEZONE
TZID:W. Europe Standard Time
BEGIN:STANDARD
DTSTART:16010101T030000
TZOFFSETFROM:+0200
TZOFFSETTO:+0100
RRULE:FREQ=YEARLY;INTERVAL=1;BYDAY=-1SU;BYMONTH=10
END:STANDARD
BEGIN:DAYLIGHT
DTSTART:16010101T020000
TZOFFSETFROM:+0100
TZOFFSETTO:+0200
RRULE:FREQ=YEARLY;INTERVAL=1;BYDAY=-1SU;BYMONTH=3
END:DAYLIGHT
END:VTIMEZONE
BEGIN:VEVENT
ATTENDEE;PARTSTAT=ACCEPTED;CN="Baker, Ben":mailto:bbaker1@teksystems.com
UID:bvlnr1i35ug30kn6rvu9dop00g@google.com
SUMMARY;LANGUAGE=en-US:Accepted: Appointment (Ben  Baker)
DTSTART;TZID=W. Europe Standard Time:20260812T120000
DTEND;TZID=W. Europe Standard Time:20260812T123000
CLASS:PUBLIC
STATUS:CONFIRMED
LOCATION;LANGUAGE=en-US:https://meet.google.com/sxh-ubud-jrd
END:VEVENT
END:VCALENDAR`

func TestICSToMarkdown(t *testing.T) {
	out := ICSToMarkdown([]byte(fixtureReplyICS))
	for _, want := range []string{
		"Accepted: Appointment",
		"When:",
		"Where:",
		"meet.google.com",
		"Attendee:",
		"Baker, Ben",
		"Accepted",
		"Calendar method: REPLY",
	} {
		if !contains(out, want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	if contains(out, "BEGIN:VCALENDAR") {
		t.Errorf("raw ICS leaked into markdown:\n%s", out)
	}
}

func TestICSToMarkdownFallback(t *testing.T) {
	out := ICSToMarkdown([]byte("not a calendar"))
	if !contains(out, "not a calendar") {
		t.Fatalf("expected raw fallback, got %q", out)
	}
}

func TestICSToMarkdownNormalizesLatin1(t *testing.T) {
	// Real-world ICS from a rental portal: summary in UTF-8, location Latin-1
	// ("N\xfcrnberg"). The markdown output must be valid UTF-8 everywhere.
	raw := "BEGIN:VCALENDAR\r\nVERSION:2.0\r\nBEGIN:VEVENT\r\n" +
		"SUMMARY:Mietwagen-Buchung: N\xc3\xbcrnberg\r\n" +
		"LOCATION:N\xfcrnberg\r\nDTSTART:20200101T090000Z\r\nDTEND:20200101T180000Z\r\n" +
		"END:VEVENT\r\nEND:VCALENDAR\r\n"
	out := ICSToMarkdown([]byte(raw))
	if !utf8.ValidString(out) {
		t.Fatalf("output is not valid UTF-8:\n%q", out)
	}
	if !contains(out, "Nürnberg") {
		t.Errorf("expected Nürnberg in output:\n%s", out)
	}
	if contains(out, "N\xfcrnberg") {
		t.Errorf("Latin-1 bytes leaked into output:\n%q", out)
	}
}

func TestICSToMarkdownAllDay(t *testing.T) {
	ics := `BEGIN:VCALENDAR
VERSION:2.0
BEGIN:VEVENT
UID:y@google.com
SUMMARY:All day thing
DTSTART;VALUE=DATE:20260815
DTEND;VALUE=DATE:20260816
END:VEVENT
END:VCALENDAR`
	out := ICSToMarkdown([]byte(ics))
	if !contains(out, "All day thing") || !contains(out, "2026-08-15") {
		t.Errorf("all-day event not parsed:\n%s", out)
	}
}

func TestCollectParts(t *testing.T) {
	p := gmailPart{
		MimeType: "multipart/mixed",
		Parts: []gmailPart{
			{MimeType: "multipart/alternative", Parts: []gmailPart{
				{MimeType: "text/plain", Body: gmailBody{Data: b64("plain text")}},
				{MimeType: "text/html", Body: gmailBody{Data: b64("<p>html</p>")}},
			}},
			{PartID: "2", MimeType: "application/pdf", Filename: "invoice.pdf", Body: gmailBody{Size: 100}},
		},
	}
	text, html, atts := collectParts(p, "root", "abc123", 0)
	if text != "plain text" {
		t.Errorf("text = %q", text)
	}
	if html != "<p>html</p>" {
		t.Errorf("html = %q", html)
	}
	if len(atts) != 1 || atts[0].FileName != "invoice.pdf" || atts[0].FileID != "abc123:2" {
		t.Errorf("atts = %+v", atts)
	}
}

type fakeGmailAPI struct {
	lastQ     string
	lastLimit int
	ids       []string
}

func (f *fakeGmailAPI) ListIDs(_ context.Context, q string, maxIDs int, _ string) ([]string, string, error) {
	f.lastQ = q
	f.lastLimit = maxIDs
	return f.ids, "", nil
}
func (f *fakeGmailAPI) GetMessage(context.Context, string) (*Message, error) {
	return nil, errors.New("unused")
}
func (f *fakeGmailAPI) GetMessageRaw(context.Context, string) ([]byte, error) {
	return nil, errors.New("unused")
}
func (f *fakeGmailAPI) DownloadAttachment(context.Context, string, string) ([]byte, error) {
	return nil, errors.New("unused")
}

func TestGmailSourcePassesQueryToListIDs(t *testing.T) {
	fake := &fakeGmailAPI{ids: []string{"m1"}}
	src := &gmailSource{c: fake, query: "from:alice@example.com"}
	ids, _, err := src.ListIDs(context.Background(), 10, "")
	if err != nil {
		t.Fatal(err)
	}
	if fake.lastQ != "from:alice@example.com" {
		t.Fatalf("ListIDs q=%q, want from:alice@example.com", fake.lastQ)
	}
	if fake.lastLimit != 10 {
		t.Fatalf("ListIDs limit=%d, want 10", fake.lastLimit)
	}
	if len(ids) != 1 || ids[0] != "m1" {
		t.Fatalf("ids=%v", ids)
	}
}

func TestGmailSourceEmptyQueryDefaultsToInbox(t *testing.T) {
	fake := &fakeGmailAPI{}
	src := &gmailSource{c: fake, query: ""}
	if _, _, err := src.ListIDs(context.Background(), 5, ""); err != nil {
		t.Fatal(err)
	}
	if fake.lastQ != "in:inbox" {
		t.Fatalf("empty query q=%q, want in:inbox", fake.lastQ)
	}
}

func TestParseCLIGmailQuery(t *testing.T) {
	cli, code, err := ParseCLI([]string{
		"--source", "gmail",
		"--query", "from:letrado@example.com",
		"--out", t.TempDir(),
		"--dry-run",
	})
	if err != nil || code != 0 {
		t.Fatalf("ParseCLI: code=%d err=%v", code, err)
	}
	if cli.Sync.Query != "from:letrado@example.com" {
		t.Fatalf("query=%q", cli.Sync.Query)
	}
	if cli.Sync.Gmail == nil {
		t.Fatal("gmail source not configured")
	}
}

func TestParseCLIThrottleAndMaxConcurrency(t *testing.T) {
	t.Setenv("M365_TENANT", "t")
	t.Setenv("M365_CLIENT_ID", "c")
	t.Setenv("M365_CLIENT_SECRET", "s")
	t.Setenv("M365_USERS", "r@x.de")
	cli, code, err := ParseCLI([]string{
		"--source", "m365", "--out", t.TempDir(),
		"--throttle", "250ms", "--max-concurrency", "3",
	})
	if err != nil || code != 0 {
		t.Fatalf("ParseCLI: code=%d err=%v", code, err)
	}
	if cli.Sync.Throttle != 250*time.Millisecond {
		t.Fatalf("Throttle = %v, want 250ms", cli.Sync.Throttle)
	}
	if cli.Sync.MaxConcurrency != 3 {
		t.Fatalf("MaxConcurrency = %d, want 3", cli.Sync.MaxConcurrency)
	}
}

func TestParseCLIInvalidThrottle(t *testing.T) {
	_, code, err := ParseCLI([]string{
		"--source", "m365", "--throttle", "soon",
		"--env", filepath.Join(t.TempDir(), "empty.env"),
	})
	if err == nil {
		t.Fatal("expected an error for a bad -throttle")
	}
	if code != 2 {
		t.Fatalf("code = %d, want 2", code)
	}
}

func b64(s string) string {
	return base64.URLEncoding.EncodeToString([]byte(s))
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// commitSource is a fake Source with one id per entry and a recorded Commit,
// used to prove per-source commit isolation (#321).
type commitSource struct {
	folder    string
	ids       []string
	commitErr error
	committed bool
}

func (s *commitSource) ListIDs(context.Context, int, string) ([]string, string, error) {
	return s.ids, "", nil
}
func (s *commitSource) Get(_ context.Context, id string) (*Message, error) {
	return &Message{ID: id}, nil
}
func (s *commitSource) DownloadAttachment(context.Context, *Message, Attachment) ([]byte, error) {
	return nil, errors.New("unused")
}
func (s *commitSource) Folder() string { return s.folder }
func (s *commitSource) Commit() error {
	if s.commitErr != nil {
		return s.commitErr
	}
	s.committed = true
	return nil
}

// listFailSource always fails to list, mimicking an Exchange-throttled mailbox.
type listFailSource struct {
	folder    string
	committed bool
}

func (s *listFailSource) ListIDs(context.Context, int, string) ([]string, string, error) {
	return nil, "", errors.New("503 CommandConcurrencyLimitReached")
}
func (s *listFailSource) Get(context.Context, string) (*Message, error) {
	return nil, errors.New("unused")
}
func (s *listFailSource) DownloadAttachment(context.Context, *Message, Attachment) ([]byte, error) {
	return nil, errors.New("unused")
}
func (s *listFailSource) Folder() string { return s.folder }
func (s *listFailSource) Commit() error  { s.committed = true; return nil }

// TestRunSkipsSourceThatFailsToList proves one throttled source does not abort
// the whole run: the other source syncs and commits, the failing one is skipped
// and not committed, and Run returns no fatal error (#321).
func TestRunSkipsSourceThatFailsToList(t *testing.T) {
	out := t.TempDir()
	good := &commitSource{folder: "good", ids: []string{"a", "b"}}
	bad := &listFailSource{folder: "bad"}
	stats, err := Run(context.Background(), SyncConfig{
		Out:     out,
		Workers: 2,
		Force:   true,
		Policy:  RetryPolicy{MaxAttempts: 1},
		sources: []Source{good, bad},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.New != 2 {
		t.Fatalf("New = %d, want 2", stats.New)
	}
	if !good.committed {
		t.Fatalf("good source must commit")
	}
	if bad.committed {
		t.Fatalf("throttled source must not commit")
	}
}

// TestRunSkipsSourceWhenDownloadFails proves a download failure also isolates
// the commit and does not abort the other source (#321).
func TestRunSkipsSourceWhenDownloadFails(t *testing.T) {
	out := t.TempDir()
	good := &commitSource{folder: "good", ids: []string{"a"}}
	bad := &commitSource{folder: "bad", ids: []string{"x"}, commitErr: nil}
	badGet := &failingGetSource{commitSource: bad}
	stats, err := Run(context.Background(), SyncConfig{
		Out:     out,
		Workers: 2,
		Force:   true,
		Policy:  RetryPolicy{MaxAttempts: 1},
		sources: []Source{good, badGet},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.New != 1 || stats.Failed != 1 {
		t.Fatalf("New=%d Failed=%d, want 1/1", stats.New, stats.Failed)
	}
	if !good.committed {
		t.Fatalf("good source must commit")
	}
	if bad.committed {
		t.Fatalf("source with a failed download must not commit")
	}
}

// failingGetSource lists fine but fails on Get.
type failingGetSource struct{ *commitSource }

func (s *failingGetSource) Get(context.Context, string) (*Message, error) {
	return nil, errors.New("503 transient")
}

// TestRetryHonorsRetryAfter proves a server-provided Retry-After overrides the
// exponential backoff (#321).
func TestRetryHonorsRetryAfter(t *testing.T) {
	attempts := 0
	start := time.Now()
	err := Retry(context.Background(), RetryPolicy{
		MaxAttempts: 3, BaseDelay: time.Nanosecond, MaxDelay: time.Nanosecond, Jitter: 0.001,
	}, func() error {
		attempts++
		if attempts == 1 {
			return &errRetry{err: errors.New("throttle"), after: 150 * time.Millisecond}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("Retry: %v", err)
	}
	if attempts != 2 {
		t.Fatalf("attempts = %d, want 2", attempts)
	}
	if d := time.Since(start); d < 120*time.Millisecond {
		t.Fatalf("Retry-After not honored: retried after %v", d)
	}
}

// retryListSource throttles its ListIDs a bounded number of times, then
// succeeds. It mimics an Exchange mailbox returning
// CommandConcurrencyLimitReached with a Retry-After (#321).
type retryListSource struct {
	folder    string
	ids       []string
	failFirst int
	mu        sync.Mutex
	listCalls int
	committed bool
}

func (s *retryListSource) ListIDs(context.Context, int, string) ([]string, string, error) {
	s.mu.Lock()
	s.listCalls++
	n := s.listCalls
	s.mu.Unlock()
	if n <= s.failFirst {
		return nil, "", &errRetry{err: errors.New("503 CommandConcurrencyLimitReached"), after: time.Millisecond}
	}
	return s.ids, "", nil
}
func (s *retryListSource) Get(_ context.Context, id string) (*Message, error) {
	return &Message{ID: id}, nil
}
func (s *retryListSource) DownloadAttachment(context.Context, *Message, Attachment) ([]byte, error) {
	return nil, errors.New("unused")
}
func (s *retryListSource) Folder() string { return s.folder }
func (s *retryListSource) Commit() error  { s.committed = true; return nil }

func testPolicy() RetryPolicy {
	return RetryPolicy{MaxAttempts: 3, BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond, Jitter: 0.01}
}

// TestRunRetriesThrottledMailboxThenSyncs proves a throttled listing is
// retried and, once the throttle clears, the mailbox syncs and commits (#321).
func TestRunRetriesThrottledMailboxThenSyncs(t *testing.T) {
	out := t.TempDir()
	src := &retryListSource{folder: "m365/r", ids: []string{"m1", "m2"}, failFirst: 1}
	stats, err := Run(context.Background(), SyncConfig{
		Out: out, Workers: 4, Force: true, Policy: testPolicy(),
		sources: []Source{src},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.New != 2 {
		t.Fatalf("New = %d, want 2", stats.New)
	}
	if src.listCalls != 2 {
		t.Fatalf("listCalls = %d, want 2 (one throttle, one success)", src.listCalls)
	}
	if !src.committed {
		t.Fatal("mailbox must commit after a successful retry")
	}
}

// TestRunSkipsMailboxAfterBoundedThrottleRetries proves a mailbox that keeps
// throttling is retried a bounded number of times, then skipped without
// aborting the run; the other source still syncs (#321).
func TestRunSkipsMailboxAfterBoundedThrottleRetries(t *testing.T) {
	out := t.TempDir()
	good := &commitSource{folder: "good", ids: []string{"a", "b"}}
	bad := &retryListSource{folder: "m365/r", failFirst: 1000}
	stats, err := Run(context.Background(), SyncConfig{
		Out: out, Workers: 2, Force: true, Policy: testPolicy(),
		sources: []Source{good, bad},
	})
	if err != nil {
		t.Fatalf("Run must stay non-fatal: %v", err)
	}
	if stats.New != 2 {
		t.Fatalf("New = %d, want 2 from the healthy source", stats.New)
	}
	if stats.Failed != 1 {
		t.Fatalf("Failed = %d, want 1 (skipped mailbox)", stats.Failed)
	}
	if bad.listCalls != testPolicy().MaxAttempts {
		t.Fatalf("listCalls = %d, want bounded %d", bad.listCalls, testPolicy().MaxAttempts)
	}
	if bad.committed {
		t.Fatal("throttled mailbox must not commit")
	}
	if !good.committed {
		t.Fatal("healthy source must commit")
	}
}

// cancelSource cancels the run context on its first Get.
type cancelSource struct {
	folder    string
	ids       []string
	cancel    context.CancelFunc
	mu        sync.Mutex
	gets      int
	committed bool
}

func (s *cancelSource) ListIDs(context.Context, int, string) ([]string, string, error) {
	return s.ids, "", nil
}
func (s *cancelSource) Get(context.Context, string) (*Message, error) {
	s.mu.Lock()
	s.gets++
	n := s.gets
	s.mu.Unlock()
	if n == 1 {
		s.cancel()
	}
	return &Message{ID: "x"}, nil
}
func (s *cancelSource) DownloadAttachment(context.Context, *Message, Attachment) ([]byte, error) {
	return nil, errors.New("unused")
}
func (s *cancelSource) Folder() string { return s.folder }
func (s *cancelSource) Commit() error  { s.committed = true; return nil }

// TestRunCancelDoesNotCommit proves a cancelled run never advances a source's
// durable delta state, so a later run stays gap-free (#321).
func TestRunCancelDoesNotCommit(t *testing.T) {
	out := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	src := &cancelSource{folder: "m365/r", ids: []string{"1", "2", "3"}, cancel: cancel}
	_, err := Run(ctx, SyncConfig{
		Out: out, Workers: 1, Force: true, Policy: testPolicy(),
		sources: []Source{src},
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run err = %v, want context.Canceled", err)
	}
	if src.committed {
		t.Fatal("cancelled run must not commit the source")
	}
}

// TestRunRetryNoDuplicateWrites proves a retried message is written once and a
// repeated run skips it instead of duplicating (#321).
func TestRunRetryNoDuplicateWrites(t *testing.T) {
	out := t.TempDir()
	src := &flakyGetSource{folder: "m365/r", id: "m1"}
	cfg := SyncConfig{Out: out, Workers: 2, Policy: testPolicy(), sources: []Source{src}}
	stats, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if stats.New != 1 {
		t.Fatalf("New = %d, want 1", stats.New)
	}
	if n := countMessageJSON(t, out); n != 1 {
		t.Fatalf("message.json count = %d, want 1", n)
	}
	// Second run: the message is already on disk, so it is skipped, not
	// re-fetched or duplicated.
	stats2, err := Run(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Run 2: %v", err)
	}
	if stats2.New != 0 || stats2.Skipped != 1 {
		t.Fatalf("second run New=%d Skipped=%d, want 0/1", stats2.New, stats2.Skipped)
	}
	if n := countMessageJSON(t, out); n != 1 {
		t.Fatalf("message.json count after rerun = %d, want 1", n)
	}
}

// flakyGetSource throttles the first Get, then succeeds with one attachment.
type flakyGetSource struct {
	folder string
	id     string
	mu     sync.Mutex
	gets   int
}

func (s *flakyGetSource) ListIDs(context.Context, int, string) ([]string, string, error) {
	return []string{s.id}, "", nil
}
func (s *flakyGetSource) Get(context.Context, string) (*Message, error) {
	s.mu.Lock()
	s.gets++
	n := s.gets
	s.mu.Unlock()
	if n == 1 {
		return nil, &errRetry{err: errors.New("503 throttled"), after: time.Millisecond}
	}
	return &Message{ID: s.id, Attachments: []Attachment{{FileID: "a1", FileName: "x.pdf", StoredName: "x.pdf"}}}, nil
}
func (s *flakyGetSource) DownloadAttachment(context.Context, *Message, Attachment) ([]byte, error) {
	return []byte("data"), nil
}
func (s *flakyGetSource) Folder() string { return s.folder }

func countMessageJSON(t *testing.T, root string) int {
	t.Helper()
	var n int
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && info.Name() == "message.json" {
			n++
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk: %v", err)
	}
	return n
}

// seqSource records the maximum number of concurrent Get calls. When
// sequential is set, Run must never let more than one Get overlap.
type seqSource struct {
	folder     string
	ids        []string
	sequential bool
	mu         sync.Mutex
	inFlight   int
	maxSeen    int
	committed  bool
}

func (s *seqSource) ListIDs(context.Context, int, string) ([]string, string, error) {
	return s.ids, "", nil
}
func (s *seqSource) Get(context.Context, string) (*Message, error) {
	s.mu.Lock()
	s.inFlight++
	if s.inFlight > s.maxSeen {
		s.maxSeen = s.inFlight
	}
	s.mu.Unlock()
	time.Sleep(5 * time.Millisecond)
	s.mu.Lock()
	s.inFlight--
	s.mu.Unlock()
	return &Message{ID: "x"}, nil
}
func (s *seqSource) DownloadAttachment(context.Context, *Message, Attachment) ([]byte, error) {
	return nil, errors.New("unused")
}
func (s *seqSource) Folder() string   { return s.folder }
func (s *seqSource) Sequential() bool { return s.sequential }
func (s *seqSource) Commit() error    { s.committed = true; return nil }

// TestRunSequentialSourceKeepsOneInFlight proves a Sequencer source is fetched
// one message at a time even when Workers and MaxConcurrency are high (#321).
func TestRunSequentialSourceKeepsOneInFlight(t *testing.T) {
	out := t.TempDir()
	src := &seqSource{folder: "m365/r", sequential: true, ids: []string{"1", "2", "3", "4", "5"}}
	if _, err := Run(context.Background(), SyncConfig{
		Out: out, Workers: 8, MaxConcurrency: 4, Force: true, Policy: testPolicy(),
		sources: []Source{src},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if src.maxSeen != 1 {
		t.Fatalf("max concurrent Get = %d, want 1 for a sequential source", src.maxSeen)
	}
}

// TestRunParallelSourceUsesWorkers is the control: a non-sequential source may
// run its messages concurrently, so the sequential test is not trivially true.
func TestRunParallelSourceUsesWorkers(t *testing.T) {
	out := t.TempDir()
	src := &seqSource{folder: "m365/r", ids: []string{"1", "2", "3", "4", "5", "6", "7", "8"}}
	src.sequential = false
	if _, err := Run(context.Background(), SyncConfig{
		Out: out, Workers: 4, MaxConcurrency: 1, Force: true, Policy: testPolicy(),
		sources: []Source{src},
	}); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if src.maxSeen < 2 {
		t.Fatalf("max concurrent Get = %d, want >1 for a parallel source", src.maxSeen)
	}
}
