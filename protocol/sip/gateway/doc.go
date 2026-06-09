// Package gateway wires stack + transaction + uas (+ optional outbound) into a runnable SIP server.
//
// # UDP UAS (gateway.UAS)
//
//  Open()
//    → stack.NewEndpoint (async handlers, OnSIPResponse → outbound + transaction)
//    → uas.Handlers.AttachWithTransaction (INVITE/BYE/CANCEL/ACK + server tx)
//  Serve(ctx)
//    → read loop: parse → dispatch → send response → OnResponseSent → BeginInviteServer
//
// # Bidirectional endpoint (gateway.Endpoint)
//
// Same UDP socket for inbound UAS and outbound UAC:
//
//  ep := gateway.NewEndpoint(gateway.EndpointConfig{...})
//  ep.Open()   // UAS listen + outbound.BindSender(sharedSender)
//  ep.Dial()   // outbound INVITE; responses hit OnSIPResponse → outbound.HandleSIPResponse
//
// # TCP/TLS (gateway.StartTCPListeners)
//
// Accepts connections, reads stack.ReadMessage frames, calls ep.DispatchRequest, writes
// responses on the TCP conn, then ep.NotifyResponseDelivered (same OnResponseSent hooks as UDP).
//
// # SDP helpers (invite.go)
//
//   - Ringing / InviteAnswer — build 180/200 with dialog tags, SDP answer,
//     and Contact on the actual signaling port (UAS.SIPPort())
//   - PickCodec — choose codec from offer
//
// Pure signaling only; attach protocol/sipmedia/session for RTP/voice.
package gateway
