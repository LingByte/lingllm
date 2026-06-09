package stack

import (
	"bufio"
	"strings"
	"testing"
)

func TestParse_HeaderFolding(t *testing.T) {
	raw := strings.Join([]string{
		"INVITE sip:user@domain.com SIP/2.0",
		"Call-Id: abc123",
		"Subject: A long",
		"  subject line",
		"Content-Length: 0",
		"",
		"",
	}, "\r\n")
	msg, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if msg.GetHeader(HeaderSubject) != "A long subject line" {
		t.Fatalf("subject %q", msg.GetHeader(HeaderSubject))
	}
}

func TestParse_ContentLengthTrim(t *testing.T) {
	raw := "INVITE sip:a SIP/2.0\r\nContent-Length: 3\r\n\r\nhelextra"
	msg, err := Parse(raw)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Body != "hel" {
		t.Fatalf("body %q", msg.Body)
	}
}

func TestReadMessage_ConflictingContentLength(t *testing.T) {
	raw := "INVITE sip:a SIP/2.0\r\nContent-Length: 3\r\nContent-Length: 4\r\n\r\n"
	_, err := ReadMessage(bufio.NewReader(strings.NewReader(raw)))
	if err == nil {
		t.Fatal("expected conflict error")
	}
}
