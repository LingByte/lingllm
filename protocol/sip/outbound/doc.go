// Package outbound implements SIP UAC (outbound) signaling without media binding.
//
// It shares the UDP socket with inbound UAS via SignalingSender (see gateway.Endpoint).
// Responses are routed through Manager.HandleSIPResponse on stack.EndpointConfig.OnSIPResponse.
//
// Supported signaling:
//   - INVITE / provisional / 200 OK + ACK
//   - CANCEL (RFC 3261 §9.1, with retransmit)
//   - BYE (in-dialog)
//   - UPDATE session-timer refresh (RFC 4028 UAC refresher)
//   - UDP / TCP / TLS transport selection and connection pooling
package outbound
