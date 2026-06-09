package outbound

import "testing"

func TestResolveTransport_Precedence(t *testing.T) {
	tgt := DialTarget{
		RequestURI: "sip:user@host:5060;transport=tcp",
		Transport:  TransportTLS,
	}
	if got := ResolveTransport(tgt); got != TransportTCP {
		t.Fatalf("uri param wins: got %v", got)
	}
	tgt = DialTarget{RequestURI: "sip:user@host", Transport: TransportTLS}
	if got := ResolveTransport(tgt); got != TransportTLS {
		t.Fatalf("target transport: got %v", got)
	}
	if got := ResolveTransport(DialTarget{RequestURI: "sip:user@host"}); got != TransportUDP {
		t.Fatalf("default udp: got %v", got)
	}
}

func TestTransportFromRequestURI_SIPS(t *testing.T) {
	if got := transportFromRequestURI("sips:user@host"); got != TransportTLS {
		t.Fatalf("sips scheme: %v", got)
	}
	if got := transportFromRequestURI("sips:user@host;transport=udp"); got != TransportUDP {
		t.Fatalf("explicit override: %v", got)
	}
}

func TestTransportMethods(t *testing.T) {
	if !TransportUDP.IsValid() || TransportUnset.IsValid() {
		t.Fatal("validity")
	}
	if !TransportTLS.IsTLS() || TransportTCP.IsTLS() {
		t.Fatal("tls flag")
	}
	if !TransportTCP.IsConnectionOriented() || TransportUDP.IsConnectionOriented() {
		t.Fatal("conn oriented")
	}
	if TransportUDP.ViaToken() != "SIP/2.0/UDP" {
		t.Fatal("via token")
	}
	if TransportUnset.ViaToken() != "SIP/2.0/UDP" {
		t.Fatal("unset via defaults udp")
	}
}

func TestParseTransportToken(t *testing.T) {
	if got := parseTransportToken("tcp"); got != TransportTCP {
		t.Fatalf("tcp: %v", got)
	}
	if got := parseTransportToken("bogus"); got != TransportUnset {
		t.Fatalf("bogus: %v", got)
	}
}
