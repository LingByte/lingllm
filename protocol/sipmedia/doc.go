/*
 * Copyright 2026 LingByte Authors
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

// Package sipmedia is the SIP RTP/audio plane, separate from pure signaling (protocol/sip).
//
// Layering:
//
//	protocol/sipmedia/rtp            RTP/RTCP, SRTP SDES, DTLS-SRTP
//	protocol/sipmedia/session        CallSession (RTP ↔ media.MediaSession)
//	protocol/sipmedia/bridge         Two-leg PCM transcode or G.711 RTP relay
//	protocol/sipmedia/codecreg       SDP offer codec negotiation → media.CodecConfig
//	protocol/sipmedia/dtmf           RFC 2833 + SIP INFO DTMF processors
//	protocol/sipmedia/transferbridge Media bridge after protocol/sip/transfer dial
//
// Wire with protocol/sip/gateway or protocol/sip/outbound for signaling;
// attach protocol/voice on session.CallSession for ASR/TTS/LLM.
package sipmedia
