// Package stack is the lowest SIP/2.0 signaling layer in lingllm.
//
// It owns wire-format messages and UDP I/O. Higher packages build on it:
//
//	protocol/sip/transaction  — RFC 3261 client/server transaction state machines
//	protocol/sip/dialog       — Call-ID / tag dialog registry
//	protocol/sip/uas          — method handlers wired to stack.Endpoint
//	protocol/sip/gateway      — combined UAS listen socket + optional TCP/TLS
//
// # SIP message shape (RFC 3261)
//
// Every SIP message is ASCII text with CRLF line endings:
//
//	start-line CRLF
//	*(message-header CRLF)
//	CRLF
//	[message-body]
//
// A request start-line has three tokens:
//
//	METHOD Request-URI SIP-Version
//
// Example INVITE (headers abbreviated):
//
//	INVITE sip:alice@example.com SIP/2.0
//	Via: SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK776asdhds
//	Max-Forwards: 70
//	From: Bob <sip:bob@example.com>;tag=a6c85cf
//	To: Alice <sip:alice@example.com>
//	Call-ID: a84b4c76e66710@192.0.2.1
//	CSeq: 314159 INVITE
//	Contact: <sip:bob@192.0.2.1>
//	Content-Type: application/sdp
//	Content-Length: 142
//
//	v=0
//	o=bob 2890844526 2890844526 IN IP4 192.0.2.1
//	...
//
// A response start-line has three tokens:
//
//	SIP-Version Status-Code Reason-Phrase
//
// Example 200 OK to the INVITE above:
//
//	SIP/2.0 200 OK
//	Via: SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK776asdhds
//	From: Bob <sip:bob@example.com>;tag=a6c85cf
//	To: Alice <sip:alice@example.com>;tag=1928301774
//	Call-ID: a84b4c76e66710@192.0.2.1
//	CSeq: 314159 INVITE
//	Contact: <sip:alice@pc33.example.com>
//	Content-Type: application/sdp
//	Content-Length: 131
//
//	v=0
//	...
//
// Key headers used throughout the stack:
//
//   - Via:        routing; each hop adds a Via with a unique branch parameter
//   - Call-ID:    dialog identifier (must be globally unique)
//   - CSeq:       "<number> <METHOD>" — pairs requests with responses
//   - From / To:  dialog parties; To gains a ;tag= on the first provisional/final response
//   - Contact:    direct URI for subsequent in-dialog requests
//   - Content-Length: byte length of the body (mandatory when a body is present on TCP/TLS)
//
// # What this package provides
//
//   - Message — parsed request/response with header maps and body
//   - Parse / Message.String — text serialization (CRLF)
//   - ReadMessage — stream-oriented read using Content-Length (for TCP/TLS gateways)
//   - Endpoint — UDP listen loop: parse datagram → dispatch by method → optional auto-reply
//   - DatagramTransport / UDPTransport — thin UDP adapter
//   - Method name constants and small helpers (ParseCSeqNum, ParseRAck, …)
//
// # What this package deliberately does NOT do
//
//   - Transaction timers, retransmission, or CANCEL matching (see transaction package)
//   - Digest authentication, Record-Route routing, or dialog state
//   - TCP/TLS listeners (gateway package; it reuses Message + ReadMessage)
//   - RTP or codec handling (see protocol/sipmedia)
//
// # UDP Endpoint lifecycle
//
//	ep := stack.NewEndpoint(stack.EndpointConfig{Host: "0.0.0.0", Port: 5060})
//	ep.RegisterHandler(stack.MethodInvite, myInviteHandler)
//	ep.Open()
//	go ep.Serve(ctx)   // blocks until ctx cancelled, Close(), or fatal read error
//
// Handlers return a response Message; Endpoint sends it on the same UDP socket.
// Responses received on the socket are forwarded to EndpointConfig.OnSIPResponse
// (used by the outbound Manager and transaction layer).
//
// # Known limitations
//
//   - Malformed header lines without ":" are still skipped rather than rejected.
//   - Very large SIP bodies over UDP may exceed datagram MTU (no automatic fragmentation).
//   - Set EndpointConfig.SyncHandlers to force synchronous dispatch (default is async).
package stack
