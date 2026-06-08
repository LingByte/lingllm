package dtmf

import (
	"context"

	"github.com/LingByte/lingllm/media"
	"github.com/sirupsen/logrus"
)

// Handler is called for each completed DTMF key (typically when RFC 2833 E bit is set).
type Handler func(ctx context.Context, digit string)

// AttachProcessor registers a high-priority processor that receives *media.DTMFPacket from the media session.
func AttachProcessor(ms *media.MediaSession, name string, h Handler) {
	if ms == nil || h == nil {
		return
	}
	if name == "" {
		name = "sip-dtmf"
	}
	proc := media.NewPacketProcessor(name, media.PriorityHigh,
		func(ctx context.Context, session *media.MediaSession, packet media.MediaPacket) error {
			d, ok := packet.(*media.DTMFPacket)
			if !ok || d == nil || d.Digit == "" {
				return nil
			}
			if !d.End {
				return nil
			}
			h(ctx, d.Digit)
			return nil
		})
	ms.RegisterProcessor(proc)
}

// AttachLogger registers a processor that logs DTMF digits at info level.
func AttachLogger(ms *media.MediaSession) {
	if ms == nil {
		return
	}
	AttachProcessor(ms, "sip-dtmf-log", func(ctx context.Context, digit string) {
		logrus.WithField("digit", digit).Info("sip dtmf")
	})
}
