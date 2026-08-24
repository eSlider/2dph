package chat

import (
	"github.com/eSlider/2dph/pkg/utils"
)

// Root locates the 2dph project root (KB_ROOT, or walk up for var/ or .git).
func Root() string { return utils.Root() }

// Dir is var/corpus/chats under the project root.
func Dir() string {
	return Root() + "/var/corpus/chats"
}
