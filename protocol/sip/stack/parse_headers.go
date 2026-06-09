package stack

import (
	"fmt"
	"strconv"
	"strings"
)

// unfoldHeaderLines merges RFC 3261 §7.3.1 continuation lines (starting with SP/HTAB)
// into single logical header lines.
func unfoldHeaderLines(lines []string) []string {
	if len(lines) == 0 {
		return nil
	}
	out := make([]string, 0, len(lines))
	for _, line := range lines {
		if line == "" {
			continue
		}
		if len(line) > 0 && (line[0] == ' ' || line[0] == '\t') {
			if len(out) == 0 {
				continue
			}
			trimmed := strings.TrimSpace(line)
			if trimmed == "" {
				continue
			}
			out[len(out)-1] += " " + trimmed
			continue
		}
		out = append(out, line)
	}
	return out
}

// parseContentLengthValues returns every Content-Length value from unfolded header lines.
func parseContentLengthValues(headerLines []string) ([]int, error) {
	var values []int
	for _, line := range headerLines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		if canonicalHeaderKey(parts[0]) != canonicalHeaderKey(HeaderContentLength) {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err != nil || n < 0 {
			return nil, fmt.Errorf("%s: invalid Content-Length %q", errPrefix, strings.TrimSpace(parts[1]))
		}
		values = append(values, n)
	}
	return values, nil
}

// contentLengthFromHeaders picks a single Content-Length, erroring on conflicting duplicates.
func contentLengthFromHeaders(headerLines []string) (int, bool, error) {
	values, err := parseContentLengthValues(headerLines)
	if err != nil {
		return 0, false, err
	}
	if len(values) == 0 {
		return 0, false, nil
	}
	first := values[0]
	for _, v := range values[1:] {
		if v != first {
			return 0, false, fmt.Errorf("%s: conflicting Content-Length values", errPrefix)
		}
	}
	return first, true, nil
}

func applyHeadersToMessage(msg *Message, headerLines []string) {
	for _, line := range headerLines {
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])
		if key == "" {
			continue
		}
		msg.AddHeader(key, val)
	}
}

func trimBodyToContentLength(body string, contentLen int) string {
	normalized := normalizeCRLF(body)
	if contentLen <= 0 {
		return ""
	}
	b := []byte(normalized)
	if len(b) <= contentLen {
		return normalized
	}
	return string(b[:contentLen])
}
