package brain

import (
	"os"

	"github.com/eSlider/2dph/internal/brain/rank"
)

func eps() string { return os.Getenv("KBTEST_EPS") }

// Hit is the search hit type; ranking lives in package rank so CI can test
// it without the native ladybug library.
type Hit = rank.Hit
