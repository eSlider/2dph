package reasoner

import "os"

// LoadEnv resolves the reasoner client Config from process environment. This
// is the single config accessor for the bake-off; it replaces the ad-hoc env
// reads in the CLI and feeds New. Full typed-config wiring (internal/config,
// #90) is not yet on this branch base.
func LoadEnv() Config {
	return Config{
		BaseURL: envDefault("REASONER_BASE_URL", DefaultBaseURL),
		Model:   envDefault("REASONER_MODEL", OllamaRAM),
		Device:  envDefault("REASONER_DEVICE", "cpu"),
	}
}

func envDefault(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}
