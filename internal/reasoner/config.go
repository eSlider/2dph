package reasoner

import (
	"strings"

	"github.com/eSlider/2dph/internal/config"
)

// active is the reasoner client config from the typed 2dph config stack. It is
// set by Configure (from internal/config, #91) and feeds the CLI defaults via
// NewCLI/LoadEnv. Zero value = defaults.
var active = FromTyped(config.Defaults())

// Configure wires the reasoner client config from the typed 2dph config. A nil
// cfg is ignored so callers can pass the stack result unconditionally.
func Configure(c *config.Config) {
	if c == nil {
		return
	}
	active = FromTyped(*c)
}

// FromTyped maps the typed Config.Reasoner section onto the client Config,
// falling back to the built-in defaults for fields left empty.
func FromTyped(c config.Config) Config {
	return Config{
		BaseURL: strDefault(c.Reasoner.BaseURL, DefaultBaseURL),
		Model:   strDefault(c.Reasoner.Model, OllamaRAM),
		Device:  strDefault(c.Reasoner.Device, "cpu"),
	}
}

// LoadEnv resolves the reasoner client Config from the typed config stack.
// Retained for callers that need the effective settings without a CLI parse.
func LoadEnv() Config {
	return active
}

func strDefault(v, def string) string {
	if strings.TrimSpace(v) == "" {
		return def
	}
	return v
}
