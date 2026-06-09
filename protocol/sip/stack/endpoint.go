package stack

import (
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"time"
)

// HandlerFunc is a UAS-style SIP method handler.
// It receives the parsed request and the source UDP address. Returning nil means
// "do not send anything" (used when the handler will respond asynchronously
// via transaction layer or multiple provisional responses).
type HandlerFunc func(msg *Message, addr *net.UDPAddr) *Message

// EventType identifies telemetry emitted from the UDP read loop.
type EventType int

const (
	// EventDatagramReceived — raw bytes received before parse (after noise filter).
	EventDatagramReceived EventType = iota
	// EventParseError — datagram was not valid SIP text.
	EventParseError
	// EventRequestReceived — a SIP request was parsed and will be dispatched.
	EventRequestReceived
	// EventResponseReceived — a SIP response arrived (forwarded to OnSIPResponse).
	EventResponseReceived
	// EventResponseSent — a handler response was written to the socket successfully.
	EventResponseSent
)

// Event is a lightweight observation from the read loop for metrics/tracing.
type Event struct {
	Type     EventType
	Addr     *net.UDPAddr
	Raw      []byte
	Request  *Message
	Response *Message
	Err      error
}

// EndpointConfig configures a UDP signaling Endpoint.
type EndpointConfig struct {
	// Host is the local address to bind (empty or "0.0.0.0" / "::" = all interfaces).
	Host string
	// Port is the UDP port (typically 5060).
	Port int
	// Network selects the UDP socket family: "udp4", "udp6", or "udp" (dual-stack).
	// Default "udp4".
	Network string

	// ReadBufSize is the UDP receive buffer (default 65535, max single datagram size).
	ReadBufSize int
	// ReadDeadline is applied before each read so Serve can poll ctx.Done()
	// and shut down promptly (default 1s). Timeout reads are retried, not fatal.
	ReadDeadline time.Duration
	// SyncHandlers when true runs method handlers in the read loop; default is false
	// (each request is handled in its own goroutine so slow work does not block I/O).
	SyncHandlers bool

	// OnReadError is called once for non-timeout read errors before Serve returns.
	OnReadError func(err error)
	// OnDatagram receives every non-noise UDP payload before parsing (wire tap).
	OnDatagram func(raw []byte, addr *net.UDPAddr)
	// OnParseErr receives datagrams that failed Parse (malformed SIP).
	OnParseErr func(raw []byte, addr *net.UDPAddr, err error)
	// OnRequest is notified after a request is parsed, before the method handler runs.
	OnRequest func(req *Message, addr *net.UDPAddr)
	// OnResponse is notified when a handler returns a response, before UDP send.
	OnResponse func(req *Message, resp *Message, addr *net.UDPAddr)
	// OnResponseSent runs after the response bytes were written successfully.
	OnResponseSent func(req *Message, resp *Message, addr *net.UDPAddr)
	// OnSIPResponse receives every SIP response datagram (outbound transaction matching).
	OnSIPResponse func(resp *Message, addr *net.UDPAddr)
	// OnMessageSent runs after any outbound write via Endpoint.Send (UAC or UAS).
	OnMessageSent func(msg *Message, addr *net.UDPAddr)
	// OnEvent is a unified hook for metrics; see EventType constants.
	OnEvent func(e Event)
	// NoRouteHandler handles methods with no RegisterHandler entry.
	// When nil, DefaultNoRouteResponse (501 Not Implemented) is used.
	NoRouteHandler HandlerFunc
}

// Endpoint is a minimal SIP UAS over UDP: one socket, parse, dispatch by method.
//
// It does not implement transactions. For production INVITE handling, register
// handlers that delegate to protocol/sip/transaction and return nil or 100 Trying
// synchronously while the transaction layer sends further responses.
type Endpoint struct {
	cfg EndpointConfig

	mu       sync.Mutex
	handlers map[string]HandlerFunc
	tr       *UDPTransport

	wg sync.WaitGroup
}

// NewEndpoint constructs an endpoint. Call Open then Serve.
func NewEndpoint(cfg EndpointConfig) *Endpoint {
	if cfg.ReadBufSize <= 0 {
		cfg.ReadBufSize = 65535
	}
	if cfg.ReadDeadline <= 0 {
		cfg.ReadDeadline = time.Second
	}
	if strings.TrimSpace(cfg.Network) == "" {
		cfg.Network = "udp4"
	}
	if cfg.NoRouteHandler == nil {
		cfg.NoRouteHandler = defaultNoRouteHandler
	}
	return &Endpoint{
		cfg:      cfg,
		handlers: make(map[string]HandlerFunc),
	}
}

// RegisterHandler registers a SIP method handler (method is case-insensitive).
func (e *Endpoint) RegisterHandler(method string, h HandlerFunc) {
	if e == nil {
		return
	}
	method = strings.ToUpper(strings.TrimSpace(method))
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.handlers == nil {
		e.handlers = make(map[string]HandlerFunc)
	}
	e.handlers[method] = h
}

func (e *Endpoint) listenNetwork() string {
	n := strings.TrimSpace(e.cfg.Network)
	if n == "" {
		return "udp4"
	}
	return n
}

// Open binds the UDP listen socket.
func (e *Endpoint) Open() error {
	if e == nil {
		return fmt.Errorf("%s: nil endpoint", errPrefix)
	}
	host := strings.TrimSpace(e.cfg.Host)
	var ip net.IP
	if host == "" {
		switch e.listenNetwork() {
		case "udp6":
			ip = net.IPv6unspecified
		default:
			ip = net.IPv4zero
		}
	} else {
		ip = net.ParseIP(host)
	}
	addr := &net.UDPAddr{
		IP:   ip,
		Port: e.cfg.Port,
	}
	conn, err := net.ListenUDP(e.listenNetwork(), addr)
	if err != nil {
		return fmt.Errorf("%s: listen udp: %w", errPrefix, err)
	}
	e.mu.Lock()
	e.tr = NewUDPTransport(conn)
	e.mu.Unlock()
	return nil
}

// Transport returns the datagram transport after Open, or nil.
func (e *Endpoint) Transport() DatagramTransport {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	return e.tr
}

// ListenAddr returns the bound UDP address after Open, or nil.
func (e *Endpoint) ListenAddr() net.Addr {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.tr == nil || e.tr.conn == nil {
		return nil
	}
	return e.tr.conn.LocalAddr()
}

// Send writes a SIP message to addr using the bound UDP socket.
func (e *Endpoint) Send(msg *Message, addr *net.UDPAddr) error {
	if e == nil {
		return fmt.Errorf("%s: nil endpoint", errPrefix)
	}
	e.mu.Lock()
	tr := e.tr
	e.mu.Unlock()
	if tr == nil || tr.conn == nil {
		return fmt.Errorf("%s: endpoint not open", errPrefix)
	}
	if msg == nil {
		return fmt.Errorf("%s: nil message", errPrefix)
	}
	msg.PrepareForSend()
	raw := msg.String()
	_, err := tr.conn.WriteToUDP([]byte(raw), addr)
	if err == nil && e.cfg.OnMessageSent != nil {
		e.cfg.OnMessageSent(msg, addr)
	}
	return err
}

// Close closes the listen socket, waits for in-flight async handlers, and unblocks Serve.
func (e *Endpoint) Close() error {
	if e == nil {
		return nil
	}
	e.mu.Lock()
	tr := e.tr
	e.tr = nil
	e.mu.Unlock()
	var err error
	if tr != nil {
		err = tr.Close()
	}
	e.wg.Wait()
	return err
}

// AppendOnResponseSent chains fn after the existing OnResponseSent callback.
// Call before Serve starts, or only from the setup goroutine (not concurrently with the read loop).
func (e *Endpoint) AppendOnResponseSent(fn func(*Message, *Message, *net.UDPAddr)) {
	if e == nil || fn == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	prev := e.cfg.OnResponseSent
	e.cfg.OnResponseSent = func(req, resp *Message, addr *net.UDPAddr) {
		if prev != nil {
			prev(req, resp, addr)
		}
		fn(req, resp, addr)
	}
}

// Serve runs the read/dispatch loop until ctx is cancelled, Close is called, or a non-timeout
// read error occurs (then returns a wrapped error after optional OnReadError).
// It returns ctx.Err() when the context was cancelled before a read completes.
func (e *Endpoint) Serve(ctx context.Context) error {
	if e == nil {
		return fmt.Errorf("%s: nil endpoint", errPrefix)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	e.mu.Lock()
	tr := e.tr
	e.mu.Unlock()
	if tr == nil || tr.conn == nil {
		return fmt.Errorf("%s: endpoint not open", errPrefix)
	}

	buf := make([]byte, e.cfg.ReadBufSize)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		_ = tr.conn.SetReadDeadline(time.Now().Add(e.cfg.ReadDeadline))
		n, addr, err := tr.conn.ReadFromUDP(buf)
		if err != nil {
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			if e.cfg.OnReadError != nil {
				e.cfg.OnReadError(err)
			}
			return fmt.Errorf("%s: read udp: %w", errPrefix, err)
		}

		rawBytes := append([]byte(nil), buf[:n]...)
		if e.cfg.OnDatagram != nil {
			e.cfg.OnDatagram(rawBytes, addr)
		}
		if IsSignalingNoiseDatagram(rawBytes) {
			continue
		}
		if e.cfg.OnEvent != nil {
			e.cfg.OnEvent(Event{Type: EventDatagramReceived, Raw: rawBytes, Addr: addr})
		}

		raw := string(rawBytes)
		msg, err := Parse(raw)
		if err != nil {
			if e.cfg.OnParseErr != nil {
				e.cfg.OnParseErr(rawBytes, addr, err)
			}
			if e.cfg.OnEvent != nil {
				e.cfg.OnEvent(Event{Type: EventParseError, Raw: rawBytes, Addr: addr, Err: err})
			}
			continue
		}
		if msg == nil {
			continue
		}

		if !msg.IsRequest {
			if e.cfg.OnSIPResponse != nil {
				e.cfg.OnSIPResponse(msg, addr)
			}
			if e.cfg.OnEvent != nil {
				e.cfg.OnEvent(Event{Type: EventResponseReceived, Addr: addr, Raw: rawBytes, Response: msg})
			}
			continue
		}

		if e.cfg.SyncHandlers {
			e.handleRequest(msg, addr, rawBytes)
			continue
		}
		e.wg.Add(1)
		go func(req *Message, peer *net.UDPAddr, raw []byte) {
			defer e.wg.Done()
			e.handleRequest(req, peer, raw)
		}(msg, addr, rawBytes)
	}
}

func (e *Endpoint) handleRequest(msg *Message, addr *net.UDPAddr, rawBytes []byte) {
	if e.cfg.OnRequest != nil {
		e.cfg.OnRequest(msg, addr)
	}
	if e.cfg.OnEvent != nil {
		e.cfg.OnEvent(Event{Type: EventRequestReceived, Addr: addr, Raw: rawBytes, Request: msg})
	}

	method := strings.ToUpper(msg.Method)
	e.mu.Lock()
	h := e.handlers[method]
	if h == nil {
		h = e.cfg.NoRouteHandler
	}
	e.mu.Unlock()

	if h == nil {
		return
	}

	resp := h(msg, addr)
	if resp == nil {
		return
	}

	if e.cfg.OnResponse != nil {
		e.cfg.OnResponse(msg, resp, addr)
	}
	if err := e.Send(resp, addr); err != nil {
		if e.cfg.OnReadError != nil {
			e.cfg.OnReadError(fmt.Errorf("%s: send response: %w", errPrefix, err))
		}
		return
	}
	if e.cfg.OnEvent != nil {
		e.cfg.OnEvent(Event{Type: EventResponseSent, Addr: addr, Request: msg, Response: resp})
	}
	var onSent func(*Message, *Message, *net.UDPAddr)
	e.mu.Lock()
	onSent = e.cfg.OnResponseSent
	e.mu.Unlock()
	if onSent != nil {
		onSent(msg, resp, addr)
	}
}

// SetNoRouteHandler sets the fallback handler for unknown methods (same as EndpointConfig.NoRouteHandler).
func (e *Endpoint) SetNoRouteHandler(h HandlerFunc) {
	if e == nil {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if h == nil {
		h = defaultNoRouteHandler
	}
	e.cfg.NoRouteHandler = h
}

// DispatchRequest runs the registered method handler (or NoRouteHandler) without sending on UDP.
// Used by alternate transports (e.g. TCP/TLS) that write responses themselves.
func (e *Endpoint) DispatchRequest(req *Message, addr *net.UDPAddr) *Message {
	if e == nil || req == nil {
		return nil
	}
	method := strings.ToUpper(strings.TrimSpace(req.Method))
	e.mu.Lock()
	h := e.handlers[method]
	if h == nil {
		h = e.cfg.NoRouteHandler
	}
	e.mu.Unlock()
	if h == nil {
		return nil
	}
	return h(req, addr)
}

// InvokeOnSIPResponse calls the configured OnSIPResponse hook (e.g. for responses received on TCP).
func (e *Endpoint) InvokeOnSIPResponse(resp *Message, addr *net.UDPAddr) {
	if e == nil {
		return
	}
	e.mu.Lock()
	fn := e.cfg.OnSIPResponse
	e.mu.Unlock()
	if fn != nil {
		fn(resp, addr)
	}
}

// NotifyResponseDelivered runs the same post-send hooks as UDP after a response is written on
// an alternate transport (TCP/TLS). Call after the response bytes are on the wire.
func (e *Endpoint) NotifyResponseDelivered(req, resp *Message, addr *net.UDPAddr) {
	if e == nil || resp == nil {
		return
	}
	if e.cfg.OnMessageSent != nil {
		e.cfg.OnMessageSent(resp, addr)
	}
	if e.cfg.OnEvent != nil {
		e.cfg.OnEvent(Event{Type: EventResponseSent, Addr: addr, Request: req, Response: resp})
	}
	e.mu.Lock()
	onSent := e.cfg.OnResponseSent
	e.mu.Unlock()
	if onSent != nil {
		onSent(req, resp, addr)
	}
}
