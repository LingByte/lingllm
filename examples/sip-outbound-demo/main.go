// SIP outbound (UAC) demo: dial a remote UAS, wait for 200 OK + ACK, then BYE.
//
// Usage:
//
//	# Terminal A: inbound UAS
//	go run ./examples/sip-uas-demo
//
//	# Terminal B: outbound call to the UAS (same LAN)
//	SIP_TARGET_URI="sip:1000@192.168.28.128" \
//	SIP_SIGNALING_ADDR="192.168.28.128:5060" \
//	go run ./examples/sip-outbound-demo
//
// Env:
//   - SIP_UDP_PORT        local bind port (default 5062)
//   - SIP_LOCAL_IP        advertised in SDP (auto-detected if empty)
//   - SIP_TARGET_URI      Request-URI for INVITE
//   - SIP_SIGNALING_ADDR  host:port of next hop
//   - SIP_FROM_USER       CLI user part (default lingllm)
//   - SIP_RTP_PORT        SDP media port (signaling only, default 10002)
//   - SIP_HANGUP_AFTER    seconds after 200 OK before BYE (default 5)
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/LingByte/lingllm/protocol/sip/gateway"
	"github.com/LingByte/lingllm/protocol/sip/outbound"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	port := envInt("SIP_UDP_PORT", 5062)
	localIP := envOr("SIP_LOCAL_IP", "")
	targetURI := envOr("SIP_TARGET_URI", "sip:1000@127.0.0.1")
	sigAddr := envOr("SIP_SIGNALING_ADDR", "127.0.0.1:5060")
	fromUser := envOr("SIP_FROM_USER", "lingllm")
	rtpPort := envInt("SIP_RTP_PORT", 10002)
	hangupAfter := envInt("SIP_HANGUP_AFTER", 5)

	var establishedCallID string
	establishedCh := make(chan string, 1)

	ep := gateway.NewEndpoint(gateway.EndpointConfig{
		UASConfig: gateway.UASConfig{
			Host:    "0.0.0.0",
			Port:    port,
			LocalIP: localIP,
		},
		Outbound: outbound.ManagerConfig{
			LocalIP:        localIP,
			SIPPort:        port,
			FromUser:       fromUser,
			DefaultRTPPort: rtpPort,
			OnEstablished: func(leg outbound.EstablishedLeg) {
				establishedCallID = leg.CallID
				codec := ""
				rtp := ""
				if leg.Answer != nil {
					if len(leg.Answer.Codecs) > 0 {
						codec = leg.Answer.Codecs[0].Name
					}
					rtp = fmt.Sprintf("%s:%d", leg.Answer.IP, leg.Answer.Port)
				}
				logrus.WithFields(logrus.Fields{
					"call_id": leg.CallID,
					"codec":   codec,
					"rtp":     rtp,
				}).Info("outbound: call established")
				select {
				case establishedCh <- leg.CallID:
				default:
				}
			},
			OnEvent: func(ev outbound.DialEvent) {
				if ev.State == outbound.DialEventProvisional {
					logrus.WithFields(logrus.Fields{
						"call_id": ev.CallID,
						"status":  ev.StatusCode,
						"text":    ev.StatusText,
					}).Info("outbound: ringing")
				}
				if ev.State == outbound.DialEventFailed {
					logrus.WithFields(logrus.Fields{
						"call_id": ev.CallID,
						"status":  ev.StatusCode,
						"reason":  ev.Reason,
					}).Warn("outbound: call failed")
				}
			},
		},
	})

	if err := ep.Open(); err != nil {
		logrus.WithError(err).Fatal("open endpoint")
	}
	defer ep.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		if err := ep.Serve(ctx); err != nil && ctx.Err() == nil {
			logrus.WithError(err).Error("serve stopped")
			stop()
		}
	}()

	logrus.WithFields(logrus.Fields{
		"listen":    fmt.Sprintf("udp://0.0.0.0:%d", port),
		"local_ip":  ep.LocalIP(),
		"target":    targetURI,
		"signaling": sigAddr,
	}).Info("sip outbound demo ready")

	callID, err := ep.Dial(ctx, outbound.DialRequest{
		Scenario: outbound.ScenarioManual,
		Target: outbound.DialTarget{
			RequestURI:    targetURI,
			SignalingAddr: sigAddr,
		},
		RTPPort: rtpPort,
	})
	if err != nil {
		logrus.WithError(err).Fatal("dial failed")
	}
	logrus.WithField("call_id", callID).Info("outbound: INVITE sent")

	go func() {
		select {
		case <-ctx.Done():
			return
		case id := <-establishedCh:
			time.Sleep(time.Duration(hangupAfter) * time.Second)
			if err := ep.Hangup(id); err != nil {
				logrus.WithError(err).Warn("outbound: BYE failed")
				return
			}
			logrus.WithField("call_id", id).Info("outbound: BYE sent")
			stop()
		case <-time.After(30 * time.Second):
			logrus.Warn("outbound: no answer, sending CANCEL")
			_ = ep.Cancel(callID)
			stop()
		}
	}()

	<-ctx.Done()
	_ = establishedCallID
	logrus.Info("sip outbound demo stopped")
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
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
