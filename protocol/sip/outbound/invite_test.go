package outbound

import (
	"strings"
	"testing"

	"github.com/LingByte/lingllm/protocol/sip/stack"
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

func TestFormatToHeaderAndHelpers(t *testing.T) {
	if formatToHeader("") != "<sip:invalid@invalid>" {
		t.Fatal("empty to")
	}
	if formatToHeader("bob@example") != "<sip:bob@example>" {
		t.Fatal("prefix sip")
	}
	if nonEmpty("", "def") != "def" || nonEmpty("x", "def") != "x" {
		t.Fatal("nonEmpty")
	}
	if nonZero(0, 9) != 9 || nonZero(3, 9) != 3 {
		t.Fatal("nonZero")
	}
	if !strings.Contains(newCallID(""), "@127.0.0.1") {
		t.Fatal("newCallID default host")
	}
}

func TestFormatVia(t *testing.T) {
	got := formatVia(TransportTCP, "1.2.3.4", 5060, "branch1")
	if !strings.Contains(got, "SIP/2.0/TCP") || !strings.Contains(got, "z9hG4bKbranch1") {
		t.Fatalf("via: %q", got)
	}
}

func TestBuildINVITE_PAIAndPrivacy(t *testing.T) {
	p := inviteParams{
		LocalIP: "127.0.0.1", SIPHost: "127.0.0.1", SIPPort: 5060,
		RequestURI: "sip:bob@example.com", CallID: "test@127.0.0.1",
		FromTag: "abc", Branch: "branch1", CSeq: 1, LocalRTPPort: 10000,
		SDPBody: "v=0\r\n", FromUser: "alice",
		AssertedIdentityURI:         "sip:+8613800138000@trust.example",
		AssertedIdentityDisplayName: "Customer Service",
		PrivacyTokens:               []string{"id"},
	}
	msg := buildINVITE(p)
	want := `"Customer Service" <sip:+8613800138000@trust.example>`
	if got := msg.GetHeader(stack.HeaderPAssertedIdentity); got != want {
		t.Fatalf("PAI = %q want %q", got, want)
	}
	if got := msg.GetHeader(stack.HeaderPrivacy); got != "id" {
		t.Fatalf("Privacy = %q", got)
	}
}

func TestBuildINVITE_OmitsEmptyPAI(t *testing.T) {
	p := inviteParams{
		LocalIP: "127.0.0.1", SIPHost: "127.0.0.1", SIPPort: 5060,
		RequestURI: "sip:bob@example.com", CallID: "c@h",
		FromTag: "t", Branch: "b", CSeq: 1, FromUser: "alice",
		AssertedIdentityURI: "   ",
	}
	msg := buildINVITE(p)
	if msg.GetHeader(stack.HeaderPAssertedIdentity) != "" {
		t.Fatal("whitespace PAI omitted")
	}
}
