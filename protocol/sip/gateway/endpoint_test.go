package gateway

import (
	"testing"

	"github.com/LingByte/lingllm/protocol/sip/outbound"
)

func TestNewEndpoint_WiresOutbound(t *testing.T) {
	ep := NewEndpoint(EndpointConfig{
		UASConfig: UASConfig{Host: "127.0.0.1", Port: 15060},
		Outbound:  outbound.ManagerConfig{LocalIP: "127.0.0.1"},
	})
	if ep == nil || ep.UAS() == nil || ep.Outbound() == nil {
		t.Fatal("nil parts")
	}
}
