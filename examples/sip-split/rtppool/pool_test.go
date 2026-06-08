package rtppool

import (
	"testing"
	"time"

	"github.com/LingByte/lingllm/examples/sip-split/controlapi"
)

func TestParseControlURLs_Multi(t *testing.T) {
	got := ParseControlURLs("http://a:8090, http://b:8091/", "")
	if len(got) != 2 || got[0] != "http://a:8090" || got[1] != "http://b:8091" {
		t.Fatalf("got %v", got)
	}
}

func TestParseControlURLs_Empty(t *testing.T) {
	got := ParseControlURLs("", "")
	if got != nil {
		t.Fatalf("expected nil, got %v", got)
	}
}

func TestPool_SortedHealthy(t *testing.T) {
	p := &Pool{
		nodes: map[string]*Node{
			"b": {ID: "b", ControlURL: "http://b", Healthy: true, ActiveLegs: 3},
			"a": {ID: "a", ControlURL: "http://a", Healthy: true, ActiveLegs: 1},
			"c": {ID: "c", ControlURL: "http://c", Healthy: false, ActiveLegs: 0},
		},
		order: []string{"b", "a", "c"},
		binds: make(map[string]string),
	}
	got := p.sortedHealthy()
	if len(got) != 2 || got[0].ID != "a" || got[1].ID != "b" {
		t.Fatalf("order: %+v", got)
	}
}

func TestPool_RegisterAndPrune(t *testing.T) {
	p := New(nil)
	p.SetTTL(30 * time.Second)

	resp, err := p.Register(controlapi.RegisterNodeRequest{
		NodeID:     "rtp-a",
		ControlURL: "http://a:8090",
		MediaIP:    "10.0.0.1",
		ActiveLegs: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.NodeID != "rtp-a" || resp.TTLSecs != 30 {
		t.Fatalf("resp: %+v", resp)
	}
	nodes := p.Nodes()
	if len(nodes) != 1 || nodes[0].ID != "rtp-a" || nodes[0].ActiveLegs != 2 {
		t.Fatalf("nodes: %+v", nodes)
	}

	p.mu.Lock()
	p.nodes["rtp-a"].LastSeen = time.Now().Add(-60 * time.Second)
	p.mu.Unlock()
	if n := p.Prune(); n != 1 {
		t.Fatalf("pruned %d", n)
	}
	if len(p.nodes) != 0 {
		t.Fatalf("expected empty pool")
	}
}
