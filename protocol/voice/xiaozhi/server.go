package xiaozhi

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/LingByte/lingllm/protocol/voice/gateway"
	"github.com/LingByte/lingllm/protocol/voice/transport"
	"github.com/gorilla/websocket"
)

// ServerConfig wires the xiaozhi adapter.
type ServerConfig struct {
	// Mode selects pipeline (ASR+dialog+TTS) or realtime (pkg/realtime).
	// Empty defaults to pipeline.
	Mode string

	SessionFactory       transport.SessionFactory
	RealtimeFactory      RealtimeAgentFactory
	DialogWSURL          string
	CallIDPrefix         string
	ResolveDevicePayload func(ctx context.Context, deviceID string) ([]byte, error)
	ConfigureClient      func(*gateway.ClientConfig)
	OnSessionStart       func(ctx context.Context, callID, deviceID string)
	OnSessionEnd         func(ctx context.Context, callID, reason string)
}

// Server accepts xiaozhi WebSocket connections.
type Server struct {
	cfg ServerConfig
	up  websocket.Upgrader
}

// NewServer validates cfg.
func NewServer(cfg ServerConfig) (*Server, error) {
	switch normalizeMode(cfg.Mode) {
	case ModeRealtime:
		if cfg.RealtimeFactory == nil {
			return nil, errors.New("xiaozhi: nil RealtimeFactory in realtime mode")
		}
	default:
		if cfg.SessionFactory == nil {
			return nil, errors.New("xiaozhi: nil SessionFactory in pipeline mode")
		}
		if strings.TrimSpace(cfg.DialogWSURL) == "" {
			return nil, errors.New("xiaozhi: empty DialogWSURL in pipeline mode")
		}
	}
	if cfg.CallIDPrefix == "" {
		cfg.CallIDPrefix = "xz"
	}
	return &Server{
		cfg: cfg,
		up: websocket.Upgrader{
			CheckOrigin:     func(_ *http.Request) bool { return true },
			ReadBufferSize:  4 * 1024,
			WriteBufferSize: 16 * 1024,
		},
	}, nil
}

// Handle upgrades the HTTP connection to the xiaozhi protocol.
func (s *Server) Handle(w http.ResponseWriter, r *http.Request) {
	conn, err := s.up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	deviceID := strings.TrimSpace(r.Header.Get("Device-Id"))
	if deviceID == "" {
		deviceID = strings.TrimSpace(r.Header.Get("device-id"))
	}
	if deviceID == "" {
		deviceID = strings.TrimSpace(r.URL.Query().Get("device-id"))
	}
	if deviceID == "" {
		deviceID = strings.TrimSpace(r.URL.Query().Get("device_id"))
	}

	callID := fmt.Sprintf("%s-%d", s.cfg.CallIDPrefix, time.Now().UnixNano())
	cfg := s.cfg

	payloadRaw := strings.TrimSpace(r.URL.Query().Get("payload"))
	if payloadRaw == "" && deviceID != "" && cfg.ResolveDevicePayload != nil {
		resolved, err := cfg.ResolveDevicePayload(r.Context(), deviceID)
		if err != nil {
			_ = conn.WriteMessage(websocket.TextMessage, MakeError("device binding failed", true))
			_ = conn.Close()
			return
		}
		payloadRaw = string(resolved)
	}
	if payloadRaw != "" {
		merged, err := gateway.MergeDialogPayloadQuery(strings.TrimSpace(cfg.DialogWSURL), []byte(payloadRaw))
		if err != nil {
			_ = conn.WriteMessage(websocket.TextMessage, MakeError("invalid payload query", true))
			_ = conn.Close()
			return
		}
		cfg.DialogWSURL = merged
	}

	sess := newSession(cfg, conn, callID, deviceID)
	sess.run(r.Context())
}
