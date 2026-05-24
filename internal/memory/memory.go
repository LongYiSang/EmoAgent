package memory

import (
	"context"

	memorycore "github.com/longyisang/emoagent-memorycore/pkg/memorycore"
)

type Service = memorycore.Service
type Options = memorycore.Options

func Open(ctx context.Context, opts Options) (Service, error) {
	return memorycore.Open(ctx, opts)
}
