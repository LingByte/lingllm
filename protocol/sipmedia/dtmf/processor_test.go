package dtmf

import (
	"context"
	"testing"

	"github.com/LingByte/lingllm/media"
)

func TestAttachProcessor_NilSafe(t *testing.T) {
	AttachProcessor(nil, "x", func(context.Context, string) {})
	AttachProcessor(media.NewDefaultSession(), "x", nil)
}

func TestAttachLogger_NilSafe(t *testing.T) {
	AttachLogger(nil)
}
