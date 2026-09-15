package docbody

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/eSlider/2dph/internal/docgraph"
	"github.com/eslider/go-onlyoffice"
)

// Env vars for the document-body path (no secrets, no host URLs; OnlyOffice
// creds come from the standard ONLYOFFICE_* variables).
const (
	EnvEnable     = "GATOR_DOC_TEXT"       // truthy → extract PDF body text
	EnvLimit      = "GATOR_DOC_TEXT_LIMIT" // max body bytes (default docgraph.DefaultBodyLimit)
	EnvRootID     = "GATOR_DOC_ROOT_ID"    // OnlyOffice folder id rooting oo_path
	EnvRootPrefix = "GATOR_DOC_ROOT_PREFIX"
)

// Enabled reports whether body extraction is requested: the flag wins, else
// the env var GATOR_DOC_TEXT. Truthy values: 1/true/yes/on (case-insensitive).
func Enabled(flag bool) bool {
	if flag {
		return true
	}
	return truthy(os.Getenv(EnvEnable))
}

// LimitFromEnv returns the body byte cap: GATOR_DOC_TEXT_LIMIT when positive,
// else docgraph.DefaultBodyLimit.
func LimitFromEnv() int {
	if n, err := strconv.Atoi(strings.TrimSpace(os.Getenv(EnvLimit))); err == nil && n > 0 {
		return n
	}
	return docgraph.DefaultBodyLimit
}

// FromEnv builds the live Provider from environment: OnlyOffice credentials
// (ONLYOFFICE_URL/USER/PASS), GATOR_DOC_ROOT_ID (required) and optional
// GATOR_DOC_ROOT_PREFIX (default "external"). It fails fast when creds/root
// are missing so a misconfigured run does not silently stay metadata-only.
func FromEnv() (Provider, error) {
	creds := onlyoffice.GetEnvironmentCredentials()
	if creds.Url == "" || creds.User == "" || creds.Password == "" {
		return Provider{}, fmt.Errorf("docbody: OnlyOffice credentials required (ONLYOFFICE_URL/USER/PASS)")
	}
	root := strings.TrimSpace(os.Getenv(EnvRootID))
	if root == "" {
		return Provider{}, fmt.Errorf("docbody: %s (OnlyOffice folder id) is required", EnvRootID)
	}
	prefix := strings.TrimSpace(os.Getenv(EnvRootPrefix))
	if prefix == "" {
		prefix = "external"
	}
	return Provider{
		Fetch:   OnlyOfficeFetcher{Client: onlyoffice.NewClient(creds), RootID: root, Prefix: prefix},
		Extract: PDFExtractor{},
	}, nil
}

// truthy parses a common boolean env value.
func truthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
