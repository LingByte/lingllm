package rtppool

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/LingByte/lingllm/examples/sip-split/controlapi"
)

// Registrar heartbeats an RTP node to the signaling server's registry API.
type Registrar struct {
	Client   *http.Client
	Registry string // signaling HTTP base URL, e.g. http://host:8080
	Node     controlapi.RegisterNodeRequest
	Interval time.Duration
	LegCount func() int // optional; sent as active_legs on each heartbeat
}

// NewRegistrar builds a registrar with a default 10s heartbeat interval.
func NewRegistrar(client *http.Client, registryURL string, node controlapi.RegisterNodeRequest) *Registrar {
	return &Registrar{
		Client:   client,
		Registry: strings.TrimRight(strings.TrimSpace(registryURL), "/"),
		Node:     node,
		Interval: 10 * time.Second,
	}
}

// Run registers immediately, then heartbeats until ctx is cancelled.
func (r *Registrar) Run(ctx context.Context) error {
	if r == nil || r.Registry == "" {
		return fmt.Errorf("registry url required")
	}
	if r.Client == nil {
		r.Client = http.DefaultClient
	}
	if r.Interval <= 0 {
		r.Interval = 10 * time.Second
	}
	if err := r.register(ctx); err != nil {
		return err
	}
	ticker := time.NewTicker(r.Interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := r.register(ctx); err != nil {
				return err
			}
		}
	}
}

// Deregister removes this node from the signaling pool (best-effort).
func (r *Registrar) Deregister(ctx context.Context) error {
	if r == nil || r.Registry == "" || strings.TrimSpace(r.Node.NodeID) == "" {
		return nil
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, r.Registry+controlapi.NodePath(r.Node.NodeID), nil)
	if err != nil {
		return err
	}
	resp, err := r.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNotFound {
		return fmt.Errorf("deregister status %d", resp.StatusCode)
	}
	return nil
}

func (r *Registrar) register(ctx context.Context) error {
	if r.LegCount != nil {
		r.Node.ActiveLegs = r.LegCount()
	}
	body, _ := json.Marshal(r.Node)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Registry+controlapi.NodesRegisterPath, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := r.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		var eb controlapi.ErrorBody
		_ = json.Unmarshal(raw, &eb)
		if eb.Error != "" {
			return fmt.Errorf("register: %s", eb.Error)
		}
		return fmt.Errorf("register status %d", resp.StatusCode)
	}
	return nil
}

// PublicControlURL builds the control URL RTP advertises to signaling.
func PublicControlURL(publicEnv, mediaIP string, port int) string {
	if u := strings.TrimRight(strings.TrimSpace(publicEnv), "/"); u != "" {
		return u
	}
	ip := strings.TrimSpace(mediaIP)
	if ip == "" {
		ip = "127.0.0.1"
	}
	return fmt.Sprintf("http://%s:%d", ip, port)
}
