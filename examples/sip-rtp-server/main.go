// SIP RTP media server — runs separately from the signaling server.
//
// Exposes an HTTP control API; the signaling server allocates legs on INVITE
// and activates them on ACK.
//
// Usage:
//
//	# Terminal A: RTP media plane
//	RTP_MEDIA_IP=192.168.28.128 RTP_CONTROL_PORT=8090 go run ./examples/sip-rtp-server
//
//	# Terminal B: SIP signaling (RTP nodes register here)
//	SIP_CONTROL_PORT=8080 go run ./examples/sip-signaling-server
//
//	# Terminal C: second RTP node on the same host (must use a different control port)
//	RTP_NODE_ID=rtp-b RTP_CONTROL_PORT=8091 RTP_MEDIA_IP=192.168.28.129 \
//	  SIP_REGISTRY_URL=http://192.168.28.128:8080 go run ./examples/sip-rtp-server
//
// On one machine, each RTP instance needs a unique RTP_CONTROL_PORT (8090, 8091, …)
// or set RTP_CONTROL_PORT=0 to let the OS pick a free port.
//
// Env:
//   - SIP_REGISTRY_URL        signaling registry base URL (required for multi-node)
//   - RTP_NODE_ID             node id (default rtp-<bound-port>)
//   - RTP_CONTROL_PUBLIC_URL  control URL advertised to signaling (default http://<media_ip>:<port>)
//   - RTP_UDP_HOST            UDP bind host (default 0.0.0.0)
//   - RTP_MEDIA_IP            IP advertised in SDP c= line (auto-detect if empty)
//   - RTP_CONTROL_HOST        HTTP bind host (default 0.0.0.0)
//   - RTP_CONTROL_PORT        HTTP control port (default 8090; 0 = auto)
//   - RTP_MAX_LEGS            max concurrent legs (0 = unlimited)
//
// Realtime AI (optional — replaces silence demo when configured):
//
//	export REALTIME_CONFIG_JSON='{"provider":"aliyun_omni","api_key":"sk-...","model":"qwen3.5-omni-flash-realtime-2026-03-15"}'
//	export REALTIME_VOICE=Cherry
//
// Volcengine:
//
//	export REALTIME_CONFIG_JSON='{"provider":"volcengine_dialogue","appId":"...","accessKey":"..."}'
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/LingByte/lingllm/examples/sip-split/controlapi"
	"github.com/LingByte/lingllm/examples/sip-split/rtppool"
	"github.com/LingByte/lingllm/protocol/sip/sdp"
	"github.com/LingByte/lingllm/protocol/sipmedia/rtp"
	"github.com/LingByte/lingllm/protocol/sipmedia/session"
	"github.com/LingByte/lingllm/protocol/voice/siprealtime"
	_ "github.com/LingByte/lingllm/realtime/aliyunomni"
	_ "github.com/LingByte/lingllm/realtime/volcdialogue"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	mediaIP := envOr("RTP_MEDIA_IP", detectOutboundIP())
	if mediaIP == "" {
		mediaIP = "127.0.0.1"
	}
	ctrlHost := envOr("RTP_CONTROL_HOST", "0.0.0.0")
	ctrlPort := envInt("RTP_CONTROL_PORT", 8090)
	maxLegs := envInt("RTP_MAX_LEGS", 0)

	listener, err := net.Listen("tcp", net.JoinHostPort(ctrlHost, strconv.Itoa(ctrlPort)))
	if err != nil {
		if ctrlPort == 8090 {
			logrus.WithError(err).Fatal("control port 8090 in use — run another instance with RTP_CONTROL_PORT=8091 (or RTP_CONTROL_PORT=0 for auto)")
		}
		logrus.WithError(err).Fatal("http listen")
	}
	actualPort := listener.Addr().(*net.TCPAddr).Port

	nodeID := strings.TrimSpace(os.Getenv("RTP_NODE_ID"))
	if nodeID == "" {
		nodeID = fmt.Sprintf("rtp-%d", actualPort)
	}

	store := newLegStore(mediaIP, nodeID, maxLegs)

	mux := http.NewServeMux()
	mux.HandleFunc(controlapi.HealthPath, store.handleHealth)
	mux.HandleFunc(controlapi.LegsPath, store.handleLegs)
	mux.HandleFunc(controlapi.LegsPath+"/", store.handleLegByID)

	srv := &http.Server{
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		logrus.WithFields(logrus.Fields{
			"control":  fmt.Sprintf("%s:%d", ctrlHost, actualPort),
			"media_ip": mediaIP,
			"node_id":  nodeID,
		}).Info("sip rtp media server listening")
		if err := srv.Serve(listener); err != nil && err != http.ErrServerClosed {
			logrus.WithError(err).Fatal("http serve")
		}
	}()

	registryURL := strings.TrimSpace(os.Getenv("SIP_REGISTRY_URL"))
	var registrar *rtppool.Registrar
	if registryURL != "" {
		controlURL := rtppool.PublicControlURL(os.Getenv("RTP_CONTROL_PUBLIC_URL"), mediaIP, actualPort)
		registrar = rtppool.NewRegistrar(&http.Client{Timeout: 5 * time.Second}, registryURL, controlapi.RegisterNodeRequest{
			NodeID:     nodeID,
			ControlURL: controlURL,
			MediaIP:    mediaIP,
		})
		registrar.LegCount = store.activeLegCount
		go func() {
			if err := registrar.Run(ctx); err != nil && ctx.Err() == nil {
				logrus.WithError(err).Error("rtp registry heartbeat stopped")
				stop()
			}
		}()
		logrus.WithFields(logrus.Fields{
			"registry":    registryURL,
			"control_url": controlURL,
		}).Info("registered with sip signaling registry")
	} else {
		logrus.Warn("SIP_REGISTRY_URL not set — node will not join signaling pool")
	}

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if registrar != nil {
		_ = registrar.Deregister(shutdownCtx)
	}
	_ = srv.Shutdown(shutdownCtx)
	store.stopAll()
	logrus.Info("sip rtp media server stopped")
}

type mediaLeg struct {
	callID    string
	cs        *session.CallSession
	rtpSess   *rtp.Session
	codec     sdp.Codec
	ai        *siprealtime.Bridge
	stopMedia chan struct{}
	mediaDone chan struct{}
	active    bool
}

type legStore struct {
	mu      sync.Mutex
	mediaIP string
	nodeID  string
	maxLegs int
	legs    map[string]*mediaLeg
}

func newLegStore(mediaIP, nodeID string, maxLegs int) *legStore {
	return &legStore{mediaIP: mediaIP, nodeID: nodeID, maxLegs: maxLegs, legs: make(map[string]*mediaLeg)}
}

func (s *legStore) activeLegCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.legs)
}

func (s *legStore) handleHealth(w http.ResponseWriter, _ *http.Request) {
	s.mu.Lock()
	n := len(s.legs)
	s.mu.Unlock()
	writeJSON(w, http.StatusOK, controlapi.HealthResponse{
		Status:     "ok",
		NodeID:     s.nodeID,
		ActiveLegs: n,
		MediaIP:    s.mediaIP,
	})
}

func (s *legStore) handleLegs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req controlapi.PrepareLegRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, controlapi.ErrorBody{Error: "bad json"})
		return
	}
	req.CallID = strings.TrimSpace(req.CallID)
	if req.CallID == "" || strings.TrimSpace(req.OfferSDP) == "" {
		writeJSON(w, http.StatusBadRequest, controlapi.ErrorBody{Error: "call_id and offer_sdp required"})
		return
	}

	resp, err := s.prepare(req.CallID, req.OfferSDP)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, controlapi.ErrorBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, resp)
}

func (s *legStore) handleLegByID(w http.ResponseWriter, r *http.Request) {
	callID := strings.TrimPrefix(r.URL.Path, controlapi.LegsPath+"/")
	callID = strings.TrimSpace(callID)
	if callID == "" {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(callID, controlapi.LegStartSuffix) {
		callID = strings.TrimSuffix(callID, controlapi.LegStartSuffix)
		callID = strings.TrimSuffix(callID, "/")
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if err := s.start(callID); err != nil {
			writeJSON(w, http.StatusNotFound, controlapi.ErrorBody{Error: err.Error()})
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"active"}`))
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if err := s.remove(callID); err != nil {
		writeJSON(w, http.StatusNotFound, controlapi.ErrorBody{Error: err.Error()})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *legStore) prepare(callID, offerBody string) (*controlapi.PrepareLegResponse, error) {
	offer, err := sdp.Parse(offerBody)
	if err != nil {
		return nil, fmt.Errorf("parse offer sdp: %w", err)
	}
	if offer.Port <= 0 || offer.IP == "" {
		return nil, fmt.Errorf("offer missing media address")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.maxLegs > 0 && len(s.legs) >= s.maxLegs {
		if _, exists := s.legs[callID]; !exists {
			return nil, fmt.Errorf("node at capacity (%d legs)", s.maxLegs)
		}
	}
	if old := s.legs[callID]; old != nil {
		s.stopLegLocked(old)
		delete(s.legs, callID)
	}

	rtpSess, err := rtp.NewSession(0)
	if err != nil {
		return nil, err
	}
	if err := session.ApplyRemoteSDP(rtpSess, offer); err != nil {
		_ = rtpSess.Close()
		return nil, err
	}
	cs, err := session.NewCallSession(callID, rtpSess, offer.Codecs)
	if err != nil {
		_ = rtpSess.Close()
		return nil, err
	}

	leg := &mediaLeg{
		callID:    callID,
		cs:        cs,
		rtpSess:   rtpSess,
		codec:     cs.NegotiatedCodec(),
		stopMedia: make(chan struct{}),
		mediaDone: make(chan struct{}),
	}
	s.legs[callID] = leg

	localPort := 0
	if la := rtpSess.LocalAddr; la != nil {
		localPort = la.Port
	}

	logrus.WithFields(logrus.Fields{
		"call_id": callID,
		"remote":  fmt.Sprintf("%s:%d", offer.IP, offer.Port),
		"local":   fmt.Sprintf("%s:%d", s.mediaIP, localPort),
		"codec":   leg.codec.Name,
	}).Info("rtp leg prepared")

	return &controlapi.PrepareLegResponse{
		CallID:    callID,
		NodeID:    s.nodeID,
		MediaIP:   s.mediaIP,
		MediaPort: localPort,
		Codec:     leg.codec.Name,
	}, nil
}

func (s *legStore) start(callID string) error {
	s.mu.Lock()
	leg := s.legs[callID]
	if leg == nil {
		s.mu.Unlock()
		return fmt.Errorf("leg not found")
	}
	if leg.active {
		s.mu.Unlock()
		return nil
	}
	leg.active = true
	s.mu.Unlock()

	if cfg, err := siprealtime.ConfigFromEnv(); err == nil {
		br, err := siprealtime.Attach(leg.cs, cfg)
		if err != nil {
			logrus.WithError(err).WithField("call_id", callID).Error("realtime attach failed")
		} else {
			leg.ai = br
			logrus.WithField("call_id", callID).Info("realtime AI enabled on rtp leg")
		}
	} else if err != siprealtime.ErrNotConfigured {
		logrus.WithError(err).Warn("realtime config invalid")
	}

	leg.cs.StartOnACK()
	go s.runMedia(leg)

	logrus.WithField("call_id", callID).Info("rtp leg active (streaming)")
	return nil
}

func (s *legStore) runMedia(leg *mediaLeg) {
	defer close(leg.mediaDone)
	stats := time.NewTicker(5 * time.Second)
	defer stats.Stop()

	logStats := func() {
		st := leg.cs.RTCPStats()
		logrus.WithFields(logrus.Fields{
			"call_id": leg.callID,
			"rx":      st.LocalPacketsRecv,
			"rtt_ms":  st.RTTMs,
			"ai":      leg.ai != nil,
		}).Info("rtp leg stats")
	}

	if leg.ai != nil {
		for {
			select {
			case <-leg.stopMedia:
				return
			case <-stats.C:
				logStats()
			}
		}
	}

	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	silence := pcmuSilenceFrame()
	pt := leg.codec.PayloadType
	if pt == 0 && !strings.EqualFold(leg.codec.Name, "pcmu") {
		pt = leg.codec.PayloadType
	}

	for {
		select {
		case <-leg.stopMedia:
			return
		case <-stats.C:
			logStats()
		case <-ticker.C:
			_ = leg.rtpSess.SendRTP(silence, pt, 160)
		}
	}
}

func (s *legStore) remove(callID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	leg := s.legs[callID]
	if leg == nil {
		return fmt.Errorf("leg not found")
	}
	s.stopLegLocked(leg)
	delete(s.legs, callID)
	logrus.WithField("call_id", callID).Info("rtp leg removed")
	return nil
}

func (s *legStore) stopLegLocked(leg *mediaLeg) {
	if leg == nil {
		return
	}
	if leg.active {
		select {
		case <-leg.stopMedia:
		default:
			close(leg.stopMedia)
		}
		<-leg.mediaDone
	}
	if leg.ai != nil {
		_ = leg.ai.Close()
		leg.ai = nil
	}
	if leg.cs != nil {
		leg.cs.Stop()
	}
}

func (s *legStore) stopAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, leg := range s.legs {
		s.stopLegLocked(leg)
		delete(s.legs, id)
	}
}

func pcmuSilenceFrame() []byte {
	// G.711 μ-law silence / comfort noise (0xFF per byte, 20 ms @ 8 kHz).
	b := make([]byte, 160)
	for i := range b {
		b[i] = 0xFF
	}
	return b
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

func detectOutboundIP() string {
	conn, err := net.Dial("udp4", "8.8.8.8:53")
	if err != nil {
		return ""
	}
	defer conn.Close()
	la, ok := conn.LocalAddr().(*net.UDPAddr)
	if !ok || la.IP == nil || la.IP.IsUnspecified() {
		return ""
	}
	return la.IP.String()
}
