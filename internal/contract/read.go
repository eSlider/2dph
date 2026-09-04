package contract

// Read-контракт brain (P-9.4, docs/brain/read-contract.md): нормативный
// формат ответов search/get/stats/audit + версионность. Зеркало write-
// контракта (Leaf/ContentHash) — пакет cgo-free, работает и тестируется без
// Ladybug. Клиенты читают brain только через эти форматы (HTTP :8630 /
// MCP / CLI --json), никогда не парсят var/kb.lbug напрямую.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// ReadContractVersion — текущая версия контракта чтения (semver MAJOR.MINOR).
// Additive-изменения (новые опциональные поля, уточнение доков) поднимают
// MINOR; breaking-изменения (удаление/переименование обязательных полей,
// смена типов) — MAJOR. Клиенты обязаны принимать любой ответ с тем же
// MAJOR и НЕ обязаны понимать более новые MINOR (поле contract_version в
// ответе). Отсутствие поля = ответ сформирован до контракта (старый код).
const ReadContractVersion = "1.0"

// --- Типы ответов (JSON-схема: поля, обязательность, отсутствие → отсутствие) ---

// HopNode — узел обхода --hop N (File → Commit → Person).
type HopNode struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Name  string `json:"name"`
	Depth int    `json:"depth"`
}

// SearchHit — один результат поиска.
type SearchHit struct {
	ID         string    `json:"id"`                   // required, непустой
	Text       string    `json:"text"`                 // required (полный текст листа)
	Root       string    `json:"root"`                 // required, facts|info
	Confidence string    `json:"confidence,omitempty"` // optional; confirmed|estimated|inferred|…
	Score      float64   `json:"score"`                // required (RRF/косинус)
	Snippet    string    `json:"snippet,omitempty"`    // optional, до 280 рун
	ValidFrom  string    `json:"valid_from,omitempty"` // optional, YYYY-MM-DD (D24)
	ValidTo    string    `json:"valid_to,omitempty"`   // optional, YYYY-MM-DD (D24)
	Hops       []HopNode `json:"hops,omitempty"`       // optional, только с --hop
}

// WebBlock — второй независимый источник (SearXNG), держится отдельно от
// графовых hits: «наши» vs «не наши».
type WebBlock struct {
	Status  string        `json:"status"`            // required: ok|throttled|skipped|refused|…
	Note    string        `json:"note,omitempty"`    // optional
	Cached  bool          `json:"cached,omitempty"`  // optional
	Results []WebBlockHit `json:"results,omitempty"` // optional
}

// WebBlockHit — одна ссылка web-блока.
type WebBlockHit struct {
	Rank    int    `json:"rank"`
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
	Engine  string `json:"engine"`
}

// SearchResponse — ответ search (GET /search, MCP search, CLI --json).
type SearchResponse struct {
	ContractVersion string      `json:"contract_version"` // required, semver MAJOR.MINOR
	Query           string      `json:"query"`            // required
	RootFilter      string      `json:"root_filter"`      // required; "" = все корни
	AsOf            string      `json:"as_of,omitempty"`  // optional, YYYY-MM-DD
	Count           int         `json:"count"`            // required, >= 0
	Results         []SearchHit `json:"results"`          // required, массив (может быть пустым)
	Web             *WebBlock   `json:"web,omitempty"`    // optional
}

// GetResponse — ответ get (GET /get?id=…, MCP get, CLI --json).
type GetResponse struct {
	ContractVersion string `json:"contract_version"`     // required
	ID              string `json:"id"`                   // required
	Root            string `json:"root"`                 // required, facts|info
	Confidence      string `json:"confidence"`           // required
	Source          string `json:"source"`               // required (корпус: mail/git/chats/docs/facts)
	Type            string `json:"type"`                 // required (kind листа)
	ValidFrom       string `json:"valid_from,omitempty"` // optional, YYYY-MM-DD
	ValidTo         string `json:"valid_to,omitempty"`   // optional, YYYY-MM-DD
	Snippet         string `json:"snippet,omitempty"`    // optional (без --body)
	Text            string `json:"text,omitempty"`       // optional (с --body)
}

// StatsResponse — ответ stats (GET /stats, CLI --json). required-ядро едино
// для CLI и HTTP; model (CLI) и ann (HTTP) — аддитивные расширения.
type StatsResponse struct {
	ContractVersion string         `json:"contract_version"` // required
	Total           int            `json:"total"`            // required, >= 0
	ByRoot          map[string]int `json:"by_root"`          // required, фактические корни
	DB              string         `json:"db"`               // required (путь к kb.lbug)
}

// AuditRow — одна строка гистограммы audit (root × confidence).
type AuditRow struct {
	Root       string `json:"root"`       // required, facts|info
	Confidence string `json:"confidence"` // required
	Count      int    `json:"count"`      // required, >= 0
}

// AuditResponse — ответ audit (GET /audit, MCP audit).
type AuditResponse struct {
	ContractVersion string     `json:"contract_version"` // required
	Status          string     `json:"status"`           // required, "ok"
	ByConfidence    []AuditRow `json:"by_confidence"`    // required, массив
}

// --- Доменные предикаты (общие для валидаторов и гейта) ---

// IsRoot — допустимое значение root: facts (утверждение, ≥2 источника) или
// info (нарратив). Единый термин с write-контрактом (contract.md).
func IsRoot(s string) bool { return s == "facts" || s == "info" }

// IsConfidence — confidence непустой; домен открыт
// (confirmed/estimated/inferred/hypothesis/partial/…).
func IsConfidence(s string) bool { return s != "" }

// ValidDay — пусто (отсутствие) или YYYY-MM-DD (D24 день валидности).
func ValidDay(s string) bool {
	if s == "" {
		return true
	}
	if len(s) != 10 || s[4] != '-' || s[7] != '-' {
		return false
	}
	for i := 0; i < 10; i++ {
		if i == 4 || i == 7 {
			continue
		}
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	// Месяц 1..12 и день 1..31 обязательны — «2026-13-40» это не день.
	m, err1 := strconv.Atoi(s[5:7])
	d, err2 := strconv.Atoi(s[8:10])
	return err1 == nil && err2 == nil && m >= 1 && m <= 12 && d >= 1 && d <= 31
}

// CompatibleVersion — ответ совместим с текущим контрактом: contract_version
// парсится как MAJOR.MINOR[.PATCH] и MAJOR совпадает с текущим.
// Дополнительные MINOR аддитивны — клиент обязан принимать тот же MAJOR.
func CompatibleVersion(v string) bool {
	major, _, ok := parseVersion(v)
	return ok && major == readContractMajor()
}

// parseVersion разбирает "N.M[.P]" в (major, minor).
func parseVersion(v string) (int, int, bool) {
	if v == "" {
		return 0, 0, false
	}
	parts := strings.Split(v, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, 0, false
	}
	major, err1 := strconv.Atoi(parts[0])
	minor, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || major < 0 || minor < 0 {
		return 0, 0, false
	}
	return major, minor, true
}

func readContractMajor() int {
	major, _, _ := parseVersion(ReadContractVersion)
	return major
}

// --- Валидаторы (формат ответов; первая же ошибка) ---

// rawKeys возвращает топ-левел ключи JSON-объекта (для required-полей,
// чьё значение может быть пустым — присутствие != значение).
func rawKeys(body []byte) (map[string]json.RawMessage, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("contract: not a JSON object: %w", err)
	}
	return raw, nil
}

func checkVersion(v string) error {
	if v == "" {
		return fmt.Errorf("contract: contract_version missing")
	}
	if !CompatibleVersion(v) {
		return fmt.Errorf("contract: contract_version %q incompatible with %q", v, ReadContractVersion)
	}
	return nil
}

// ValidateSearchResponse проверяет JSON ответа search против схемы контракта.
func ValidateSearchResponse(body []byte) (SearchResponse, error) {
	var r SearchResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return r, fmt.Errorf("contract: search: %w", err)
	}
	if err := checkVersion(r.ContractVersion); err != nil {
		return r, err
	}
	if r.Query == "" {
		return r, fmt.Errorf("contract: search: query missing")
	}
	// root_filter обязателен, но его значение может быть "" (все корни) —
	// присутствие проверяем по сырому ключу, а не по значению.
	raw, err := rawKeys(body)
	if err != nil {
		return r, err
	}
	if _, ok := raw["root_filter"]; !ok {
		return r, fmt.Errorf("contract: search: root_filter missing")
	}
	if r.Count < 0 {
		return r, fmt.Errorf("contract: search: count must be >= 0, got %d", r.Count)
	}
	if r.Results == nil {
		return r, fmt.Errorf("contract: search: results missing")
	}
	if r.AsOf != "" && !ValidDay(r.AsOf) {
		return r, fmt.Errorf("contract: search: as_of %q is not YYYY-MM-DD", r.AsOf)
	}
	for i, h := range r.Results {
		if h.ID == "" {
			return r, fmt.Errorf("contract: search: results[%d].id missing", i)
		}
		if !IsRoot(h.Root) {
			return r, fmt.Errorf("contract: search: results[%d].root %q, want facts|info", i, h.Root)
		}
		if !ValidDay(h.ValidFrom) {
			return r, fmt.Errorf("contract: search: results[%d].valid_from %q is not YYYY-MM-DD", i, h.ValidFrom)
		}
		if !ValidDay(h.ValidTo) {
			return r, fmt.Errorf("contract: search: results[%d].valid_to %q is not YYYY-MM-DD", i, h.ValidTo)
		}
		for j, n := range h.Hops {
			if n.ID == "" {
				return r, fmt.Errorf("contract: search: results[%d].hops[%d].id missing", i, j)
			}
			if n.Label == "" {
				return r, fmt.Errorf("contract: search: results[%d].hops[%d].label missing", i, j)
			}
			if n.Name == "" {
				return r, fmt.Errorf("contract: search: results[%d].hops[%d].name missing", i, j)
			}
		}
	}
	if r.Web != nil && r.Web.Status == "" {
		return r, fmt.Errorf("contract: search: web.status missing")
	}
	return r, nil
}

// ValidateGetResponse проверяет JSON ответа get.
func ValidateGetResponse(body []byte) (GetResponse, error) {
	var r GetResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return r, fmt.Errorf("contract: get: %w", err)
	}
	if err := checkVersion(r.ContractVersion); err != nil {
		return r, err
	}
	if r.ID == "" {
		return r, fmt.Errorf("contract: get: id missing")
	}
	if !IsRoot(r.Root) {
		return r, fmt.Errorf("contract: get: root %q, want facts|info", r.Root)
	}
	if !IsConfidence(r.Confidence) {
		return r, fmt.Errorf("contract: get: confidence missing")
	}
	if r.Source == "" {
		return r, fmt.Errorf("contract: get: source missing")
	}
	if r.Type == "" {
		return r, fmt.Errorf("contract: get: type missing")
	}
	if !ValidDay(r.ValidFrom) || !ValidDay(r.ValidTo) {
		return r, fmt.Errorf("contract: get: valid_from/valid_to must be YYYY-MM-DD")
	}
	return r, nil
}

// ValidateStatsResponse проверяет JSON ответа stats.
func ValidateStatsResponse(body []byte) (StatsResponse, error) {
	var r StatsResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return r, fmt.Errorf("contract: stats: %w", err)
	}
	if err := checkVersion(r.ContractVersion); err != nil {
		return r, err
	}
	if r.Total < 0 {
		return r, fmt.Errorf("contract: stats: total must be >= 0, got %d", r.Total)
	}
	if r.ByRoot == nil {
		return r, fmt.Errorf("contract: stats: by_root missing")
	}
	if r.DB == "" {
		return r, fmt.Errorf("contract: stats: db missing")
	}
	for root, n := range r.ByRoot {
		if !IsRoot(root) {
			return r, fmt.Errorf("contract: stats: by_root key %q, want facts|info", root)
		}
		if n < 0 {
			return r, fmt.Errorf("contract: stats: by_root[%q] < 0", root)
		}
	}
	return r, nil
}

// ValidateAuditResponse проверяет JSON ответа audit.
func ValidateAuditResponse(body []byte) (AuditResponse, error) {
	var r AuditResponse
	if err := json.Unmarshal(body, &r); err != nil {
		return r, fmt.Errorf("contract: audit: %w", err)
	}
	if err := checkVersion(r.ContractVersion); err != nil {
		return r, err
	}
	if r.Status != "ok" {
		return r, fmt.Errorf("contract: audit: status %q, want ok", r.Status)
	}
	if r.ByConfidence == nil {
		return r, fmt.Errorf("contract: audit: by_confidence missing")
	}
	for i, row := range r.ByConfidence {
		if !IsRoot(row.Root) {
			return r, fmt.Errorf("contract: audit: by_confidence[%d].root %q, want facts|info", i, row.Root)
		}
		if !IsConfidence(row.Confidence) {
			return r, fmt.Errorf("contract: audit: by_confidence[%d].confidence missing", i)
		}
		if row.Count < 0 {
			return r, fmt.Errorf("contract: audit: by_confidence[%d].count < 0", i)
		}
	}
	return r, nil
}
