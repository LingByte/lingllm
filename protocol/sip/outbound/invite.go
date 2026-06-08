package outbound

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/LingByte/lingllm/protocol/sip/historyinfo"
	"github.com/LingByte/lingllm/protocol/sip/identity"
	"github.com/LingByte/lingllm/protocol/sip/stack"
)

// inviteParams carries dialog fields needed for INVITE and later ACK.
type inviteParams struct {
	LocalIP         string
	SIPHost         string
	SIPPort         int
	RequestURI      string
	CallID          string
	FromTag         string
	Branch          string
	CSeq            int
	LocalRTPPort    int
	SDPBody         string
	FromUser        string // sip:FromUser@host:port
	FromDisplayName string // optional; quoted display-name in From

	// AssertedIdentityURI / AssertedIdentityDisplayName: RFC 3325 P-Asserted-Identity
	// content. Set this when the platform has carrier-verified the calling
	// number (e.g. the operator gave us a CLI we're authorised to assert).
	// Empty URI = header omitted; From header still acts as the claimed identity.
	//
	// Two-row form (sip + tel together) isn't expressible here on purpose:
	// the vast majority of carrier interop wants exactly one sip: PAI, and
	// the From header already carries the user-claimed identity. Add a
	// slice version later if a customer specifically requires both.
	AssertedIdentityURI         string
	AssertedIdentityDisplayName string
	// PrivacyTokens are RFC 3323 tokens (e.g. "id", "id;critical"). When
	// non-empty we add the Privacy header verbatim; downstream SBC honors
	// it by stripping PAI / anonymising From at trust-domain boundaries.
	// Set "id" to mark this outbound call as "show no caller ID" while
	// still carrying carrier-verified PAI inside the trust domain.
	PrivacyTokens []string

	// HistoryInfo (RFC 7044) is the call-retarget chain emitted on the
	// outbound INVITE. For B2BUA transfer legs this should contain at
	// minimum:
	//   index=1 → the original inbound Request-URI / To URI (the trunk
	//             number / DID the customer dialed)
	//   index=2 → the new target (this agent / next hop), optionally
	//             with a Reason header value identifying the retarget
	//             cause.
	// Empty chain → header omitted. Use historyinfo.AppendTransferEntry
	// to build the chain so any pre-existing entries from upstream SBCs
	// get carried forward.
	HistoryInfo []historyinfo.Entry
	// Diversion (RFC 5806) is the legacy equivalent of HistoryInfo for
	// PBX / desk-phone populations that don't parse History-Info. We
	// emit BOTH headers on transfer scenarios; downstream picks whichever
	// it understands. Empty chain → header omitted.
	Diversion []historyinfo.Diversion

	// ViaTransport drives the Via header's transport token (RFC 3261
	// §7.1: "SIP/2.0/UDP|TCP|TLS"). Empty / TransportUnset falls back
	// to UDP for backward compatibility with the original UDP-only
	// outbound path. Set in Manager.Dial via ResolveTransport(target).
	ViaTransport Transport

	// IdentityHeader is the **already-rendered** RFC 8224 Identity
	// header value (everything after "Identity: "). Empty → header
	// omitted. Populated in Manager.Dial when ManagerConfig.STIRSigner
	// is set; the actual signing happens upstream of buildINVITE to
	// keep this builder side-effect free.
	IdentityHeader string
}

// sipFormatDisplayName renders a SIP From display-name in a wire format
// every reasonable SBC / carrier will accept.
//
// Two encodings:
//
//   - 纯 ASCII (token / quoted-string)：原样 quoted-string，反斜杠转义引号/反斜杠，
//     回车换行折叠为空格。
//   - 含任意非 ASCII（中文 / emoji 等）：RFC 2047 §5 MIME encoded-word
//     `=?UTF-8?B?<base64>?=` 形式，整个 token 不再加引号。
//
// 为什么不直接把 UTF-8 quoted-string 塞进去？—— RFC 3261 §25.1 BNF 仍然
// 严格遵守 RFC 2822 的 quoted-string ASCII 范围，国内运营商 SBC（移动 /
// 联通 / 电信 NGN 网关）经常按字面执行，发现 `"牛牛科技无限公司"` 这种
// 高位字节直接 strip 整个 From display-name 或 400 Bad Request 退回。
// MIME encoded-word 是这些设备公认能透传的中文显示名编码。
func sipFormatDisplayName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if isASCIIOnly(s) {
		return sipQuotedASCIIDisplay(s)
	}
	return "=?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte(s)) + "?="
}

// isASCIIOnly 仅检测是否含非 ASCII（即 UTF-8 多字节）字符；ASCII 控制字符
// （\r\n\t 等）由 quoted-string 路径自行折叠，不必跳到 MIME 编码。
func isASCIIOnly(s string) bool {
	for _, r := range s {
		if r > 0x7E {
			return false
		}
	}
	return true
}

func sipQuotedASCIIDisplay(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\r', '\n':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// formatOutboundFromHeader builds the From header value (INVITE/ACK/BYE in-dialog).
func formatOutboundFromHeader(displayName, user, host string, port int, tag string) string {
	user = sanitizeSIPUser(user)
	host = nonEmpty(host, "127.0.0.1")
	port = nonZero(port, 6050)
	uri := fmt.Sprintf("<sip:%s@%s:%d>", user, host, port)
	dn := sipFormatDisplayName(displayName)
	if dn == "" {
		return uri + ";tag=" + tag
	}
	return dn + " " + uri + ";tag=" + tag
}

func formatOutboundContact(user, host string, port int) string {
	user = sanitizeSIPUser(user)
	host = nonEmpty(host, "127.0.0.1")
	port = nonZero(port, 6050)
	return fmt.Sprintf("<sip:%s@%s:%d>", user, host, port)
}

func sanitizeSIPUser(user string) string {
	user = strings.TrimSpace(user)
	if user == "" {
		return "soulnexus"
	}
	var b strings.Builder
	for _, r := range user {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' || r == '+' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	s := strings.Trim(strings.TrimSpace(b.String()), "._-+")
	if s == "" {
		return "soulnexus"
	}
	return s
}

// formatVia renders a Via header line using the supplied transport.
// Empty / unset transport degrades to UDP (the original outbound
// behaviour) so this helper is safe to call from every existing
// build* function without behavioural changes when ViaTransport is
// not yet populated.
func formatVia(transport Transport, host string, port int, branch string) string {
	tok := transport.ViaToken()
	if !transport.IsValid() {
		tok = TransportUDP.ViaToken()
	}
	return fmt.Sprintf("%s %s:%d;branch=z9hG4bK%s;rport",
		tok, nonEmpty(host, "127.0.0.1"), nonZero(port, 6050), branch)
}

func buildINVITE(p inviteParams) *stack.Message {
	via := formatVia(p.ViaTransport, p.SIPHost, p.SIPPort, p.Branch)

	from := formatOutboundFromHeader(p.FromDisplayName, p.FromUser, p.SIPHost, p.SIPPort, p.FromTag)
	to := formatToHeader(p.RequestURI)

	msg := &stack.Message{
		IsRequest:  true,
		Method:     stack.MethodInvite,
		RequestURI: p.RequestURI,
		Version:    "SIP/2.0",
		Body:       p.SDPBody,
	}
	msg.SetHeader("Via", via)
	msg.SetHeader("Max-Forwards", "70")
	msg.SetHeader("From", from)
	msg.SetHeader("To", to)
	msg.SetHeader("Call-ID", p.CallID)
	msg.SetHeader("CSeq", fmt.Sprintf("%d INVITE", p.CSeq))
	msg.SetHeader("Contact", formatOutboundContact(p.FromUser, p.SIPHost, p.SIPPort))
	// RFC 3325 P-Asserted-Identity: carrier-validated CLI carried separately
	// from the (user-claimed) From header. Only emitted when the caller has
	// explicitly populated AssertedIdentityURI — we do NOT auto-derive PAI
	// from FromUser because that would leak unsigned claims as if they
	// were operator-verified.
	if pai := strings.TrimSpace(p.AssertedIdentityURI); pai != "" {
		hdr := identity.Asserted{
			URI:         pai,
			DisplayName: strings.TrimSpace(p.AssertedIdentityDisplayName),
		}.FormatHeader()
		if hdr != "" {
			msg.SetHeader("P-Asserted-Identity", hdr)
		}
	}
	// RFC 3323 Privacy: token list (id / header / user / session / critical).
	// Set "id" to make a withheld-CLI call (carrier still sees PAI inside the
	// trust domain, but its egress rule strips PAI before handing to PSTN).
	if pr := identity.FormatPrivacyHeader(p.PrivacyTokens); pr != "" {
		msg.SetHeader("Privacy", pr)
	}
	// RFC 7044 History-Info / RFC 5806 Diversion: surface the retarget
	// chain on B2BUA transfer legs. We emit BOTH because the downstream
	// PBX / phone population is mixed — modern Avaya/Cisco honor
	// History-Info, older Yealink/Polycom/Asterisk read Diversion. See
	// docs/sip_gap_analysis.md §"转接架构说明" for why this matters.
	if h := historyinfo.FormatChain(p.HistoryInfo); h != "" {
		msg.SetHeader("History-Info", h)
	}
	if d := historyinfo.FormatDiversionChain(p.Diversion); d != "" {
		msg.SetHeader("Diversion", d)
	}
	// RFC 8224 Identity (SHAKEN). The header value is rendered upstream
	// by ManagerConfig.STIRSigner; empty means "don't sign this leg"
	// (signer absent or signing failed soft-fail; see outbound/stir.go).
	if id := strings.TrimSpace(p.IdentityHeader); id != "" {
		msg.SetHeader("Identity", id)
	}
	msg.SetHeader("User-Agent", "LingLLM-SIP/1.0")
	msg.SetHeader("Content-Type", "application/sdp")
	msg.SetHeader("Allow", "INVITE, ACK, BYE, CANCEL, OPTIONS, UPDATE")
	// RFC 4028 — advertise capability for session timers (Supported:
	// timer). We still don't *propose* Session-Expires in this INVITE
	// because UAC-side refreshing is opt-in based on peer policy:
	//   - If peer responds with `Session-Expires: <N>;refresher=uac`,
	//     manager.handleResponse arms `outRefresher` (see refresher.go)
	//     which sends UPDATE refreshes at N/2.
	//   - If peer responds with `refresher=uas` (or no Session-Expires),
	//     peer owns refresh and we stay passive.
	// Note: inbound mid-dialog refreshes from peer are NOT yet honored
	// (outbound connPeer drops mid-dialog requests — see peer.go:223).
	msg.SetHeader("Supported", "timer, 100rel, replaces")
	msg.SetHeader("Content-Length", strconv.Itoa(stack.BodyBytesLen(p.SDPBody)))
	return msg
}

func formatToHeader(requestURI string) string {
	u := strings.TrimSpace(requestURI)
	if u == "" {
		return "<sip:invalid@invalid>"
	}
	if !strings.HasPrefix(strings.ToLower(u), "sip:") {
		u = "sip:" + u
	}
	return "<" + u + ">"
}

func nonEmpty(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

func nonZero(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}

func newCallID(localIP string) string {
	// Host part should match Via/Contact identity (SIPHost) so carriers do not rewrite Call-ID.
	return fmt.Sprintf("%d@%s", time.Now().UnixNano(), nonEmpty(localIP, "127.0.0.1"))
}
