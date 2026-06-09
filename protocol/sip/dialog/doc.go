// Package dialog tracks minimal SIP dialog identifiers for UAS-centric call legs.
//
// A dialog is created by an INVITE and confirmed by ACK (RFC 3261 §12). This package
// stores the fields needed to match in-dialog requests and ACKs without parsing SDP:
//
//   - Call-ID — dialog identifier (same for the whole call leg)
//   - InviteBranch — top Via branch of the creating INVITE (transaction layer key)
//   - CSeqInvite — INVITE sequence number (ACK reuses it with method ACK)
//   - RemoteTag — typically ;tag= from the INVITE From (caller)
//   - LocalTag — ;tag= added by the UAS on To in 1xx/2xx
//
// Example INVITE establishing a dialog:
//
//	INVITE sip:bob@example.com SIP/2.0
//	Via: SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK776asdhds
//	From: Alice <sip:alice@example.com>;tag=1928301774
//	To: Bob <sip:bob@example.com>
//	Call-ID: a84b4c76e66710@192.0.2.1
//	CSeq: 314159 INVITE
//
// After 200 OK, To contains the UAS tag:
//
//	To: Bob <sip:bob@example.com>;tag=a6c85cf
//
// Matching ACK:
//
//	ACK sip:bob@example.com SIP/2.0
//	Via: SIP/2.0/UDP 192.0.2.1:5060;branch=z9hG4bK776asdhds
//	From: Alice <sip:alice@example.com>;tag=1928301774
//	To: Bob <sip:bob@example.com>;tag=a6c85cf
//	Call-ID: a84b4c76e66710@192.0.2.1
//	CSeq: 314159 ACK
//
// # Usage
//
//	d, err := dialog.NewUASFromINVITE(invite)
//	d.SetLocalTagFromToHeader(resp.GetHeader(stack.HeaderTo))
//	d.Confirm()
//	if d.MatchACK(ack) { ... }
//
// Registry stores one Dialog per Call-ID for simple B2BUA/UAS apps.
//
// Media and SDP negotiation live in protocol/sipmedia/session, not here.
package dialog
