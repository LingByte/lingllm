// Package session binds protocol/sipmedia/rtp to media using protocol/sip/sdp negotiation (no duplicate codec math).
//
// Use NewMediaLeg after sdp.Parse on the INVITE body and configuring the RTP session remote address
// from the offer. Media uses media/encoder CreateDecode/CreateEncode only.
package session
