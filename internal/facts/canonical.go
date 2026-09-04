package facts

import (
	"fmt"
	"net/url"
	"strings"
)

// trackingParams — query-параметры, не влияющие на ресурс: UTM-метки,
// referral, ATS-идентификатор (gh_jid), токены. Канонический URL их
// выкидывает, иначе один и тот же ресурс из разных каналов получит разные
// ключи (identity, L-9.3 #232; конвенция gator G-8.1).
func isTrackingParam(key string) bool {
	k := strings.ToLower(key)
	return k == "ref" || k == "gh_jid" || k == "token" || strings.HasPrefix(k, "utm_")
}

// CanonicalURL — детерминированная канонизация URL ресурса (вакансии и
// т.п.), локальная реализация конвенции gator G-8.1 (gator-модуль не
// импортируется): lowercase схемы/host, strip default-порта (80/443), strip
// tracking-параметров (ref/utm_*/gh_jid/token), сортировка оставшихся
// query-параметров, без фрагмента и trailing slash. Единый ключ: один и тот
// же ресурс из разных каналов/написаний даёт один результат — основа
// проверки identity (#232).
//
// Только абсолютные http(s) URL; относительные и другие схемы — ошибка.
func CanonicalURL(raw string) (string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", fmt.Errorf("canonical url %q: %w", raw, err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return "", fmt.Errorf("canonical url %q: scheme must be http(s)", raw)
	}
	if u.Host == "" {
		return "", fmt.Errorf("canonical url %q: host required", raw)
	}
	u.Scheme = strings.ToLower(u.Scheme)
	u.Host = strings.ToLower(strings.TrimSuffix(u.Host, ".")) // FQDN trailing dot
	if p := u.Port(); (u.Scheme == "http" && p == "80") || (u.Scheme == "https" && p == "443") {
		u.Host = u.Hostname()
	}
	u.Fragment = "" // якорь не влияет на ресурс
	// Query: выкинуть tracking, остальное отсортировать (url.Values.Encode
	// сортирует ключи — детерминизм независимо от порядка в raw).
	q := u.Query()
	for k := range q {
		if isTrackingParam(k) {
			q.Del(k)
		}
	}
	u.RawQuery = q.Encode()
	u.Path = strings.TrimSuffix(u.Path, "/")
	return u.String(), nil
}
