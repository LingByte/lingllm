// Minimal SIP UAS demo: OPTIONS, INVITE (SDP answer), ACK, BYE over UDP.
//
// Usage:
//
//	go run ./examples/sip-uas-demo
//	SIP_UDP_PORT=5060 SIP_RTP_PORT=10000 go run ./examples/sip-uas-demo
//
// Test with sip-cli or another softphone pointed at udp://127.0.0.1:5060 .
package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/LingByte/lingllm/protocol/sip/dialog"
	"github.com/LingByte/lingllm/protocol/sip/gateway"
	"github.com/LingByte/lingllm/protocol/sip/sdp"
	"github.com/LingByte/lingllm/protocol/sip/stack"
	"github.com/LingByte/lingllm/protocol/sip/uas"
	"github.com/sirupsen/logrus"
)

func main() {
	logrus.SetFormatter(&logrus.TextFormatter{FullTimestamp: true})

	port := envInt("SIP_UDP_PORT", 5060)
	rtpPort := envInt("SIP_RTP_PORT", 10000)
	host := envOr("SIP_UDP_HOST", "0.0.0.0")
	localIP := envOr("SIP_LOCAL_IP", "")

	registry := dialog.NewRegistry()
	tags := make(map[string]string) // Call-ID -> local To tag

	var srv *gateway.UAS
	srv = gateway.NewUAS(gateway.UASConfig{
		Host:    host,
		Port:    port,
		LocalIP: localIP,
		Handlers: uas.Handlers{
			Invite: func(req *stack.Message, addr *net.UDPAddr) (*stack.Message, error) {
				callID := req.GetHeader("Call-ID")
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

				if ring, err := gateway.Ringing(req, tag); err == nil && ring != nil {
					if sendErr := srv.Send(ring, addr); sendErr != nil {
						logrus.WithError(sendErr).Warn("invite: send 180")
					}
				}

				resp, dlg, err := gateway.InviteAnswer(req, srv.LocalIP(), rtpPort, codec, tag)
				if err != nil {
					logrus.WithError(err).Error("invite: answer failed")
					return uas.ErrorResponse(req, 500, "Server Internal Error")
				}
				_ = registry.Put(dlg)
				logrus.WithFields(logrus.Fields{
					"call_id": callID,
					"codec":   codec.Name,
					"remote":  addr.String(),
					"rtp":     rtpPort,
				}).Info("invite: answered 200 OK")
				return resp, nil
			},
			Ack: func(req *stack.Message, addr *net.UDPAddr) error {
				callID := req.GetHeader("Call-ID")
				if d := registry.Get(callID); d != nil {
					d.Confirm()
					logrus.WithField("call_id", callID).Info("dialog: confirmed")
				}
				return nil
			},
			Bye: func(req *stack.Message, addr *net.UDPAddr) (*stack.Message, error) {
				callID := req.GetHeader("Call-ID")
				registry.Delete(callID)
				delete(tags, callID)
				logrus.WithField("call_id", callID).Info("bye: call ended")
				return uas.NewResponse(req, 200, "OK", "", "")
			},
			Register: func(req *stack.Message, addr *net.UDPAddr) (*stack.Message, error) {
				resp, err := uas.NewResponse(req, 200, "OK", "", "")
				if err != nil {
					return nil, err
				}
				resp.SetHeader("Expires", "3600")
				return resp, nil
			},
		},
	})

	if err := srv.Open(); err != nil {
		logrus.WithError(err).Fatal("open sip uas")
	}
	defer srv.Close()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logrus.WithFields(logrus.Fields{
		"signaling": fmt.Sprintf("udp://%s:%d", host, port),
		"local_ip":  srv.LocalIP(),
		"rtp_port":  rtpPort,
	}).Info("sip uas demo running (Ctrl+C to stop)")

	go func() {
		if err := srv.Serve(ctx); err != nil && ctx.Err() == nil {
			logrus.WithError(err).Error("sip serve stopped")
			stop()
		}
	}()

	<-ctx.Done()
	time.Sleep(100 * time.Millisecond)
	logrus.Info("sip uas demo stopped")
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
