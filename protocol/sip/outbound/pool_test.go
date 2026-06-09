package outbound

import (
	"bufio"
	"context"
	"crypto/tls"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func TestSignalingPool_UDP_DoesNotPool(t *testing.T) {
	udpAddr := &net.UDPAddr{IP: net.ParseIP("127.0.0.1"), Port: 5060}
	var sent int32
	send := func(*stack.Message, *net.UDPAddr) error { atomic.AddInt32(&sent, 1); return nil }
	pool := newSignalingPool(poolConfig{UDPSend: send})
	defer pool.Close()

	p1, err := pool.Get(context.Background(), TransportUDP, udpAddr)
	if err != nil {
		t.Fatalf("Get udp: %v", err)
	}
	p2, err := pool.Get(context.Background(), TransportUDP, udpAddr)
	if err != nil {
		t.Fatalf("Get udp 2: %v", err)
	}
	if p1 == p2 {
		t.Error("UDP peers should be per-call")
	}
	if err := p1.Send(minimalSIPInvite()); err != nil {
		t.Errorf("send: %v", err)
	}
	if atomic.LoadInt32(&sent) != 1 {
		t.Errorf("sent=%d", sent)
	}
}

func TestSignalingPool_TCP_ReusesConn(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	var accepts int32
	var wg sync.WaitGroup
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			atomic.AddInt32(&accepts, 1)
			wg.Add(1)
			go func(c net.Conn) {
				defer wg.Done()
				defer c.Close()
				br := bufio.NewReader(c)
				for {
					_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
					if _, err := stack.ReadMessage(br); err != nil {
						return
					}
				}
			}(c)
		}
	}()

	addr := ln.Addr().(*net.TCPAddr)
	dst := &net.UDPAddr{IP: addr.IP, Port: addr.Port}
	pool := newSignalingPool(poolConfig{
		ResponseSink: func(*stack.Message, *net.UDPAddr) {},
	})
	defer pool.Close()

	p1, err := pool.Get(context.Background(), TransportTCP, dst)
	if err != nil {
		t.Fatalf("Get tcp 1: %v", err)
	}
	p2, err := pool.Get(context.Background(), TransportTCP, dst)
	if err != nil {
		t.Fatalf("Get tcp 2: %v", err)
	}
	if p1 != p2 {
		t.Error("must reuse TCP peer")
	}
	_ = p1.Send(minimalSIPInvite())
	time.Sleep(50 * time.Millisecond)
	if atomic.LoadInt32(&accepts) != 1 {
		t.Errorf("accepts=%d", accepts)
	}
}

func TestSignalingPool_EvictsOnPeerClose(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		c, err := ln.Accept()
		if err != nil {
			return
		}
		_ = c.Close()
	}()
	defer ln.Close()

	addr := ln.Addr().(*net.TCPAddr)
	dst := &net.UDPAddr{IP: addr.IP, Port: addr.Port}
	pool := newSignalingPool(poolConfig{
		ResponseSink: func(*stack.Message, *net.UDPAddr) {},
	})
	defer pool.Close()

	if _, err := pool.Get(context.Background(), TransportTCP, dst); err != nil {
		t.Fatalf("Get: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		pool.mu.Lock()
		n := len(pool.conns)
		pool.mu.Unlock()
		if n == 0 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("pool did not evict dead conn")
}

func TestPoolKeyAndServerName(t *testing.T) {
	dst := &net.UDPAddr{IP: net.ParseIP("203.0.113.1"), Port: 5061}
	if got := poolKey(TransportTLS, dst); got == "" {
		t.Fatal("pool key")
	}
	cfg := &tls.Config{}
	out := withServerNameIfMissing(cfg, "example.com")
	if out.ServerName != "example.com" {
		t.Fatalf("server name %q", out.ServerName)
	}
	keep := &tls.Config{ServerName: "pinned.example"}
	if got := withServerNameIfMissing(keep, "other"); got.ServerName != "pinned.example" {
		t.Fatal("preserve existing ServerName")
	}
}
