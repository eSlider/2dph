package mailsync

// Generic IMAP source (Alfahosting & friends): read-only UID sync of every
// selectable folder into var/mail/<folder>/<uid>/<uid>.eml for the enmime
// importer (bin/mail/import.go --from-eml). Never sets \Seen (readonly
// select + BODY.PEEK[]).
//
// Env (via --env file or process env):
//
//	IMAP_HOST       e.g. mail.example.com
//	IMAP_PORT       default 993
//	IMAP_USER       mailbox user
//	IMAP_PASSWORD   mailbox password
//	IMAP_FOLDERS    comma list (empty = auto-list all selectable folders)

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/mail"
)

// dialTimeout bounds TCP+TLS setup so a dead host can't stall a cycle.
var dialTimeout = 30 * time.Second

func dialOptions() *imapclient.Options {
	return &imapclient.Options{
		Dialer: &net.Dialer{Timeout: dialTimeout},
	}
}

// IMAPConfig configures one generic IMAP account.
type IMAPConfig struct {
	Host     string
	Port     int
	User     string
	Password string
	Folders  []string // empty = auto-list all selectable folders
}

// imapSource syncs ONE mailbox folder into <out>/<dir>/<uid>/<uid>.eml.
// Each instance holds its own connection selected readonly, so concurrent
// fetches never fight over selected-state across folders.
type imapSource struct {
	cfg     IMAPConfig
	mailbox string // server-side name, e.g. "INBOX.Sent"
	dir     string // sanitized on-disk name under Out

	mu sync.Mutex
	c  *imapclient.Client
}

var _ Source = (*imapSource)(nil)
var _ RawMailer = (*imapSource)(nil)

func (s *imapSource) Folder() string { return s.dir }

// addr returns host:port with the 993 default applied.
func (cfg IMAPConfig) addr() string {
	port := cfg.Port
	if port == 0 {
		port = 993
	}
	return fmt.Sprintf("%s:%d", cfg.Host, port)
}

// dial opens a TLS connection, logs in and selects the mailbox readonly.
func (s *imapSource) dial() error {
	c, err := imapclient.DialTLS(s.cfg.addr(), dialOptions())
	if err != nil {
		return fmt.Errorf("imap dial %s: %w", s.cfg.addr(), err)
	}
	if err := c.Login(s.cfg.User, s.cfg.Password).Wait(); err != nil {
		c.Close()
		return fmt.Errorf("imap login %s: %w", s.cfg.User, err)
	}
	if _, err := c.Select(s.mailbox, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		c.Logout()
		return fmt.Errorf("imap select %s: %w", s.mailbox, err)
	}
	s.c = c
	return nil
}

// client returns a live connection, redialing once if the current one died.
func (s *imapSource) client() (*imapclient.Client, error) {
	if s.c != nil {
		return s.c, nil
	}
	if err := s.dial(); err != nil {
		return nil, err
	}
	return s.c, nil
}

// reconnect drops the cached connection so the next call redials.
func (s *imapSource) reconnect(err error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.c != nil {
		s.c.Close()
		s.c = nil
	}
	return fmt.Errorf("imap %s: %w", s.mailbox, err)
}

// ListIDs yields every UID in the folder (SEARCH ALL, readonly).
func (s *imapSource) ListIDs(ctx context.Context, limit int, cursor string) ([]string, string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	c, err := s.client()
	if err != nil {
		return nil, "", err
	}
	data, err := c.UIDSearch(&imap.SearchCriteria{}, nil).Wait()
	if err != nil {
		if rerr := s.reconnect(err); rerr != nil {
			return nil, "", rerr
		}
		c, _ = s.client()
		data, err = c.UIDSearch(&imap.SearchCriteria{}, nil).Wait()
		if err != nil {
			return nil, "", s.reconnect(err)
		}
	}
	uids, ok := data.All.(imap.UIDSet)
	if !ok {
		return nil, "", fmt.Errorf("imap %s: unexpected search result %T", s.mailbox, data.All)
	}
	nums, ok := uids.Nums()
	if !ok {
		return nil, "", fmt.Errorf("imap %s: UID set too large to enumerate", s.mailbox)
	}
	ids := make([]string, len(nums))
	for i, u := range nums {
		ids[i] = fmt.Sprintf("%d", uint32(u))
	}
	return ids, "", ctx.Err()
}

// GetRaw fetches the full RFC 822 message via UID FETCH BODY.PEEK[].
func (s *imapSource) GetRaw(ctx context.Context, id string) ([]byte, error) {
	var uid imap.UID
	if _, err := fmt.Sscanf(id, "%d", &uid); err != nil {
		return nil, fmt.Errorf("imap uid %q: %w", id, err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	fetch := func(c *imapclient.Client) ([]byte, error) {
		msgs, err := c.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{
			BodySection: []*imap.FetchItemBodySection{{Peek: true}},
		}).Collect()
		if err != nil {
			return nil, err
		}
		if len(msgs) == 0 || len(msgs[0].BodySection) == 0 {
			return nil, fmt.Errorf("imap uid %s: no body returned", id)
		}
		return msgs[0].BodySection[0].Bytes, nil
	}

	c, err := s.client()
	if err != nil {
		return nil, err
	}
	b, err := fetch(c)
	if err != nil {
		_ = s.reconnect(err) // drop conn; retry below gets a fresh one
		c2, cerr := s.client()
		if cerr != nil {
			return nil, cerr
		}
		b, err = fetch(c2)
		if err != nil {
			return nil, fmt.Errorf("imap %s uid %s: %w", s.mailbox, id, err)
		}
	}
	return b, nil
}

// Get builds a normalized Message by parsing the raw RFC 822 with
// emersion/go-message (the code-standard MIME handler; enmime is legacy
// parity only, see mime-emersion #95). Used when the caller runs without
// --raw; the raw .eml path is what we normally drive.
func (s *imapSource) Get(ctx context.Context, id string) (*Message, error) {
	raw, err := s.GetRaw(ctx, id)
	if err != nil {
		return nil, err
	}
	return parseIMAPMessage(s.dir, id, raw)
}

// parseIMAPMessage parses a raw RFC 822 with emersion/go-message (the
// code-standard MIME handler; enmime is legacy parity only, see mime-emersion
// #95). Extracted from Get so the parse is testable offline against fixtures.
func parseIMAPMessage(folder, id string, raw []byte) (*Message, error) {
	r, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("imap %s uid %s: parse: %w", folder, id, err)
	}
	date, _ := r.Header.Date()
	m := &Message{
		Source:        "imap",
		ID:            id,
		Folder:        folder,
		Subject:       r.Header.Get("Subject"),
		From:          r.Header.Get("From"),
		To:            r.Header.Get("To"),
		CC:            r.Header.Get("Cc"),
		BCC:           r.Header.Get("Bcc"),
		ReceivedAt:    date,
		MimeMessageID: r.Header.Get("Message-ID"),
	}
	for {
		part, err := r.NextPart()
		if err != nil {
			break // io.EOF (or an unknown-charset part) ends iteration
		}
		media := strings.ToLower(strings.TrimSpace(strings.SplitN(
			part.Header.Get("Content-Type"), ";", 2)[0]))
		if media != "text/plain" && media != "text/html" {
			continue
		}
		body, err := io.ReadAll(io.LimitReader(part.Body, 4<<20))
		if err != nil {
			continue
		}
		switch media {
		case "text/plain":
			if m.TextBody == "" {
				m.TextBody = string(body)
			}
		case "text/html":
			if m.HTMLBody == "" {
				m.HTMLBody = string(body)
			}
		}
	}
	return m, nil
}

// DownloadAttachment is not used: raw mode carries attachments inline in the
// .eml and the importer decodes them via the MIME handler registry.
func (s *imapSource) DownloadAttachment(ctx context.Context, msg *Message, att Attachment) ([]byte, error) {
	return nil, fmt.Errorf("imap: attachments decode from raw .eml during import")
}

// IMAPFolders lists every selectable folder on the server (sorted).
func IMAPFolders(cfg IMAPConfig) ([]string, error) {
	c, err := imapclient.DialTLS(cfg.addr(), dialOptions())
	if err != nil {
		return nil, fmt.Errorf("imap dial %s: %w", cfg.addr(), err)
	}
	defer c.Close()
	if err := c.Login(cfg.User, cfg.Password).Wait(); err != nil {
		return nil, fmt.Errorf("imap login %s: %w", cfg.User, err)
	}
	listed, err := c.List("", "*", nil).Collect()
	if err != nil {
		return nil, fmt.Errorf("imap list: %w", err)
	}
	var folders []string
	for _, mb := range listed {
		if mb == nil || hasNoSelect(mb.Attrs) {
			continue
		}
		folders = append(folders, mb.Mailbox)
	}
	if len(folders) == 0 {
		return nil, fmt.Errorf("imap: no selectable folders found for %s", cfg.User)
	}
	sortStrings(folders)
	return folders, nil
}

// newIMAPSources dials one readonly source per folder. Empty Folders means
// auto-list every selectable folder on the server.
func newIMAPSources(ctx context.Context, cfg IMAPConfig) ([]Source, error) {
	folders := cfg.Folders
	if len(folders) == 0 {
		var err error
		if folders, err = IMAPFolders(cfg); err != nil {
			return nil, err
		}
	}
	out := make([]Source, 0, len(folders))
	for _, f := range folders {
		src := &imapSource{cfg: cfg, mailbox: f, dir: SanitizeFolder(f)}
		if err := src.dial(); err != nil {
			return nil, err
		}
		out = append(out, src)
	}
	return out, nil
}

// SanitizeFolder maps a mailbox name to one filesystem path segment,
// keeping dots (INBOX.Sent stays readable) but dropping separator hazards.
func SanitizeFolder(name string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", "*", "_", "?", "_",
		"\"", "_", "<", "_", ">", "_", "|", "_", " ", "_", "[", "", "]", "")
	out := strings.TrimSpace(r.Replace(name))
	if out == "" || out == "." || out == ".." {
		out = "_unnamed"
	}
	return out
}

func sortStrings(xs []string) {
	for i := 1; i < len(xs); i++ {
		for j := i; j > 0 && xs[j] < xs[j-1]; j-- {
			xs[j], xs[j-1] = xs[j-1], xs[j]
		}
	}
}

func hasNoSelect(attrs []imap.MailboxAttr) bool {
	for _, a := range attrs {
		if a == imap.MailboxAttrNoSelect || a == imap.MailboxAttrNonExistent {
			return true
		}
	}
	return false
}

// IMAPIdle blocks until ANY of the given folders reports a change (or the
// deadline passes). Used by bin/mail/watch.go between sync passes; a fresh
// short-lived connection per wait keeps the watcher crash-simple.
func IMAPIdle(cfg IMAPConfig, folders []string, maxWait time.Duration) error {
	if len(folders) == 0 {
		time.Sleep(maxWait)
		return nil
	}
	type result struct{ err error }
	done := make(chan result, len(folders))
	for _, f := range folders {
		go func(mailbox string) {
			done <- result{err: idleOne(cfg, mailbox, maxWait)}
		}(f)
	}
	// First wake-up wins; the rest unblock via their own timeouts.
	err := (<-done).err
	return err
}

func idleOne(cfg IMAPConfig, mailbox string, maxWait time.Duration) error {
	c, err := imapclient.DialTLS(cfg.addr(), dialOptions())
	if err != nil {
		return fmt.Errorf("idle dial: %w", err)
	}
	defer c.Close()
	if err := c.Login(cfg.User, cfg.Password).Wait(); err != nil {
		return fmt.Errorf("idle login: %w", err)
	}
	if _, err := c.Select(mailbox, &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return fmt.Errorf("idle select %s: %w", mailbox, err)
	}
	cmd, err := c.Idle()
	if err != nil {
		return fmt.Errorf("idle start %s: %w", mailbox, err)
	}
	// No public "notified" signal: IDLE terminates early when the server
	// pushes unilateral data, so race Wait against the deadline. Close is
	// single-shot; only the loser path calls it.
	waitErr := make(chan error, 1)
	go func() { waitErr <- cmd.Wait() }()
	timer := time.NewTimer(maxWait)
	defer timer.Stop()
	select {
	case err := <-waitErr:
		if err != nil && !strings.Contains(err.Error(), "EOF") {
			return fmt.Errorf("idle wait %s: %w", mailbox, err)
		}
		return nil // server pushed an update
	case <-timer.C:
		if err := cmd.Close(); err != nil && !strings.Contains(err.Error(), "already closed") {
			return fmt.Errorf("idle close %s: %w", mailbox, err)
		}
		return nil // quiet window elapsed
	}
}

// IMAPEnv picks IMAP_* credentials from an env map (file values already merged
// over process env by readEnv).
func IMAPEnv(env map[string]string) (*IMAPConfig, error) {
	host := pick(env["IMAP_HOST"], env["MAIL_IMAP_HOST"])
	user := pick(env["IMAP_USER"], env["MAIL_IMAP_USER"])
	pass := pick(env["IMAP_PASSWORD"], env["MAIL_IMAP_PASSWORD"])
	if host == "" || user == "" || pass == "" {
		return nil, fmt.Errorf("imap source needs IMAP_HOST/USER/PASSWORD in env (got host=%q user=%q pass=%v)", host, user, pass != "")
	}
	cfg := &IMAPConfig{Host: host, User: user, Password: pass}
	if p := pick(env["IMAP_PORT"], env["MAIL_IMAP_PORT"]); p != "" {
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err == nil {
			cfg.Port = n
		}
	}
	if fl := env["IMAP_FOLDERS"]; strings.TrimSpace(fl) != "" {
		for _, f := range strings.Split(fl, ",") {
			if f = strings.TrimSpace(f); f != "" {
				cfg.Folders = append(cfg.Folders, f)
			}
		}
	} else if os.Getenv("IMAP_FOLDERS_ALL") == "0" {
		cfg.Folders = []string{"INBOX"}
	}
	return cfg, nil
}
