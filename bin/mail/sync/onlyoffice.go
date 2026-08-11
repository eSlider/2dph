package sync

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"time"
)

// OOConfig mirrors the .env / environment used by bin/mail/import.
type OOConfig struct {
	URL      string
	User     string
	Password string
}

// OOClient is a minimal OnlyOffice API client: authentication.json for the
// bearer token plus the session cookie jar required by the .ashx download
// handler. It mirrors the endpoint contract bin/mail/import already uses.
type OOClient struct {
	cfg      OOConfig
	client   *http.Client
	mu       chan struct{}
	token    string
	folderID int
}

func NewOOClient(cfg OOConfig, folderID int) (*OOClient, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	c := &OOClient{
		cfg:      cfg,
		client:   &http.Client{Jar: jar, Timeout: 60 * time.Second},
		mu:       make(chan struct{}, 1),
		folderID: folderID,
	}
	c.mu <- struct{}{}
	if err := c.authenticate(context.Background()); err != nil {
		return nil, err
	}
	return c, nil
}

func (o *OOClient) authenticate(ctx context.Context) error {
	select {
	case <-o.mu:
	case <-ctx.Done():
		return ctx.Err()
	}
	defer func() { o.mu <- struct{}{} }()
	body, _ := json.Marshal(map[string]any{
		"userName": o.cfg.User, "password": o.cfg.Password, "type": 0,
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		strings.TrimRight(o.cfg.URL, "/")+"/api/2.0/authentication.json",
		strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("oo authenticate status %d: %s", resp.StatusCode, truncate(string(data), 200))
	}
	var out struct {
		Response struct {
			Token string `json:"token"`
		} `json:"response"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return err
	}
	if out.Response.Token == "" {
		return fmt.Errorf("oo authenticate: empty token")
	}
	o.token = out.Response.Token
	return nil
}

// get performs an authenticated GET and decodes the JSON body into out.
func (o *OOClient) get(ctx context.Context, path string, out any) error {
	u := strings.TrimRight(o.cfg.URL, "/") + path
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+o.token)
	req.Header.Set("Accept", "application/json")
	resp, err := o.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return fmt.Errorf("oo %s: status %d: %s", path, resp.StatusCode, truncate(string(data), 300))
	}
	if out != nil {
		return json.Unmarshal(data, out)
	}
	return nil
}

// ooMessage mirrors the OnlyOffice mail message JSON (subset we need).
type ooMessage struct {
	ID             int    `json:"id"`
	Subject        string `json:"subject"`
	From           string `json:"from"`
	To             string `json:"to"`
	CC             string `json:"cc"`
	BCC            string `json:"bcc"`
	ReceivedDate   string `json:"receivedDate"`
	HTMLBody       string `json:"htmlBody"`
	TextBody       string `json:"textBody"`
	HasAttachments bool   `json:"hasAttachments"`
	MimeMessageID  string `json:"mimeMessageId"`
	Attachments    []struct {
		FileID      int    `json:"fileId"`
		FileName    string `json:"fileName"`
		StoredName  string `json:"storedName"`
		Size        int64  `json:"size"`
		ContentType string `json:"contentType"`
	} `json:"attachments"`
}

// ListIDs returns message ids in the configured folder, paginating pages until
// maxIDs is reached (0 = all).
func (o *OOClient) ListIDs(ctx context.Context, maxIDs int, page int) (ids []int, next int, err error) {
	var out struct {
		Response []ooMessage `json:"response"`
	}
	count := 100
	if maxIDs > 0 && maxIDs < count {
		count = maxIDs
	}
	path := fmt.Sprintf("/api/2.0/mail/messages?folder=%d&page=%d&count=%d", o.folderID, page, count)
	if err := o.get(ctx, path, &out); err != nil {
		return nil, 0, err
	}
	for _, m := range out.Response {
		ids = append(ids, m.ID)
		if maxIDs > 0 && len(ids) >= maxIDs {
			break
		}
	}
	next = page + 1
	return ids, next, nil
}

// GetMessage fetches the full message by id and normalizes into Message.
func (o *OOClient) GetMessage(ctx context.Context, id int) (*Message, error) {
	var out struct {
		Response ooMessage `json:"response"`
	}
	path := fmt.Sprintf("/api/2.0/mail/messages/%d", id)
	if err := o.get(ctx, path, &out); err != nil {
		return nil, err
	}
	m := out.Response
	msg := &Message{
		Source:         "onlyoffice",
		ID:             fmt.Sprintf("%d", m.ID),
		Folder:         "oo",
		Subject:        m.Subject,
		From:           m.From,
		To:             m.To,
		CC:             m.CC,
		BCC:            m.BCC,
		HTMLBody:       m.HTMLBody,
		TextBody:       m.TextBody,
		HasAttachments: m.HasAttachments,
		MimeMessageID:  m.MimeMessageID,
	}
	if t, err := time.Parse(time.RFC3339Nano, m.ReceivedDate); err == nil {
		msg.ReceivedAt = t
	}
	for _, a := range m.Attachments {
		msg.Attachments = append(msg.Attachments, Attachment{
			FileID:      fmt.Sprintf("%d", a.FileID),
			FileName:    a.FileName,
			StoredName:  a.StoredName,
			Size:        a.Size,
			ContentType: a.ContentType,
		})
	}
	return msg, nil
}

// DownloadAttachment fetches attachment bytes via the .ashx handler, which
// requires the session cookie (client.Jar) captured during authenticate().
func (o *OOClient) DownloadAttachment(ctx context.Context, fileID string) ([]byte, error) {
	u := strings.TrimRight(o.cfg.URL, "/") + "/addons/mail/httphandlers/download.ashx?attachid=" + url.QueryEscape(fileID)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	resp, err := o.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("oo download attach %s: status %d", fileID, resp.StatusCode)
	}
	return data, nil
}
