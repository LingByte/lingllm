// Package uas registers inbound (UAS) SIP method handlers on stack.Endpoint.
//
// # Handler wiring
//
// Handlers is a struct of typed callbacks (InviteHandler, SimpleHandler, AckHandler).
// Attach registers them on stack.Endpoint; AttachWithTransaction also chains the
// transaction layer (retransmissions, CANCEL, ACK, BeginInviteServer).
//
// # Inbound INVITE flow (with transaction binding)
//
//  1. UDP datagram → stack.Endpoint parses INVITE
//  2. ChainInviteServerTx: duplicate INVITE → resend stored final (if any)
//  3. InviteHandler runs → returns 100/180/200 (or nil to suppress auto-send)
//  4. stack.Endpoint sends response on UDP
//  5. OnResponseSent → AfterResponseSentBeginServerTx → transaction.BeginInviteServer
//  6. ACK arrives → ChainAckServerTx → transaction.HandleAck (stops 2xx retransmit)
//
// CANCEL before final:
//
//  WrapHandlersWithTransaction replaces Cancel handler → HandleCancelRequest (200 to CANCEL)
//  TU must still answer INVITE with 487 (or similar).
//
// # Building responses
//
// Use NewResponse / ErrorResponse — they copy stack.CorrelationHeaders from the request
// and set stack.HeaderContentLength. Provisional responses should add To ;tag= via dialog.AppendTagAfterNameAddr.
//
// # Helpers (server_tx.go)
//
//   - ChainInviteServerTx / ChainNonInviteServerTx — absorb retransmissions
//   - AfterResponseSentBeginServerTx — register server transaction after final on wire
//   - ChainAckServerTx — match ACK to pending INVITE server tx
//   - WithOnResponseSentAppended — compose OnResponseSent hooks
//
// Media (RTP/codec) lives in protocol/sipmedia; dialog tags in protocol/sip/dialog.
package uas
