package docbody

import (
	"testing"

	"github.com/eSlider/2dph/internal/docgraph"
)

func TestEnabled(t *testing.T) {
	t.Setenv(EnvEnable, "")
	if Enabled(false) {
		t.Fatal("must be disabled by default without flag")
	}
	if !Enabled(true) {
		t.Fatal("flag must enable")
	}
	for _, v := range []string{"1", "true", "TRUE", "yes", "on"} {
		t.Setenv(EnvEnable, v)
		if !Enabled(false) {
			t.Errorf("GATOR_DOC_TEXT=%q must enable", v)
		}
	}
	for _, v := range []string{"0", "false", "no", "off", ""} {
		t.Setenv(EnvEnable, v)
		if Enabled(false) {
			t.Errorf("GATOR_DOC_TEXT=%q must not enable", v)
		}
	}
}

func TestLimitFromEnv(t *testing.T) {
	t.Setenv(EnvLimit, "")
	if got := LimitFromEnv(); got != docgraph.DefaultBodyLimit {
		t.Fatalf("default limit = %d", got)
	}
	t.Setenv(EnvLimit, "4096")
	if got := LimitFromEnv(); got != 4096 {
		t.Fatalf("limit = %d", got)
	}
	t.Setenv(EnvLimit, "nope")
	if got := LimitFromEnv(); got != docgraph.DefaultBodyLimit {
		t.Fatalf("invalid limit = %d", got)
	}
}
