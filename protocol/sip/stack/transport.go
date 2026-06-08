package stack

import (
	"context"
	"fmt"
	"net"
)

// DatagramTransport is SIP message I/O over connectionless datagrams.
// Implementations must be safe for use from one reader and concurrent writers unless documented otherwise.
type DatagramTransport interface {
	ReadFrom(ctx context.Context, buf []byte) (n int, addr *net.UDPAddr, err error)
	WriteTo(ctx context.Context, p []byte, addr *net.UDPAddr) (n int, err error)
	Close() error
	LocalAddr() net.Addr
	String() string
}

// UDPTransport adapts *net.UDPConn to DatagramTransport.
type UDPTransport struct {
	conn *net.UDPConn
}

// NewUDPTransport wraps an existing UDP connection.
func NewUDPTransport(conn *net.UDPConn) *UDPTransport {
	return &UDPTransport{conn: conn}
}

func (t *UDPTransport) String() string { return "UDPTransport" }

func (t *UDPTransport) LocalAddr() net.Addr {
	if t == nil || t.conn == nil {
		return nil
	}
	return t.conn.LocalAddr()
}

// ReadFrom reads a datagram. If ctx is cancelled, returns ctx.Err() without blocking indefinitely
// (requires SetReadDeadline on the conn by the caller, or Endpoint sets deadlines each iteration).
func (t *UDPTransport) ReadFrom(ctx context.Context, buf []byte) (int, *net.UDPAddr, error) {
	if t == nil || t.conn == nil {
		return 0, nil, fmt.Errorf("sip1/stack: udp transport not started")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return 0, nil, ctx.Err()
		default:
		}
	}
	return t.conn.ReadFromUDP(buf)
}

func (t *UDPTransport) WriteTo(ctx context.Context, p []byte, addr *net.UDPAddr) (int, error) {
	if t == nil || t.conn == nil {
		return 0, fmt.Errorf("sip1/stack: udp transport not started")
	}
	if ctx != nil {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}
	}
	return t.conn.WriteToUDP(p, addr)
}

func (t *UDPTransport) Close() error {
	if t == nil || t.conn == nil {
		return nil
	}
	return t.conn.Close()
}

// IsSignalingNoiseDatagram reports payloads that are never valid SIP but often hit a UDP
// signaling port: NAT / CRLF keepalives (e.g. "\r\n\r\n", RFC 5626 style), or whitespace-only pings.
// Parsing them yields "empty message" noise; callers may skip them silently.
func IsSignalingNoiseDatagram(b []byte) bool {
	if len(b) == 0 || len(b) > 64 {
		return false
	}
	for _, c := range b {
		if c != '\r' && c != '\n' && c != ' ' && c != '\t' {
			return false
		}
	}
	return true
}
