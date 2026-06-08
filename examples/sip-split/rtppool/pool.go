// Package rtppool routes signaling control requests across multiple RTP media nodes.
//
// Nodes are discovered dynamically: RTP servers register with the signaling
// server's HTTP control API and send periodic heartbeats. Static seed URLs
// (RTP_CONTROL_URLS) remain supported for local development.
package rtppool

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/LingByte/lingllm/examples/sip-split/controlapi"
)

// Node is one RTP media server's control plane endpoint.
type Node struct {
	ID         string
	ControlURL string
	MediaIP    string
	ActiveLegs int
	Healthy    bool
	Static     bool
	LastSeen   time.Time
}

// Pool selects RTP nodes and remembers which node owns each Call-ID.
type Pool struct {
	client *http.Client
	ttl    time.Duration
	mu     sync.RWMutex
	nodes  map[string]*Node // keyed by node ID
	order  []string         // stable iteration order
	binds  map[string]string
}

// New creates an empty pool. Optional seed URLs are treated as static nodes.
func New(client *http.Client, seedURLs ...string) *Pool {
	p := &Pool{
		client: client,
		ttl:    time.Duration(controlapi.DefaultNodeTTLSecs) * time.Second,
		nodes:  make(map[string]*Node),
		binds:  make(map[string]string),
	}
	now := time.Now()
	for i, raw := range seedURLs {
		u := strings.TrimRight(strings.TrimSpace(raw), "/")
		if u == "" {
			continue
		}
		id := fmt.Sprintf("seed-%d", i+1)
		p.nodes[id] = &Node{
			ID:         id,
			ControlURL: u,
			Static:     true,
			Healthy:    true,
			LastSeen:   now,
		}
		p.order = append(p.order, id)
	}
	return p
}

// SetTTL overrides the expiry window for dynamically registered nodes.
func (p *Pool) SetTTL(d time.Duration) {
	if p == nil || d <= 0 {
		return
	}
	p.mu.Lock()
	p.ttl = d
	p.mu.Unlock()
}

// Register adds or refreshes a dynamically registered RTP node.
func (p *Pool) Register(req controlapi.RegisterNodeRequest) (controlapi.RegisterNodeResponse, error) {
	if p == nil {
		return controlapi.RegisterNodeResponse{}, fmt.Errorf("nil pool")
	}
	id := strings.TrimSpace(req.NodeID)
	ctrl := strings.TrimRight(strings.TrimSpace(req.ControlURL), "/")
	if id == "" || ctrl == "" {
		return controlapi.RegisterNodeResponse{}, fmt.Errorf("node_id and control_url required")
	}
	now := time.Now()
	p.mu.Lock()
	defer p.mu.Unlock()
	n, ok := p.nodes[id]
	if !ok {
		n = &Node{ID: id}
		p.nodes[id] = n
		p.order = append(p.order, id)
	}
	n.ControlURL = ctrl
	n.MediaIP = strings.TrimSpace(req.MediaIP)
	n.ActiveLegs = req.ActiveLegs
	n.Healthy = true
	n.Static = false
	n.LastSeen = now
	ttl := int(p.ttl.Seconds())
	if ttl <= 0 {
		ttl = controlapi.DefaultNodeTTLSecs
	}
	return controlapi.RegisterNodeResponse{NodeID: id, TTLSecs: ttl}, nil
}

// Unregister removes a dynamically registered node.
func (p *Pool) Unregister(nodeID string) bool {
	if p == nil {
		return false
	}
	nodeID = strings.TrimSpace(nodeID)
	p.mu.Lock()
	defer p.mu.Unlock()
	n := p.nodes[nodeID]
	if n == nil || n.Static {
		return false
	}
	delete(p.nodes, nodeID)
	p.order = removeID(p.order, nodeID)
	return true
}

// Prune drops dynamic nodes that have not heartbeated within TTL.
func (p *Pool) Prune() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	cutoff := time.Now().Add(-p.ttl)
	removed := 0
	for id, n := range p.nodes {
		if n.Static || n.LastSeen.After(cutoff) {
			continue
		}
		delete(p.nodes, id)
		p.order = removeID(p.order, id)
		removed++
	}
	return removed
}

// Nodes returns a snapshot after refreshing health from all backends.
func (p *Pool) Nodes() []Node {
	if p == nil {
		return nil
	}
	p.Refresh()
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Node, 0, len(p.order))
	for _, id := range p.order {
		if n := p.nodes[id]; n != nil {
			out = append(out, *n)
		}
	}
	return out
}

// NodeInfos returns registered nodes for the signaling HTTP list API.
func (p *Pool) NodeInfos() []controlapi.NodeInfo {
	nodes := p.Nodes()
	out := make([]controlapi.NodeInfo, len(nodes))
	for i, n := range nodes {
		out[i] = controlapi.NodeInfo{
			NodeID:     n.ID,
			ControlURL: n.ControlURL,
			MediaIP:    n.MediaIP,
			ActiveLegs: n.ActiveLegs,
			Healthy:    n.Healthy,
			Static:     n.Static,
		}
	}
	return out
}

// Refresh probes /v1/health on every known node.
func (p *Pool) Refresh() {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.client == nil {
		return
	}
	for _, id := range p.order {
		n := p.nodes[id]
		if n == nil {
			continue
		}
		h, err := fetchHealth(p.client, n.ControlURL)
		if err != nil {
			n.Healthy = false
			continue
		}
		n.Healthy = true
		n.ActiveLegs = h.ActiveLegs
		if h.MediaIP != "" {
			n.MediaIP = h.MediaIP
		}
		if h.NodeID != "" && !n.Static {
			n.ID = h.NodeID
		}
	}
}

// Prepare allocates a media leg on the least-loaded healthy node (with failover).
func (p *Pool) Prepare(callID, offerSDP string) (*controlapi.PrepareLegResponse, string, error) {
	if p == nil {
		return nil, "", fmt.Errorf("nil pool")
	}
	callID = strings.TrimSpace(callID)
	if callID == "" {
		return nil, "", fmt.Errorf("empty call_id")
	}
	p.Refresh()
	candidates := p.sortedHealthy()
	if len(candidates) == 0 {
		return nil, "", fmt.Errorf("no healthy rtp nodes (wait for rtp registration)")
	}
	var lastErr error
	for _, n := range candidates {
		resp, err := postPrepare(p.client, n.ControlURL, callID, offerSDP)
		if err != nil {
			lastErr = err
			continue
		}
		if resp.NodeID == "" {
			resp.NodeID = n.ID
		}
		p.mu.Lock()
		p.binds[callID] = n.ControlURL
		p.mu.Unlock()
		return resp, n.ControlURL, nil
	}
	if lastErr != nil {
		return nil, "", lastErr
	}
	return nil, "", fmt.Errorf("prepare failed on all nodes")
}

// Start activates the leg on the node that prepared it.
func (p *Pool) Start(callID string) error {
	url, err := p.binding(callID)
	if err != nil {
		return err
	}
	return postStart(p.client, url, callID)
}

// Delete removes the leg on the bound node.
func (p *Pool) Delete(callID string) error {
	url, err := p.binding(callID)
	if err != nil {
		return err
	}
	err = doDelete(p.client, url, callID)
	p.mu.Lock()
	delete(p.binds, callID)
	p.mu.Unlock()
	return err
}

func (p *Pool) binding(callID string) (string, error) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	u := p.binds[callID]
	if u == "" {
		return "", fmt.Errorf("no rtp binding for call_id %q", callID)
	}
	return u, nil
}

func (p *Pool) sortedHealthy() []Node {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make([]Node, 0, len(p.order))
	for _, id := range p.order {
		n := p.nodes[id]
		if n != nil && n.Healthy {
			out = append(out, *n)
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ActiveLegs != out[j].ActiveLegs {
			return out[i].ActiveLegs < out[j].ActiveLegs
		}
		return out[i].ID < out[j].ID
	})
	return out
}

func removeID(order []string, id string) []string {
	out := make([]string, 0, len(order))
	for _, x := range order {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

func fetchHealth(client *http.Client, base string) (*controlapi.HealthResponse, error) {
	resp, err := client.Get(base + controlapi.HealthPath)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("health %s: status %d", base, resp.StatusCode)
	}
	var h controlapi.HealthResponse
	if err := json.Unmarshal(raw, &h); err != nil {
		return nil, err
	}
	if h.Status == "" {
		h.Status = "ok"
	}
	return &h, nil
}

func postPrepare(client *http.Client, base, callID, offerSDP string) (*controlapi.PrepareLegResponse, error) {
	body, _ := json.Marshal(controlapi.PrepareLegRequest{CallID: callID, OfferSDP: offerSDP})
	resp, err := client.Post(base+controlapi.LegsPath, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusCreated {
		var eb controlapi.ErrorBody
		_ = json.Unmarshal(raw, &eb)
		if eb.Error != "" {
			return nil, fmt.Errorf("%s: %s", base, eb.Error)
		}
		return nil, fmt.Errorf("%s: prepare status %d", base, resp.StatusCode)
	}
	var out controlapi.PrepareLegResponse
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func postStart(client *http.Client, base, callID string) error {
	req, err := http.NewRequest(http.MethodPost, base+controlapi.LegStartPath(callID), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: start status %d", base, resp.StatusCode)
	}
	return nil
}

func doDelete(client *http.Client, base, callID string) error {
	req, err := http.NewRequest(http.MethodDelete, base+controlapi.LegPath(callID), nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s: delete status %d", base, resp.StatusCode)
	}
	return nil
}

// ParseControlURLs reads RTP_CONTROL_URLS (comma-separated) or RTP_CONTROL_URL.
// Empty env returns no seeds — RTP nodes are expected to register instead.
func ParseControlURLs(urlsEnv, singleEnv string) []string {
	raw := strings.TrimSpace(urlsEnv)
	if raw == "" {
		raw = strings.TrimSpace(singleEnv)
	}
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	var out []string
	for _, p := range parts {
		p = strings.TrimRight(strings.TrimSpace(p), "/")
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
