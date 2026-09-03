package facts

import "testing"

// TestCanonicalURLCrossSpelling — L-9.3 identity: один и тот же URL в разных
// написаниях (case/utm/порядок query/trailing slash) даёт один канонический
// ключ — основа merge/flag в URL-проверках (#232).
func TestCanonicalURLCrossSpelling(t *testing.T) {
	a := "HTTPS://Jobs.Example.COM:443/vacancy/42/?utm_source=gis&ref=newsletter"
	b := "https://jobs.example.com/vacancy/42?ref=jobfeed&utm_medium=email"
	want := "https://jobs.example.com/vacancy/42"

	gotA, err := CanonicalURL(a)
	if err != nil {
		t.Fatalf("CanonicalURL(%q): %v", a, err)
	}
	gotB, err := CanonicalURL(b)
	if err != nil {
		t.Fatalf("CanonicalURL(%q): %v", b, err)
	}
	if gotA != gotB {
		t.Errorf("cross-spelling mismatch: %q != %q", gotA, gotB)
	}
	if gotA != want {
		t.Errorf("CanonicalURL = %q, want %q", gotA, want)
	}
}

// TestCanonicalURLStripsTracking — трекинг-параметры (ref, utm_*, gh_jid,
// token) не влияют на ключ; остальные query-параметры сохраняются.
func TestCanonicalURLStripsTracking(t *testing.T) {
	raw := "https://example.com/company/foo/jobs/devops-7/?gh_jid=12345&utm_campaign=wave1&ref=linkedin&token=abc&remote=true"
	want := "https://example.com/company/foo/jobs/devops-7?remote=true"

	got, err := CanonicalURL(raw)
	if err != nil {
		t.Fatalf("CanonicalURL(%q): %v", raw, err)
	}
	if got != want {
		t.Errorf("CanonicalURL = %q, want %q", got, want)
	}
}

// TestCanonicalURLDeterministic — порядок query-параметров и регистр
// схемы/host не меняют канонический ключ.
func TestCanonicalURLDeterministic(t *testing.T) {
	a, err := CanonicalURL("HTTPS://Example.COM/company/x/jobs/y/?b=2&a=1")
	if err != nil {
		t.Fatalf("CanonicalURL #1: %v", err)
	}
	b, err := CanonicalURL("https://example.com/company/x/jobs/y/?a=1&b=2")
	if err != nil {
		t.Fatalf("CanonicalURL #2: %v", err)
	}
	if a != b {
		t.Errorf("not deterministic: %q != %q", a, b)
	}
	if a != "https://example.com/company/x/jobs/y?a=1&b=2" {
		t.Errorf("CanonicalURL = %q, want sorted query", a)
	}
}

// TestCanonicalURLNormalizes — default-порт и trailing slash убираются,
// фрагмент (якорь) не влияет на ресурс.
func TestCanonicalURLNormalizes(t *testing.T) {
	cases := []struct {
		raw, want string
	}{
		{"https://example.com/company/a/jobs/b/", "https://example.com/company/a/jobs/b"},
		{"https://example.com:443/company/a/jobs/b/", "https://example.com/company/a/jobs/b"},
		{"http://example.com:80/company/a/jobs/b/", "http://example.com/company/a/jobs/b"},
		{"https://example.com/company/a/jobs/b/#section", "https://example.com/company/a/jobs/b"},
		{"https://example.com/", "https://example.com"},
	}
	for _, tc := range cases {
		got, err := CanonicalURL(tc.raw)
		if err != nil {
			t.Errorf("CanonicalURL(%q): %v", tc.raw, err)
			continue
		}
		if got != tc.want {
			t.Errorf("CanonicalURL(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}

// TestCanonicalURLRejectsInvalid — относительные и не-http(s) URL — ошибка.
func TestCanonicalURLRejectsInvalid(t *testing.T) {
	for _, raw := range []string{
		"",
		"/company/acme/jobs/senior-go-42/",
		"ftp://example.com/jobs/x",
		"example.com/jobs/x",
		"javascript:void(0)",
	} {
		if _, err := CanonicalURL(raw); err == nil {
			t.Errorf("CanonicalURL(%q): want error, got nil", raw)
		}
	}
}
