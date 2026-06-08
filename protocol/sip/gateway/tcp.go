package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/LingByte/lingllm/protocol/sip/stack"
	"github.com/sirupsen/logrus"
)

// TCPListenerConfig configures optional SIP-over-TCP / TLS listeners sharing UAS handlers.
type TCPListenerConfig struct {
	// TCPAddr is host:port for plain TCP (empty = disabled).
	TCPAddr string
	// TLSAddr is host:port for TLS (empty = disabled).
	TLSAddr string
	TLSCert string
	TLSKey  string
}

// StartTCPListeners starts background TCP/TLS accept loops until ctx is cancelled.
// Requests are dispatched via ep.DispatchRequest; responses use the same transaction handlers as UDP.
func StartTCPListeners(ctx context.Context, ep *stack.Endpoint, cfg TCPListenerConfig) error {
	if ep == nil {
		return fmt.Errorf("sip/gateway: nil endpoint")
	}
	if cfg.TCPAddr != "" {
		go listenTCP(ctx, cfg.TCPAddr, ep)
		logrus.WithField("addr", cfg.TCPAddr).Info("sip: tcp listener")
	}
	if cfg.TLSAddr != "" && cfg.TLSCert != "" && cfg.TLSKey != "" {
		go listenTLS(ctx, cfg.TLSAddr, cfg.TLSCert, cfg.TLSKey, ep)
		logrus.WithField("addr", cfg.TLSAddr).Info("sip: tls listener")
	}
	return nil
}

func listenTCP(ctx context.Context, addr string, ep *stack.Endpoint) {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		logrus.WithFields(logrus.Fields{"addr": addr, "error": err}).Warn("sip tcp listen failed")
		return
	}
	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()
	tcpLn, _ := ln.(*net.TCPListener)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if tcpLn != nil {
			_ = tcpLn.SetDeadline(time.Now().Add(2 * time.Second))
		}
		conn, err := ln.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			continue
		}
		go runOneTCPConn(ctx, conn, ep)
	}
}

func listenTLS(ctx context.Context, addr, certFile, keyFile string, ep *stack.Endpoint) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		logrus.WithError(err).Warn("sip tls cert load failed")
		return
	}
	plain, err := net.Listen("tcp", addr)
	if err != nil {
		logrus.WithFields(logrus.Fields{"addr": addr, "error": err}).Warn("sip tls listen failed")
		return
	}
	go func() {
		<-ctx.Done()
		_ = plain.Close()
	}()
	tcpLn, _ := plain.(*net.TCPListener)
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{cert}}
	ln := tls.NewListener(plain, tlsCfg)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if tcpLn != nil {
			_ = tcpLn.SetDeadline(time.Now().Add(2 * time.Second))
		}
		conn, err := ln.Accept()
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if ctx.Err() != nil {
				return
			}
			continue
		}
		go runOneTCPConn(ctx, conn, ep)
	}
}

func runOneTCPConn(ctx context.Context, conn net.Conn, ep *stack.Endpoint) {
	defer func() { _ = conn.Close() }()
	ra, ok := conn.RemoteAddr().(*net.TCPAddr)
	if !ok {
		return
	}
	udpAddr := &net.UDPAddr{IP: ra.IP, Port: ra.Port}
	br := bufio.NewReader(conn)
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		_ = conn.SetReadDeadline(time.Now().Add(90 * time.Second))
		msg, err := stack.ReadMessage(br)
		if err != nil {
			return
		}
		if !msg.IsRequest {
			ep.InvokeOnSIPResponse(msg, udpAddr)
			continue
		}
		resp := ep.DispatchRequest(msg, udpAddr)
		if resp != nil {
			_, _ = conn.Write([]byte(resp.String()))
		}
	}
}
