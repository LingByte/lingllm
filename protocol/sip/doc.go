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

// Package sip provides a pure SIP/2.0 signaling stack (no RTP, no AI).
//
// Layering:
//
//	protocol/sip/stack        message parse/serialize, UDP Endpoint, TCP framing (ReadMessage)
//	protocol/sip/transaction  INVITE/non-INVITE client & server transactions, CANCEL, ACK, timers
//	protocol/sip/dialog       Call-ID, tags, early/confirmed dialog registry
//	protocol/sip/uas          UAS handler registration and transaction wiring on stack.Endpoint
//	protocol/sip/sdp          SDP parse/generate (signaling bodies only)
//	protocol/sip/outbound     UAC signaling (INVITE/CANCEL/BYE, TCP/TLS pool)
//	protocol/sip/historyinfo  RFC 7044 / RFC 5806 transfer headers
//	protocol/sip/identity     RFC 3325 P-Asserted-Identity / Privacy
//	protocol/sip/session_timer RFC 4028 Session-Expires negotiation
//	protocol/sip/gateway      UDP UAS + optional UAC Endpoint on one socket; TCP/TLS listeners
//	protocol/sip/transfer     B2BUA transfer signaling (REFER/NOTIFY, retarget headers)
//	protocol/sip/stir         STIR/SHAKEN passport header helpers
//	protocol/sip/hooks        Lifecycle + recording sink interfaces (no ORM)
//	protocol/sip/signalinglog Optional logrus SIP message audit hook
//	protocol/sip/metrics      Prometheus registry + SIP-layer counters
//
// RTP, codec negotiation, media sessions, and transfer bridging:
//
//	protocol/sipmedia         (see protocol/sipmedia/doc.go)
//
// AI / voice dialog:
//
//	protocol/voice
//
// Quick start:
//
//	# Pure signaling UAS: examples/sip-uas-demo
//	# Outbound signaling: examples/sip-outbound-demo
//
// Logging uses github.com/sirupsen/logrus (StandardLogger); no bootstrap required.
package sip
