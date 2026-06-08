package outbound

import (
	"strings"
	"testing"
)

func TestSipFormatDisplayName_ASCII(t *testing.T) {
	got := sipFormatDisplayName(`Acme "Corp"`)
	if !strings.Contains(got, `"Acme \"Corp\""`) {
		t.Fatalf("quoted ascii: %q", got)
	}
}

func TestSipFormatDisplayName_UTF8(t *testing.T) {
	got := sipFormatDisplayName("牛牛科技")
	if !strings.HasPrefix(got, "=?UTF-8?B?") {
		t.Fatalf("mime encoded-word: %q", got)
	}
}

func TestFormatOutboundFromHeader(t *testing.T) {
	got := formatOutboundFromHeader("Alice", "1001", "10.0.0.1", 5060, "tag1")
	if !strings.Contains(got, "tag=tag1") || !strings.Contains(got, "sip:1001@10.0.0.1:5060") {
		t.Fatalf("from: %q", got)
	}
}

func TestSanitizeSIPUser(t *testing.T) {
	if sanitizeSIPUser("") != "soulnexus" {
		t.Fatal("empty default")
	}
	if sanitizeSIPUser("a@b!c") != "a_b_c" {
		t.Fatalf("sanitize: %q", sanitizeSIPUser("a@b!c"))
	}
}

func TestFormatVia(t *testing.T) {
	got := formatVia(TransportTCP, "1.2.3.4", 5060, "branch1")
	if !strings.Contains(got, "SIP/2.0/TCP") || !strings.Contains(got, "z9hG4bKbranch1") {
		t.Fatalf("via: %q", got)
	}
}
