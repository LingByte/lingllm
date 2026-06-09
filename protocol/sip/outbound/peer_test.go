package outbound

import (
	"bufio"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func startEchoSIPServer(t *testing.T, tlsCfg *tls.Config) (addr *net.TCPAddr, gotReq <-chan *stack.Message, stop func()) {
	t.Helper()
	var ln net.Listener
	var err error
	if tlsCfg != nil {
		ln, err = tls.Listen("tcp", "127.0.0.1:0", tlsCfg)
	} else {
		ln, err = net.Listen("tcp", "127.0.0.1:0")
	}
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr = ln.Addr().(*net.TCPAddr)
	reqCh := make(chan *stack.Message, 4)
	done := make(chan struct{})

	go func() {
		defer close(done)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
		br := bufio.NewReader(conn)
		msg, err := stack.ReadMessage(br)
		if err != nil {
			return
		}
		select {
		case reqCh <- msg:
		default:
		}
		resp := &stack.Message{
			IsRequest: false, Version: stack.SIPVersion,
			StatusCode: 200, StatusText: "OK",
		}
		resp.SetHeader(stack.HeaderVia, msg.GetHeader(stack.HeaderVia))
		resp.SetHeader(stack.HeaderFrom, msg.GetHeader(stack.HeaderFrom))
		resp.SetHeader(stack.HeaderTo, msg.GetHeader(stack.HeaderTo)+";tag=srv")
		resp.SetHeader(stack.HeaderCallID, msg.GetHeader(stack.HeaderCallID))
		resp.SetHeader(stack.HeaderCSeq, msg.GetHeader(stack.HeaderCSeq))
		resp.SetHeader(stack.HeaderContentLength, "0")
		_, _ = conn.Write([]byte(resp.String()))
	}()

	stop = func() {
		_ = ln.Close()
		<-done
	}
	return addr, reqCh, stop
}

func minimalSIPInvite() *stack.Message {
	m := &stack.Message{
		IsRequest: true, Method: stack.MethodInvite,
		RequestURI: "sip:bob@127.0.0.1", Version: stack.SIPVersion,
	}
	m.SetHeader(stack.HeaderVia, "SIP/2.0/TCP 127.0.0.1:6050;branch=z9hG4bKtest;rport")
	m.SetHeader(stack.HeaderFrom, "<sip:alice@127.0.0.1>;tag=fr")
	m.SetHeader(stack.HeaderTo, "<sip:bob@127.0.0.1>")
	m.SetHeader(stack.HeaderCallID, "test-call-id@127.0.0.1")
	m.SetHeader(stack.HeaderCSeq, "1 INVITE")
	m.SetHeader(stack.HeaderMaxForwards, stack.DefaultMaxForwards)
	m.SetHeader(stack.HeaderContentLength, "0")
	return m
}

func TestUDPPeer_Send(t *testing.T) {
	dst := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060}
	var got bool
	p := newUDPPeer(func(*stack.Message, *net.UDPAddr) error { got = true; return nil }, dst)
	if err := p.Send(minimalSIPInvite()); err != nil {
		t.Fatal(err)
	}
	if !got || p.Transport() != TransportUDP || p.Remote() != dst {
		t.Fatalf("udp peer state")
	}
	if p.Close() != nil {
		t.Fatal("close")
	}
}

func TestConnPeer_TCP_SendAndReceiveResponse(t *testing.T) {
	addr, gotReq, stop := startEchoSIPServer(t, nil)
	defer stop()

	var respCount int32
	var receivedCallID atomic.Value
	sink := func(resp *stack.Message, _ *net.UDPAddr) {
		atomic.AddInt32(&respCount, 1)
		receivedCallID.Store(strings.TrimSpace(resp.GetHeader(stack.HeaderCallID)))
	}

	peer, err := dialConnPeer(TransportTCP, addr.IP.String(), addr.Port, nil, sink, nil, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer peer.Close()

	if err := peer.Send(minimalSIPInvite()); err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case <-gotReq:
	case <-time.After(2 * time.Second):
		t.Fatal("server didn't receive request")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&respCount) > 0 {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if atomic.LoadInt32(&respCount) != 1 {
		t.Fatalf("expected 1 response")
	}
	if cid, _ := receivedCallID.Load().(string); cid != "test-call-id@127.0.0.1" {
		t.Errorf("call-id %q", cid)
	}
}

func TestConnPeer_TLS_HandshakeAndSend(t *testing.T) {
	cert, serverCfg := selfSignedTLSConfig(t)
	addr, gotReq, stop := startEchoSIPServer(t, serverCfg)
	defer stop()

	pool := x509.NewCertPool()
	pool.AddCert(cert)
	clientCfg := &tls.Config{RootCAs: pool, ServerName: "127.0.0.1"}

	var respCount int32
	sink := func(*stack.Message, *net.UDPAddr) { atomic.AddInt32(&respCount, 1) }

	peer, err := dialConnPeer(TransportTLS, addr.IP.String(), addr.Port, clientCfg, sink, nil, 3*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer peer.Close()

	if err := peer.Send(minimalSIPInvite()); err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case <-gotReq:
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) && atomic.LoadInt32(&respCount) == 0 {
		time.Sleep(20 * time.Millisecond)
	}
	if atomic.LoadInt32(&respCount) != 1 {
		t.Fatalf("expected TLS response")
	}
}

func TestConnPeer_SendAfterClose(t *testing.T) {
	addr, _, stop := startEchoSIPServer(t, nil)
	defer stop()
	peer, err := dialConnPeer(TransportTCP, addr.IP.String(), addr.Port, nil, func(*stack.Message, *net.UDPAddr) {}, nil, 2*time.Second)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	_ = peer.Close()
	if err := peer.Send(minimalSIPInvite()); err == nil {
		t.Fatal("send after close")
	}
}

func TestRemoteAsUDPAddr(t *testing.T) {
	tcp := &net.TCPAddr{IP: net.ParseIP("1.2.3.4"), Port: 5060}
	got := remoteAsUDPAddr(tcp)
	if got == nil || got.Port != 5060 || !got.IP.Equal(net.ParseIP("1.2.3.4")) {
		t.Errorf("conversion: %v", got)
	}
	udp := &net.UDPAddr{IP: net.ParseIP("9.9.9.9"), Port: 1}
	if remoteAsUDPAddr(udp) != udp {
		t.Fatal("udp passthrough")
	}
	if remoteAsUDPAddr(nil) != nil {
		t.Error("nil")
	}
}

func TestSendOnPeer_FallbackUDP(t *testing.T) {
	var sent bool
	m := NewManager(ManagerConfig{})
	m.BindSender(mockSenderFunc(func(*stack.Message, *net.UDPAddr) error { sent = true; return nil }))
	leg := &outLeg{
		m:   m,
		dst: &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060},
	}
	if err := leg.sendOnPeer(minimalSIPInvite(), nil); err != nil {
		t.Fatal(err)
	}
	if !sent {
		t.Fatal("fallback send")
	}
}

func TestIsClosedConnErr(t *testing.T) {
	if isClosedConnErr(nil) {
		t.Fatal("nil err")
	}
	if !isClosedConnErr(fmt.Errorf("use of closed network connection")) {
		t.Fatal("closed")
	}
}

func selfSignedTLSConfig(t *testing.T) (*x509.Certificate, *tls.Config) {
	t.Helper()
	priv, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "lingbyte-test"},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		DNSNames:     []string{"localhost"},
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &priv.PublicKey, priv)
	if err != nil {
		t.Fatal(err)
	}
	leaf, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	keyBytes, err := x509.MarshalECPrivateKey(priv)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyBytes})
	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}
	return leaf, &tls.Config{Certificates: []tls.Certificate{tlsCert}}
}
