// Package brainclient — сервисный клиент read-контракта brain (P-9.5,
// docs/brain/read-contract.md): тонкая HTTP-обёртка над search/get/stats/
// audit с typed-ответами internal/contract (JSON-схема P-9.4) и «гейтом
// facts» (gate.go) — клиент никогда не выдаёт не подтверждённый ответ как
// подтверждённый факт.
//
// Клиент ходит ТОЛЬКО через read-контракт (HTTP :8630, bin/brain/serve.go)
// и никогда не открывает var/kb.lbug напрямую. Ответы проверяются
// контрактными валидаторами internal/contract (формат + contract_version);
// несовместимый/старый сервис → ошибка, а не молчаливая порча данных.
//
// Пакет cgo-free и не зависит от Ladybug: gator/cv/агенты могут читать brain
// с любого хоста, где поднят сервис. Вынос типов контракта в публичный
// go-* модуль для кросс-репо импорта — P-9.6 (ADR).
package brainclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/eSlider/2dph/internal/contract"
)

// Config — параметры подключения. Base — обязательный base URL сервиса
// (http://host:port), например из internal/config (host/port). Token —
// опциональный Bearer-токен (сервер с SetToken). Timeout нулевой → 30s.
type Config struct {
	Base    string
	Token   string
	Timeout time.Duration
}

// Client — один транспортный хендл к brain API. Методы синхронны и
// безопасны для параллельного использования (http.Client thread-safe).
type Client struct {
	hc    *http.Client
	base  string
	token string
}

// New возвращает клиент с cfg. Пустой base не валиден — ошибка всплывёт на
// первом же вызове (клиент никуда не ходит до явного метода).
func New(cfg Config) *Client {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	return &Client{
		hc:    &http.Client{Timeout: timeout},
		base:  strings.TrimRight(cfg.Base, "/"),
		token: cfg.Token,
	}
}

// SearchOptions — опции поиска (зеркало query-параметров /search).
type SearchOptions struct {
	Root  string // facts|info; пусто = все корни
	Limit int    // 1..100; 0 = серверный дефолт (10)
	AsOf  string // YYYY-MM-DD (D24): факты, активные на день
	NoWeb bool   // не подмешивать второй источник (noweb=1)
}

func (o SearchOptions) validate(query string) error {
	if strings.TrimSpace(query) == "" {
		return fmt.Errorf("brainclient: search: empty query")
	}
	switch o.Root {
	case "", "facts", "info":
	default:
		return fmt.Errorf("brainclient: search: root must be facts|info, got %q", o.Root)
	}
	if !contract.ValidDay(o.AsOf) {
		return fmt.Errorf("brainclient: search: as_of %q is not YYYY-MM-DD", o.AsOf)
	}
	if o.Limit < 0 || o.Limit > 100 {
		return fmt.Errorf("brainclient: search: limit must be 1..100, got %d", o.Limit)
	}
	return nil
}

// get выполняет GET path с query и декодирует тело в raw (валидация и
// типизация — у вызывающего).
func (c *Client) get(ctx context.Context, path string, q url.Values) ([]byte, error) {
	if c.base == "" {
		return nil, fmt.Errorf("brainclient: empty base URL")
	}
	u := c.base + path
	if len(q) > 0 {
		u += "?" + q.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("brainclient: %w", err)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("brainclient: GET %s: %w", path, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return nil, fmt.Errorf("brainclient: GET %s: read: %w", path, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("brainclient: GET %s: HTTP %d: %s", path, resp.StatusCode, snippet(body))
	}
	return body, nil
}

func snippet(b []byte) string {
	s := strings.TrimSpace(string(b))
	if len(s) > 160 {
		s = s[:160]
	}
	return s
}

// Search — поиск по read-контракту. Ответ проходит контрактную валидацию
// (internal/contract.ValidateSearchResponse): несовместимый сервис — ошибка.
// Для ответа с гейтом facts (--root facts) используйте Facts.
func (c *Client) Search(ctx context.Context, query string, opt SearchOptions) (*contract.SearchResponse, error) {
	if err := opt.validate(query); err != nil {
		return nil, err
	}
	q := url.Values{"q": {query}}
	if opt.Root != "" {
		q.Set("root", opt.Root)
	}
	if opt.AsOf != "" {
		q.Set("as_of", opt.AsOf)
	}
	if opt.Limit > 0 {
		q.Set("n", fmt.Sprintf("%d", opt.Limit))
	}
	if opt.NoWeb {
		q.Set("noweb", "1")
	}
	body, err := c.get(ctx, "/search", q)
	if err != nil {
		return nil, err
	}
	resp, err := contract.ValidateSearchResponse(body)
	if err != nil {
		return nil, fmt.Errorf("brainclient: search: %w", err)
	}
	return &resp, nil
}

// Facts — поиск с root=facts + «гейт facts»: ответ разделяется на
// подтверждённые факты (Confirmed) и отклонённое (NotConfirmed, с причиной).
// Клиент не должен подавать NotConfirmed как факты.
func (c *Client) Facts(ctx context.Context, query string, opt SearchOptions) (*Facts, error) {
	opt.Root = "facts"
	resp, err := c.Search(ctx, query, opt)
	if err != nil {
		return nil, err
	}
	f := GateSearch(resp)
	f.RootFilter = "facts" // клиентская мода: ответ сервера может нести root_filter=""
	return f, nil
}

// GetOptions — опции чтения одного листа.
type GetOptions struct {
	Body bool // включить полный текст (text); иначе — только метаданные
}

// Get — чтение листа по id (GET /get). Несуществующий id — ошибка с HTTP
// статусом и телом сервиса.
func (c *Client) Get(ctx context.Context, id string, opt GetOptions) (*contract.GetResponse, error) {
	if strings.TrimSpace(id) == "" {
		return nil, fmt.Errorf("brainclient: get: empty id")
	}
	q := url.Values{"id": {id}}
	if opt.Body {
		q.Set("body", "1")
	}
	body, err := c.get(ctx, "/get", q)
	if err != nil {
		return nil, err
	}
	resp, err := contract.ValidateGetResponse(body)
	if err != nil {
		return nil, fmt.Errorf("brainclient: get: %w", err)
	}
	return &resp, nil
}

// Stats — индексная статистика (GET /stats).
func (c *Client) Stats(ctx context.Context) (*contract.StatsResponse, error) {
	body, err := c.get(ctx, "/stats", nil)
	if err != nil {
		return nil, err
	}
	resp, err := contract.ValidateStatsResponse(body)
	if err != nil {
		return nil, fmt.Errorf("brainclient: stats: %w", err)
	}
	return &resp, nil
}

// Audit — гистограмма root × confidence (GET /audit). Гейт по facts-корню —
// GateAudit.
func (c *Client) Audit(ctx context.Context) (*contract.AuditResponse, error) {
	body, err := c.get(ctx, "/audit", nil)
	if err != nil {
		return nil, err
	}
	resp, err := contract.ValidateAuditResponse(body)
	if err != nil {
		return nil, fmt.Errorf("brainclient: audit: %w", err)
	}
	return &resp, nil
}
