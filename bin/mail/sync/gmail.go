package sync

import (
	"bytes"
	"context"
	"encoding/base64"
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

// GmailCredentials holds the OAuth files produced by the gmail MCP
// (@gongrzhe/server-gmail-autoauth-mcp) auto-auth flow.
type GmailCredentials struct {
	CredentialsPath string // ~/.gmail-mcp/credentials.json
	KeysPath        string // ~/.gmail-mcp/gcp-oauth.keys.json
	User            string // fixed: the authed account
}

// gmailToken is the JSON shape of credentials.json + refresh response.
type gmailToken struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	Expiry       int64  `json:"expiry_date"` // ms epoch
}

type gmailKeys struct {
	Installed *gmailKeyBlock `json:"installed"`
	Web       *gmailKeyBlock `json:"web"`
}
type gmailKeyBlock struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
}

// GmailClient talks to the Gmail REST API using the OAuth refresh token from
// ~/.gmail-mcp/. Token is refreshed lazily with a mutex-guarded cache.
type GmailClient struct {
	creds  GmailCredentials
	client *http.Client
	mu     chan struct{}
	token  *gmailToken
	user   string
}

func NewGmailClient(creds GmailCredentials) (*GmailClient, error) {
	if creds.CredentialsPath == "" {
		home, _ := os.UserHomeDir()
		creds.CredentialsPath = filepath.Join(home, ".gmail-mcp", "credentials.json")
		creds.KeysPath = filepath.Join(home, ".gmail-mcp", "gcp-oauth.keys.json")
	}
	g := &GmailClient{
		creds:  creds,
		client: &http.Client{Timeout: 60 * time.Second},
		mu:     make(chan struct{}, 1),
	}
	g.mu <- struct{}{}
	return g, nil
}

// accessToken returns a fresh bearer token, refreshing via the Google token
// endpoint when the cached one is missing or about to expire.
func (g *GmailClient) accessToken(ctx context.Context) (string, error) {
	select {
	case <-g.mu:
	case <-ctx.Done():
		return "", ctx.Err()
	}
	defer func() { g.mu <- struct{}{} }()
	if g.token != nil && g.token.AccessToken != "" && g.token.Expiry > time.Now().UnixMilli()+300_000 {
		return g.token.AccessToken, nil
	}
	return g.refreshLocked(ctx)
}

func (g *GmailClient) refreshLocked(ctx context.Context) (string, error) {
	cred, err := os.ReadFile(g.creds.CredentialsPath)
	if err != nil {
		return "", fmt.Errorf("read gmail credentials %s: %w", g.creds.CredentialsPath, err)
	}
	var t gmailToken
	if err := json.Unmarshal(cred, &t); err != nil {
		return "", fmt.Errorf("parse gmail credentials: %w", err)
	}
	if t.RefreshToken == "" {
		return "", errors.New("gmail credentials.json has no refresh_token (run the gmail MCP auth flow)")
	}
	keys, err := os.ReadFile(g.creds.KeysPath)
	if err != nil {
		return "", fmt.Errorf("read gmail keys %s: %w", g.creds.KeysPath, err)
	}
	var k gmailKeys
	if err := json.Unmarshal(keys, &k); err != nil {
		return "", fmt.Errorf("parse gmail keys: %w", err)
	}
	block := k.Installed
	if block == nil {
		block = k.Web
	}
	if block == nil {
		return "", errors.New("gmail gcp-oauth.keys.json has no installed/web block")
	}

	form := url.Values{}
	form.Set("client_id", block.ClientID)
	form.Set("client_secret", block.ClientSecret)
	form.Set("refresh_token", t.RefreshToken)
	form.Set("grant_type", "refresh_token")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://oauth2.googleapis.com/token",
		strings.NewReader(form.Encode()))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gmail token refresh: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
			Desc  string `json:"error_description"`
		}
		_ = json.Unmarshal(body, &e)
		if e.Error == "invalid_grant" {
			return "", fmt.Errorf("gmail OAuth token invalid/expired - re-auth via: npx -y @gongrzhe/server-gmail-autoauth-mcp auth (uses ~/.gmail-mcp)")
		}
		return "", fmt.Errorf("gmail token refresh status %d: %s", resp.StatusCode, truncate(string(body), 300))
	}
	var out struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int64  `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return "", fmt.Errorf("gmail token refresh parse: %w", err)
	}
	g.token = &gmailToken{
		AccessToken:  out.AccessToken,
		RefreshToken: t.RefreshToken,
		Expiry:       time.Now().UnixMilli() + out.ExpiresIn*1000,
	}
	return out.AccessToken, nil
}

// ListIDs returns message ids matching q, walking nextPageToken up to maxIDs
// (0 = unlimited). Thread-level pagination via the messages.list endpoint.
func (g *GmailClient) ListIDs(ctx context.Context, q string, maxIDs int, pageToken string) (ids []string, next string, err error) {
	for {
		params := url.Values{}
		params.Set("q", q)
		params.Set("maxResults", "100")
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}
		var out struct {
			Messages []struct {
				ID string `json:"id"`
			} `json:"messages"`
			NextPageToken string `json:"nextPageToken"`
		}
		if err := g.getJSON(ctx, "/gmail/v1/users/me/messages?"+params.Encode(), &out); err != nil {
			return nil, "", err
		}
		for _, m := range out.Messages {
			ids = append(ids, m.ID)
			if maxIDs > 0 && len(ids) >= maxIDs {
				return ids, out.NextPageToken, nil
			}
		}
		if out.NextPageToken == "" {
			break
		}
		pageToken = out.NextPageToken
	}
	return ids, "", nil
}

// GetMessage fetches a message in format=full and normalizes it.
func (g *GmailClient) GetMessage(ctx context.Context, id string) (*Message, error) {
	var raw struct {
		ID        string `json:"id"`
		ThreadID  string `json:"threadId"`
		InternalDate string `json:"internalDate"` // ms epoch string
		Payload   gmailPart
	}
	path := "/gmail/v1/users/me/messages/" + url.PathEscape(id) + "?format=full"
	if err := g.getJSON(ctx, path, &raw); err != nil {
		return nil, err
	}
	m := &Message{
		Source: "gmail",
		ID:     raw.ID,
		Folder: "gmail",
	}
	for _, h := range raw.Payload.Headers {
		switch strings.ToLower(h.Name) {
		case "subject":
			m.Subject = h.Value
		case "from":
			m.From = h.Value
		case "to":
			m.To = h.Value
		case "cc":
			m.CC = h.Value
		case "bcc":
			m.BCC = h.Value
		case "message-id":
			m.MimeMessageID = h.Value
		case "date":
			if t, err := time.Parse(time.RFC1123Z, h.Value); err == nil {
				m.ReceivedAt = t
			}
		}
	}
	if ms, err := parseMS(raw.InternalDate); err == nil {
		m.ReceivedAt = ms
	}
	m.TextBody, m.HTMLBody, m.Attachments = collectParts(raw.Payload, "root", m.ID, 0)
	m.HasAttachments = len(m.Attachments) > 0
	return m, nil
}

type gmailPart struct {
	PartID   string      `json:"partId"`
	MimeType string      `json:"mimeType"`
	Filename string      `json:"filename"`
	Body     gmailBody   `json:"body"`
	Headers  []gmailHeader `json:"headers"`
	Parts    []gmailPart `json:"parts"`
}
type gmailHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}
type gmailBody struct {
	Size         int64  `json:"size"`
	Data         string `json:"data"`
	AttachmentID string `json:"attachmentId"`
}

// collectParts walks the MIME tree: text bodies into plain/html, anything with
// a filename into attachments (returned with base64 ids for later download).
func collectParts(p gmailPart, mime string, msgID string, depth int) (text, html string, atts []Attachment) {
	if depth > 16 {
		return
	}
	mt := strings.ToLower(p.MimeType)
	if p.Filename != "" && mt != "text/plain" && mt != "text/html" {
		pid := p.PartID
		if pid == "" {
			pid = fmt.Sprintf("%d", depth)
		}
		// Gmail's attachments API keys off body.attachmentId, not partId.
		attID := p.Body.AttachmentID
		if attID == "" {
			attID = pid
		}
		atts = append(atts, Attachment{
			FileID:      msgID + ":" + attID,
			FileName:    p.Filename,
			StoredName:  p.Filename,
			Size:        p.Body.Size,
			ContentType: p.MimeType,
		})
	} else if data, err := base64.URLEncoding.DecodeString(p.Body.Data); err == nil && len(p.Body.Data) > 0 {
		s := string(data)
		if mt == "text/html" && html == "" {
			html = s
		} else if (mt == "text/plain" || mt == "") && text == "" {
			text = s
		}
	}
	for _, child := range p.Parts {
		t, h, a := collectParts(child, mt, msgID, depth+1)
		if text == "" {
			text = t
		}
		if html == "" {
			html = h
		}
		atts = append(atts, a...)
	}
	return
}

// GetMessageRaw fetches a message in format=raw and returns the decoded RFC
// 822 bytes (headers + multipart body + inline attachments). This is the raw
// email that the enmime-based importer (mailconv.FromEML) reads.
func (g *GmailClient) GetMessageRaw(ctx context.Context, id string) ([]byte, error) {
	var out struct {
		Raw string `json:"raw"`
	}
	path := "/gmail/v1/users/me/messages/" + url.PathEscape(id) + "?format=raw"
	if err := g.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	b, err := base64.URLEncoding.DecodeString(out.Raw)
	if err != nil {
		return nil, fmt.Errorf("gmail decode raw %s: %w", id, err)
	}
	return b, nil
}

// DownloadAttachment fetches an attachment's bytes from the Gmail API.
func (g *GmailClient) DownloadAttachment(ctx context.Context, msgID, attID string) ([]byte, error) {
	// attID format is "<msgId>:<partId>"; the API needs the bare attachment id.
	partID := attID
	if i := strings.Index(attID, ":"); i >= 0 {
		partID = attID[i+1:]
	}
	var out struct {
		Data string `json:"data"`
	}
	path := "/gmail/v1/users/me/messages/" + url.PathEscape(msgID) + "/attachments/" + url.PathEscape(partID)
	if err := g.getJSON(ctx, path, &out); err != nil {
		return nil, err
	}
	return base64.URLEncoding.DecodeString(out.Data)
}

func (g *GmailClient) getJSON(ctx context.Context, path string, out any) error {
	tok, err := g.accessToken(ctx)
	if err != nil {
		return err
	}
	u := "https://gmail.googleapis.com" + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 96<<20))
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("gmail %s: status %d: %s", path, resp.StatusCode, truncate(string(body), 300))
	}
	if out != nil {
		return json.Unmarshal(body, out)
	}
	return nil
}

func parseMS(s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, errors.New("empty")
	}
	var ms int64
	if _, err := fmt.Sscanf(s, "%d", &ms); err != nil {
		return time.Time{}, err
	}
	return time.UnixMilli(ms), nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

var _ = bytes.MinRead
