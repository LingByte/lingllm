// Package transfer implements B2BUA-style call transfer signaling (RFC 3515 REFER + NOTIFY).
//
// Signaling only:
//   - Inbound REFER → 202 Accepted + NOTIFY sipfrag
//   - Outbound second leg via protocol/sip/outbound.Manager.Dial
//   - RFC 7044 / RFC 5806 retarget headers (ApplyRetargetHeaders)
//
// Media bridging after transfer is in protocol/sipmedia/transferbridge.
// Business hooks (ACD, TTS brief, CRM) stay outside this package.
package transfer
