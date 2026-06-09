package transaction

import (
	"strconv"
	"strings"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

// TopVia returns the first Via header field-value (top-most in SIP message order).
func TopVia(m *stack.Message) string {
	if m == nil {
		return ""
	}
	vs := m.GetHeaders(stack.HeaderVia)
	if len(vs) == 0 {
		return ""
	}
	return strings.TrimSpace(vs[0])
}

// BranchParam extracts the branch parameter from one Via field-value (case-insensitive "branch=").
func BranchParam(viaLine string) string {
	if viaLine == "" {
		return ""
	}
	lower := strings.ToLower(viaLine)
	idx := strings.Index(lower, "branch=")
	if idx < 0 {
		return ""
	}
	v := strings.TrimSpace(viaLine[idx+len("branch="):])
	if cut := strings.IndexByte(v, ';'); cut >= 0 {
		v = v[:cut]
	}
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "\"")
	v = strings.TrimSuffix(v, "\"")
	return v
}

// TopBranch returns BranchParam(TopVia(m)).
func TopBranch(m *stack.Message) string {
	return BranchParam(TopVia(m))
}

// CSeqMethod returns the method token from a CSeq header (e.g. "314159 INVITE" → "INVITE").
// Empty string when the header is missing or malformed.
func CSeqMethod(m *stack.Message) string {
	if m == nil {
		return ""
	}
	parts := strings.Fields(strings.TrimSpace(m.GetHeader(stack.HeaderCSeq)))
	if len(parts) < 2 {
		return ""
	}
	return strings.ToUpper(parts[1])
}

// IsInviteCSeq reports whether the CSeq method is INVITE.
func IsInviteCSeq(m *stack.Message) bool {
	return CSeqMethod(m) == stack.MethodInvite
}

// IsAckCSeq reports whether the CSeq method is ACK.
func IsAckCSeq(m *stack.Message) bool {
	return CSeqMethod(m) == stack.MethodAck
}

// IsCancelCSeq reports whether the CSeq method is CANCEL.
func IsCancelCSeq(m *stack.Message) bool {
	return CSeqMethod(m) == stack.MethodCancel
}

// NonInviteServerKey builds a stable key for a non-INVITE server transaction
// (branch + Call-ID + method + CSeq number).
func NonInviteServerKey(req *stack.Message) string {
	if req == nil {
		return ""
	}
	cseq, _ := stack.ParseCSeqNum(req.GetHeader(stack.HeaderCSeq))
	return inviteClientKey(TopBranch(req), req.GetHeader(stack.HeaderCallID)) + "\x01" +
		strings.ToUpper(strings.TrimSpace(req.Method)) + "\x01" +
		strconv.Itoa(cseq)
}

func inviteClientKey(branch, callID string) string {
	return strings.TrimSpace(branch) + "\x00" + strings.TrimSpace(callID)
}

// InviteTransactionKey is the INVITE transaction map key (top Via branch + Call-ID).
func InviteTransactionKey(branch, callID string) string {
	return inviteClientKey(branch, callID)
}
