package sync

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// M365Credentials holds a Microsoft Graph app registration with the Mail.Read
// application permission. Client credentials are read from env/.env, never
// committed.
type M365Credentials struct {
	Tenant       string
	ClientID     string
	ClientSecret string
	Users        []string // mailbox addresses to sync, e.g. info@example.com
}

// m365Token is the cached access token with expiry.
type m365Token struct {
	AccessToken string
	Expiry      time.Time
}

// M365Client talks to the Microsoft Graph API using the client-credentials
// flow (app registration with Mail.Read application permission). GET-only:
// messages are never marked as read or deleted.
type M365Client struct {
	creds         M365Credentials
	base          string // graph base URL; default https://graph.microsoft.com
	tokenEndpoint string // login endpoint; default https://login.microsoftonline.com
	client        *http.Client
	mu            chan struct{}
	token         *m365Token
}

func NewM365Client(creds M365Credentials) (*M365Client, error) {
	if creds.Tenant == "" || creds.ClientID == "" || creds.ClientSecret == "" {
		return nil, errors.New("m365 needs tenant, client id and client secret")
	}
	c := &M365Client{
		creds:  creds,
		base:   "https://graph.microsoft.com",
		client: &http.Client{Timeout: 90 * time.Second},
		mu:     make(chan struct{}, 1),
	}
	c.mu <- struct{}{}
	return c, nil
}

// accessToken returns a fresh bearer token, refreshing via the Azure AD token
// endpoint when the cached one is missing or about to expire (within 2 min).
func (c *M365Client) accessToken(ctx context.Context) (string, error) {
	select {
	case <-c.mu:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { c.mu <- struct{}{} }()
	if c.token != nil && c.token.AccessToken != "" && time.Now().Before(c.token.Expiry.Add(-2*time.Minute)) {
		return c.token.AccessToken, nil
	}
	return c.refreshLocked(ctx)
}

func (c *M365Client) refreshLocked(ctx context.Context) (string, error) {
	form := url.Values{}
	form.Set("grant_type", "client_credentials")
	form.Set("client_id", c.creds.ClientID)
	form.Set("client_secret", c.creds.ClientSecret)
	form.Set("scope", "https://graph.microsoft.com/.default")

	endpoint := c.tokenEndpoint
	if endpoint == "" {
		endpoint = fmt.Sprintf("https://login.microsoftonline.com/%s/oauth2/v2.0/token", c.creds.Tenant)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("m365 token: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
			Desc  string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &e)
		return "", fmt.Errorf("m365 token status %d: %s (%s)", resp.StatusCode, e.Error, truncate(e.Desc, 200))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("m365 token parse: %w", err)
	}
	c.token = &m365Token{
		AccessToken: out.AccessToken,
		Expiry:      time.Now().Add(time.Duration(out.ExpiresIn) * time.Second),
	}
	return out.AccessToken, nil
}

// deltaPage is one response page of the Graph delta query.
type deltaPage struct {
	Value []struct {
		ID            string `json:"id"`
		RemovedReason string `json:"@odata.removedReason"`
	} `json:"value"`
	NextLink  string `json:"@odata.nextLink"`
	DeltaLink string `json:"@odata.deltaLink"`
}

// ListDeltaIDs walks the inbox delta query and returns live message ids since
// the previous deltaLink (or the full inbox when deltaLink is empty). Returns
// the new deltaLink for the next run. GET-only; nothing is mutated server-side.
func (c *M365Client) ListDeltaIDs(ctx context.Context, mailbox, deltaLink string, limit int) ([]string, string, error) {
	var (
		ids   []string
		url   string
		link  = deltaLink
	)
	if link == "" {
		url = fmt.Sprintf("/v1.0/users/%s/mailFolders/inbox/messages/delta", pathEscape(mailbox))
	} else {
		url = link
	}
	for url != "" {
		var page deltaPage
		if err := c.getJSON(ctx, url, &page); err != nil {
			return ids, link, err
		}
		for _, m := range page.Value {
			if m.RemovedReason != "" {
				continue
			}
			if m.ID == "" {
				continue
			}
			ids = append(ids, m.ID)
			if limit > 0 && len(ids) >= limit {
				if page.DeltaLink != "" {
					link = page.DeltaLink
				}
				return ids, link, nil
			}
		}
		if page.DeltaLink != "" {
			link = page.DeltaLink
			url = ""
			break
		}
		url = page.NextLink
	}
	return ids, link, nil
}

// GetMessage fetches a single message by id and normalizes it to the Message
// contract. GET-only.
func (c *M365Client) GetMessage(ctx context.Context, mailbox, id string) (*Message, error) {
	path := fmt.Sprintf("/v1.0/users/%s/messages/%s?$expand=attachments($select=id,name,contentType,size,isInline)",
		pathEscape(mailbox), pathEscape(id))
	var raw struct {
		ID                string             `json:"id"`
		Subject           string             `json:"subject"`
		From              m365Recipient      `json:"from"`
		ToRecipients      []m365Recipient    `json:"toRecipients"`
		CCRecipients      []m365Recipient    `json:"ccRecipients"`
		BCCRecipients     []m365Recipient    `json:"bccRecipients"`
		ReceivedDateTime  string             `json:"receivedDateTime"`
		Body              m365Body           `json:"body"`
		BodyPreview       string             `json:"bodyPreview"`
		InternetMessageID string             `json:"internetMessageId"`
		Attachments       []m365Attachment   `json:"attachments"`
	}
	if err := c.getJSON(ctx, path, &raw); err != nil {
		return nil, err
	}
	m := &Message{
		Source:        "m365",
		ID:            raw.ID,
		Folder:        "m365",
		Subject:       raw.Subject,
		From:          formatRecipient(raw.From),
		To:            formatRecipients(raw.ToRecipients),
		CC:            formatRecipients(raw.CCRecipients),
		BCC:           formatRecipients(raw.BCCRecipients),
		MimeMessageID: raw.InternetMessageID,
	}
	if t, err := time.Parse(time.RFC3339, raw.ReceivedDateTime); err == nil {
		m.ReceivedAt = t
	}
	switch strings.ToLower(raw.Body.ContentType) {
	case "html":
		m.HTMLBody = raw.Body.Content
		if raw.BodyPreview != "" {
			m.TextBody = raw.BodyPreview
		}
	default:
		m.TextBody = raw.Body.Content
		if raw.BodyPreview != "" {
			m.HTMLBody = raw.BodyPreview
		}
	}
	for _, a := range raw.Attachments {
		if a.IsInline || a.ID == "" || a.Name == "" {
			continue
		}
		m.Attachments = append(m.Attachments, Attachment{
			FileID:      a.ID,
			FileName:    a.Name,
			StoredName:  a.Name,
			Size:        a.Size,
			ContentType: a.ContentType,
		})
	}
	m.HasAttachments = len(m.Attachments) > 0
	return m, nil
}

type m365Recipient struct {
	EmailAddress struct {
		Name    string `json:"name"`
		Address string `json:"address"`
	} `json:"emailAddress"`
}

type m365Body struct {
	ContentType string `json:"contentType"`
	Content     string `json:"content"`
}

type m365Attachment struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"contentType"`
	Size        int64  `json:"size"`
	IsInline    bool   `json:"isInline"`
	ContentID   string `json:"contentId"`
}

func formatRecipients(rs []m365Recipient) string {
	var parts []string
	for _, r := range rs {
		if s := formatRecipient(r); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, ", ")
}

func formatRecipient(r m365Recipient) string {	a := r.EmailAddress.Address
	n := r.EmailAddress.Name
	switch {
	case n == "" || n == a:
		return a
	case a == "":
		return n
	default:
		return fmt.Sprintf("%s <%s>", n, a)
	}
}

// DownloadAttachment fetches an attachment's raw bytes via the /$value stream.
func (c *M365Client) DownloadAttachment(ctx context.Context, mailbox, msgID, attID string) ([]byte, error) {
	path := fmt.Sprintf("/v1.0/users/%s/messages/%s/attachments/%s/$value",
		pathEscape(mailbox), pathEscape(msgID), pathEscape(attID))
	tok, err := c.accessToken(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+path, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("m365 attachment: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 256<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("m365 attachment %s: status %d: %s", attID, resp.StatusCode, truncate(string(body), 300))
	}
	return body, nil
}

func (c *M365Client) getJSON(ctx context.Context, path string, out any) error {
	tok, err := c.accessToken(ctx)
	if err != nil {
		return err
	}
	u := path
	if !strings.HasPrefix(u, "http") {
		u = c.base + u
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.client.Do(req)
	if err != nil {
		return fmt.Errorf("m365 %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("m365 %s: status %d: %s", path, resp.StatusCode, truncate(string(body), 300))
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

// m365Source adapts a mailbox to the Source worker-pool contract. Each mailbox
// gets its own folder under var/mail/m365/<localpart>/ and a delta state file.
type m365Source struct {
	c         *M365Client
	mailbox   string
	localpart  string
	stateDir   string
	pending    string // delta link to persist on Commit()
	hasPending bool
}

func (s *m365Source) Folder() string { return filepath.Join("m365", s.localpart) }

func (s *m365Source) ListIDs(ctx context.Context, limit int, cursor string) ([]string, string, error) {
	link, _ := os.ReadFile(filepath.Join(s.stateDir, s.localpart+".deltalink"))
	ids, newLink, err := s.c.ListDeltaIDs(ctx, s.mailbox, strings.TrimSpace(string(link)), limit)
	if err != nil {
		return nil, "", err
	}
	// Buffer the new delta link; persist it only in Commit() after the full
	// batch downloaded, so a failed run stays retryable without gaps.
	if newLink != "" {
		s.pending = newLink
		s.hasPending = true
	}
	return ids, "", nil
}

// Commit persists the buffered delta link. Called by the sync runner only when
// every listed message downloaded successfully.
func (s *m365Source) Commit() error {
	if !s.hasPending || s.pending == "" {
		return nil
	}
	if err := os.MkdirAll(s.stateDir, 0o755); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(s.stateDir, s.localpart+".deltalink"), []byte(s.pending), 0o644)
}

func (s *m365Source) Get(ctx context.Context, id string) (*Message, error) {
	return s.c.GetMessage(ctx, s.mailbox, id)
}

func (s *m365Source) DownloadAttachment(ctx context.Context, msg *Message, att Attachment) ([]byte, error) {
	return s.c.DownloadAttachment(ctx, s.mailbox, msg.ID, att.FileID)
}

func pathEscape(s string) string {
	return url.PathEscape(s)
}
