package outbound

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/LingByte/lingllm/protocol/sip/stack"
)

func previewBody(s string, max int) string {
	s = strings.TrimSpace(s)
	if s == "" || max <= 0 {
		return ""
	}
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}

func udpAddrString(a interface{ String() string }) string {
	if a == nil {
		return ""
	}
	return a.String()
}

func randomHex(nBytes int) string {
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return fmt.Sprintf("%d", nBytes)
	}
	return hex.EncodeToString(b)
}

func inviteTxKey(branch string, cseq int) string {
	branch = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(branch), "z9hG4bK"))
	if branch == "" || cseq <= 0 {
		return ""
	}
	return fmt.Sprintf("%s|%d", branch, cseq)
}

func txKeyFromResponse(resp *stack.Message) string {
	if resp == nil {
		return ""
	}
	cseqNum, ok := stack.ParseCSeqNum(strings.TrimSpace(resp.GetHeader(stack.HeaderCSeq)))
	if !ok || cseqNum <= 0 {
		return ""
	}
	via := strings.TrimSpace(resp.GetHeader(stack.HeaderVia))
	if via == "" {
		return ""
	}
	lower := strings.ToLower(via)
	idx := strings.Index(lower, "branch=")
	if idx < 0 {
		return ""
	}
	val := via[idx+len("branch="):]
	if semi := strings.Index(val, ";"); semi >= 0 {
		val = val[:semi]
	}
	val = strings.TrimSpace(strings.Trim(val, "\""))
	return inviteTxKey(val, cseqNum)
}

func callIDLocalPart(callID string) (local string, ok bool) {
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return "", false
	}
	i := strings.LastIndex(callID, "@")
	if i <= 0 || i >= len(callID)-1 {
		return "", false
	}
	return callID[:i], true
}
