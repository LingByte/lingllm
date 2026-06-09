package stack

import (
	"strconv"
	"strings"
)

// ParseCSeqNum extracts the numeric prefix from a CSeq header value.
// Example: "314159 INVITE" -> (314159, true). The second return is false when
// the header is empty or malformed (distinguishes failure from a valid zero).
func ParseCSeqNum(cseq string) (int, bool) {
	cseq = strings.TrimSpace(cseq)
	if cseq == "" {
		return 0, false
	}
	parts := strings.Fields(cseq)
	if len(parts) < 1 {
		return 0, false
	}
	n, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, false
	}
	return n, true
}

// WithCSeqACK builds the CSeq header value for an ACK to a 2xx INVITE response.
// The sequence number must match the original INVITE; only the method changes to ACK.
func WithCSeqACK(inviteCSeq int) string {
	if inviteCSeq <= 0 {
		return "1 ACK"
	}
	return strconv.Itoa(inviteCSeq) + " ACK"
}
