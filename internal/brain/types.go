package brain

import (
	"github.com/eSlider/2dph/internal/brain/rank"
)

// Hit is the search hit type; ranking lives in package rank so CI can test
// it without the native ladybug library.
type Hit = rank.Hit
