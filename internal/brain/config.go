package brain

import (
	"github.com/eSlider/2dph/internal/config"
	"github.com/eSlider/2dph/internal/websearch"
)

// activeCfg is the process-wide typed config; zero value is Defaults().
// bin/brain entrypoints call Configure with the stack-loaded config.
var activeCfg = config.Defaults()

// Configure sets the process-wide typed config used by the brain services and
// forwards the SearXNG settings to the web-search defaults.
func Configure(c *config.Config) {
	if c == nil {
		return
	}
	activeCfg = *c
	websearch.SetDefaults(c.Search.Cache, c.Search.Env)
}

// brainCfg returns the current typed config by value (callers can't mutate it).
func brainCfg() config.Config { return activeCfg }
