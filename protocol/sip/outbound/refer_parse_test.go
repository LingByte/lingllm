package outbound

import "testing"

func TestDialTargetFromReferTo_BracketedSIP(t *testing.T) {
	tgt, err := DialTargetFromReferTo(`<sip:agent@192.168.1.10:5062;user=phone>`)
	if err != nil {
		t.Fatal(err)
	}
	if tgt.RequestURI != "sip:agent@192.168.1.10:5062" {
		t.Fatalf("uri: %q", tgt.RequestURI)
	}
	if tgt.SignalingAddr != "192.168.1.10:5062" {
		t.Fatalf("addr: %q", tgt.SignalingAddr)
	}
}

func TestDialTargetFromReferTo_DefaultPort(t *testing.T) {
	tgt, err := DialTargetFromReferTo("sip:1001@10.0.0.5")
	if err != nil {
		t.Fatal(err)
	}
	if tgt.SignalingAddr != "10.0.0.5:5060" {
		t.Fatalf("addr: %q", tgt.SignalingAddr)
	}
}

func TestDialTargetFromReferTo_SIPS(t *testing.T) {
	tgt, err := DialTargetFromReferTo("sips:secure@gw.example:5061")
	if err != nil {
		t.Fatal(err)
	}
	if tgt.RequestURI != "sips:secure@gw.example:5061" {
		t.Fatalf("uri: %q", tgt.RequestURI)
	}
}

func TestDialTargetFromReferTo_Errors(t *testing.T) {
	cases := []string{"", "tel:+1234", "sip:nouser", "sip:user@", "sip:user@host:badport"}
	for _, c := range cases {
		if _, err := DialTargetFromReferTo(c); err == nil {
			t.Fatalf("expected error for %q", c)
		}
	}
}
