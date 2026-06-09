// SIP signaling-only server — no local RTP socket.
//
// RTP media servers register themselves via HTTP (POST /v1/nodes/register) and
// send heartbeats. On INVITE the least-loaded registered node is chosen;
// Call-ID is bound to that node for ACK/BYE.
//
// Usage:
//
//	# Terminal A: SIP signaling (registry + SIP UDP)
//	SIP_UDP_PORT=5060 SIP_LOCAL_IP=192.168.28.128 SIP_CONTROL_PORT=8080 \
//	  go run ./examples/sip-signaling-server
//
//	# Terminal B: RTP node (registers to signaling)
//	RTP_NODE_ID=rtp-a RTP_MEDIA_IP=192.168.28.128 \
//	  SIP_REGISTRY_URL=http://192.168.28.128:8080 \
//	  go run ./examples/sip-rtp-server
//
//	# Terminal C: another RTP node
//	RTP_NODE_ID=rtp-b RTP_MEDIA_IP=192.168.28.129 \
//	  SIP_REGISTRY_URL=http://192.168.28.128:8080 \
//	  go run ./examples/sip-rtp-server
//
// Env:
//   - SIP_UDP_HOST        signaling bind host (default 0.0.0.0)
//   - SIP_UDP_PORT        signaling UDP port (default 5060)
//   - SIP_LOCAL_IP        advertised Contact / Via IP (auto if empty)
//   - SIP_CONTROL_HOST    registry HTTP bind host (default 0.0.0.0)
//   - SIP_CONTROL_PORT    registry HTTP port (default 8080)
//   - RTP_NODE_TTL_SEC    drop nodes without heartbeat (default 45)
//   - RTP_CONTROL_URLS    optional static seed URLs (dev only)
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/LingByte/lingllm/examples/sip-split/rtppool"
	"github.com/LingByte/lingllm/protocol/sip/dialog"
	"github.com/LingByte/lingllm/protocol/sip/gateway"
	"github.com/LingByte/lingllm/protocol/sip/sdp"
	"github.com/LingByte/lingllm/protocol/sip/stack"
	"github.com/LingByte/lingllm/protocol/sip/transfer"
	"github.com/LingByte/lingllm/protocol/sip/uas"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	sipHost := envOr("SIP_UDP_HOST", "0.0.0.0")
	sipPort := envInt("SIP_UDP_PORT", 5060)
	localIP := envOr("SIP_LOCAL_IP", "")
	ctrlHost := envOr("SIP_CONTROL_HOST", "0.0.0.0")
	ctrlPort := envInt("SIP_CONTROL_PORT", 8080)
	nodeTTL := envInt("RTP_NODE_TTL_SEC", 45)

	seedURLs := rtppool.ParseControlURLs(os.Getenv("RTP_CONTROL_URLS"), os.Getenv("RTP_CONTROL_URL"))
	client := &http.Client{Timeout: 5 * time.Second}
	pool := rtppool.New(client, seedURLs...)
	pool.SetTTL(time.Duration(nodeTTL) * time.Second)

	registry := dialog.NewRegistry()
	dialogs := transfer.NewDialogStore()
	tags := make(map[string]string)

	var srv *gateway.UAS
	srv = gateway.NewUAS(gateway.UASConfig{
		Host:    sipHost,
		Port:    sipPort,
		LocalIP: localIP,
		Handlers: uas.Handlers{
			Invite: func(req *stack.Message, addr *net.UDPAddr) (*stack.Message, error) {
				callID := req.GetHeader(stack.HeaderCallID)
				tag := gateway.NewTag()
				tags[callID] = tag

				offer, err := sdp.Parse(req.Body)
				if err != nil {
					logrus.WithError(err).Warn("invite: bad sdp")
					return uas.ErrorResponse(req, 488, "Not Acceptable Here")
				}
				codec, ok := gateway.PickCodec(offer)
				if !ok {
					return uas.ErrorResponse(req, 488, "Not Acceptable Here")
				}

				prepared, nodeURL, err := pool.Prepare(callID, req.Body)
				if err != nil {
					logrus.WithError(err).Error("invite: rtp pool prepare failed")
					return uas.ErrorResponse(req, 503, "Service Unavailable")
				}

				if ring, err := gateway.Ringing(req, tag); err == nil && ring != nil {
					if sendErr := srv.Send(ring, addr); sendErr != nil {
						logrus.WithError(sendErr).Warn("invite: send 180")
					}
				}

				resp, dlg, err := gateway.InviteAnswer(req, prepared.MediaIP, srv.SIPPort(), prepared.MediaPort, codec, tag)
				if err != nil {
					_ = pool.Delete(callID)
					logrus.WithError(err).Error("invite: answer failed")
					return uas.ErrorResponse(req, 500, "Server Internal Error")
				}
				_ = registry.Put(dlg)
				dialogs.Remember(callID, addr, req, resp.GetHeader(stack.HeaderTo))

				logrus.WithFields(logrus.Fields{
					"call_id":   callID,
					"codec":     codec.Name,
					"remote":    addr.String(),
					"media":     fmt.Sprintf("%s:%d", prepared.MediaIP, prepared.MediaPort),
					"rtp_node":  prepared.NodeID,
					"rtp_ctrl":  nodeURL,
					"rtp_codec": prepared.Codec,
				}).Info("invite: answered 200 OK (media delegated)")
				return resp, nil
			},
			Ack: func(req *stack.Message, addr *net.UDPAddr) error {
				callID := req.GetHeader(stack.HeaderCallID)
				if d := registry.Get(callID); d != nil {
					d.Confirm()
				}
				if err := pool.Start(callID); err != nil {
					logrus.WithError(err).WithField("call_id", callID).Error("ack: rtp start failed")
					return err
				}
				logrus.WithField("call_id", callID).Info("ack: media started on bound rtp node")
				return nil
			},
			Bye: func(req *stack.Message, addr *net.UDPAddr) (*stack.Message, error) {
				callID := req.GetHeader(stack.HeaderCallID)
				registry.Delete(callID)
				dialogs.Forget(callID)
				delete(tags, callID)
				if err := pool.Delete(callID); err != nil {
					logrus.WithError(err).WithField("call_id", callID).Warn("bye: rtp delete")
				}
				logrus.WithField("call_id", callID).Info("bye: call ended")
				return uas.NewResponse(req, 200, "OK", "", "")
			},
			Register: func(req *stack.Message, addr *net.UDPAddr) (*stack.Message, error) {
				resp, err := uas.NewResponse(req, 200, "OK", "", "")
				if err != nil {
					return nil, err
				}
				resp.SetHeader(stack.HeaderExpires, "3600")
				return resp, nil
			},
		},
	})

	if err := srv.Open(); err != nil {
		logrus.WithError(err).Fatal("open sip signaling server")
	}
	defer srv.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	mux := http.NewServeMux()
	rtppool.RegisterHTTP(mux, pool)
	httpSrv := &http.Server{
		Addr:              net.JoinHostPort(ctrlHost, strconv.Itoa(ctrlPort)),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		logrus.WithField("registry", httpSrv.Addr).Info("rtp registry http listening")
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logrus.WithError(err).Error("registry http stopped")
			stop()
		}
	}()

	go func() {
		ticker := time.NewTicker(15 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n := pool.Prune(); n > 0 {
					logrus.WithField("removed", n).Warn("pruned stale rtp nodes")
				}
				logPoolNodes(pool)
			}
		}
	}()

	logrus.WithFields(logrus.Fields{
		"signaling":  fmt.Sprintf("udp://%s:%d", sipHost, sipPort),
		"registry":   fmt.Sprintf("http://%s:%d", ctrlHost, ctrlPort),
		"local_ip":   srv.LocalIP(),
		"seed_nodes": len(seedURLs),
	}).Info("sip signaling server running (Ctrl+C to stop)")

	go func() {
		if err := srv.Serve(ctx); err != nil && ctx.Err() == nil {
			logrus.WithError(err).Error("sip serve stopped")
			stop()
		}
	}()

	<-ctx.Done()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = httpSrv.Shutdown(shutdownCtx)
	time.Sleep(100 * time.Millisecond)
	logrus.Info("sip signaling server stopped")
}

func logPoolNodes(pool *rtppool.Pool) {
	for _, n := range pool.Nodes() {
		logrus.WithFields(logrus.Fields{
			"node_id":     n.ID,
			"control":     n.ControlURL,
			"media_ip":    n.MediaIP,
			"active_legs": n.ActiveLegs,
			"healthy":     n.Healthy,
			"static":      n.Static,
		}).Info("rtp pool node")
	}
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
