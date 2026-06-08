// Package session binds pkg/sip1/rtp to pkg/media using pkg/sip1/sdp negotiation (no duplicate codec math).
//
// Use NewMediaLeg after sdp.Parse on the INVITE body and configuring the RTP session remote address
// from the offer. Media uses pkg/media/encoder CreateDecode/CreateEncode only.
package session
