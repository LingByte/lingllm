// Package transaction implements RFC 3261 SIP transaction-layer behavior over UDP.
//
// It sits between stack.Endpoint (wire I/O) and the TU (call logic in uas/gateway/outbound).
// TCP/TLS uses the same message matching rules; retransmission timers apply only where
// the transport is unreliable (UDP).
//
// # Transaction identity
//
// SIP matches requests and responses by the top Via branch parameter plus dialog headers:
//
//   - INVITE client/server: key = Via.branch + Call-ID
//   - Non-INVITE client:    key = Via.branch + Call-ID + CSeq number
//   - Non-INVITE server:    key = Via.branch + Call-ID + method + CSeq number
//
// Example INVITE (client transaction):
//
//	INVITE sip:bob@example.com SIP/2.0
//	Via: SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK776asdhds
//	From: Alice <sip:alice@example.com>;tag=1928301774
//	To: Bob <sip:bob@example.com>
//	Call-ID: a84b4c76e66710@192.0.2.1
//	CSeq: 314159 INVITE
//
// Matching 180 Ringing (same Via branch, Call-ID, CSeq method INVITE):
//
//	SIP/2.0 180 Ringing
//	Via: SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK776asdhds
//	From: ...
//	To: ...;tag=a6c85cf
//	Call-ID: a84b4c76e66710@192.0.2.1
//	CSeq: 314159 INVITE
//
// # UAC (client) flows
//
// INVITE:
//
//	result, err := mgr.RunInviteClient(ctx, invite, remote, send, onProvisional)
//	// Wire mgr.HandleResponse from stack.Endpoint.OnSIPResponse
//	ack, _ := transaction.BuildAckForInvite(invite, result.Final, transaction.AckRequestURIFor2xx(result.Final, invite.RequestURI))
//	_ = send(ack, result.Remote)
//
// Non-INVITE (BYE, OPTIONS as UAC, …):
//
//	result, err := mgr.RunNonInviteClient(ctx, bye, remote, send)
//
// Timers (UDP):
//   - INVITE client: Timer A (retransmit INVITE, exponential to T2) until 1xx or 2xx–6xx
//   - Non-INVITE client: Timer E (same pattern, capped at T2)
//
// # UAS (server) flows
//
// INVITE before final:
//
//	_ = mgr.RegisterPendingInviteServer(invite)   // enables CANCEL matching
//	// TU sends 100 Trying / 180 / … then final 200/487 via stack.Endpoint.Send
//	// After final is on the wire: uas.AfterResponseSentBeginServerTx → BeginInviteServer
//
// Duplicate INVITE retransmissions:
//
//	mgr.HandleInviteRequest(invite, addr)  // resends stored final (uas.ChainInviteServerTx)
//
// CANCEL (same Call-ID + CSeq number as pending INVITE):
//
//	mgr.HandleCancelRequest(cancel, addr, send)  // 200 OK to CANCEL; TU still sends 487 on INVITE
//
// ACK after 2xx:
//
//	mgr.HandleAck(ack, addr)  // stops Timer G (2xx retransmissions)
//
// Non-INVITE server (OPTIONS, REGISTER, BYE as UAS):
//
//	// After final on wire: BeginNonInviteServer
//	mgr.HandleNonInviteRequest(req, addr)  // Timer J window: resend final to retransmissions
//
// Server timers (UDP):
//   - 2xx INVITE: Timer G (retransmit final until ACK)
//   - 3xx–6xx INVITE: Timer I (64×T1 absorb window, resend final on duplicate INVITE)
//   - Non-INVITE: Timer J (64×T1)
//
// # Integration with uas package
//
//	handlers.AttachWithTransaction(ep, uas.TransactionBinding{
//	    Mgr: mgr, Send: ep.Send, Ctx: srvCtx,
//	})
//
// This chains HandleInviteRequest, HandleNonInviteRequest, HandleCancelRequest,
// HandleAck, and registers BeginInviteServer/BeginNonInviteServer on OnResponseSent.
//
// # Helpers
//
//   - TopVia / TopBranch / BranchParam — parse routing keys from messages
//   - CSeqMethod / IsInviteCSeq / IsAckCSeq / IsCancelCSeq — CSeq inspection
//   - BuildAckForInvite / AckRequestURIFor2xx — UAC ACK after INVITE completes
//   - RouteHeadersForDialog — Record-Route → Route for in-dialog requests
//
// # Known limitations
//
//   - UDP only for retransmission timers (TCP relies on stack/gateway framing).
//   - INVITE client onProvisional fires at most once (first 1xx only).
//   - No forked INVITE handling (multiple 2xx from parallel branches).
//   - No full PRACK / re-INVITE transaction state machines (see session_timer / invite_rfc3262 hooks elsewhere).
//   - CANCEL matching uses Call-ID + CSeq number; Via branch is stored but not verified on CANCEL.
package transaction
