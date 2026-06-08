// Package controlapi defines the HTTP contract between the split SIP signaling
// server and one or more RTP media servers.
package controlapi

import (
	"fmt"
	"net/http"
)

const (
	BasePath           = "/v1"
	HealthPath         = "/v1/health"
	LegsPath           = "/v1/legs"
	LegStartSuffix     = "/start"
	NodesPath          = "/v1/nodes"
	NodesRegisterPath  = "/v1/nodes/register"
	DefaultNodeTTLSecs = 45
)

// PrepareLegRequest is sent by the signaling server when an INVITE offer arrives.
type PrepareLegRequest struct {
	CallID   string `json:"call_id"`
	OfferSDP string `json:"offer_sdp"`
}

// PrepareLegResponse tells signaling which c=/m= to place in the 200 OK SDP answer.
type PrepareLegResponse struct {
	CallID    string `json:"call_id"`
	NodeID    string `json:"node_id,omitempty"`
	MediaIP   string `json:"media_ip"`
	MediaPort int    `json:"media_port"`
	Codec     string `json:"codec"`
}

// HealthResponse is returned by each RTP media server for pool selection.
type HealthResponse struct {
	Status     string `json:"status"`
	NodeID     string `json:"node_id"`
	ActiveLegs int    `json:"active_legs"`
	MediaIP    string `json:"media_ip"`
}

// RegisterNodeRequest is sent by an RTP media server to join the signaling pool.
type RegisterNodeRequest struct {
	NodeID     string `json:"node_id"`
	ControlURL string `json:"control_url"`
	MediaIP    string `json:"media_ip"`
	ActiveLegs int    `json:"active_legs,omitempty"`
}

// RegisterNodeResponse acknowledges registration / heartbeat.
type RegisterNodeResponse struct {
	NodeID  string `json:"node_id"`
	TTLSecs int    `json:"ttl_sec"`
}

// NodeInfo describes one RTP node known to the signaling server.
type NodeInfo struct {
	NodeID     string `json:"node_id"`
	ControlURL string `json:"control_url"`
	MediaIP    string `json:"media_ip"`
	ActiveLegs int    `json:"active_legs"`
	Healthy    bool   `json:"healthy"`
	Static     bool   `json:"static,omitempty"`
}

// ErrorBody is returned on failed control API calls.
type ErrorBody struct {
	Error string `json:"error"`
}

func LegStartPath(callID string) string {
	return fmt.Sprintf("%s/%s%s", LegsPath, callID, LegStartSuffix)
}

func LegPath(callID string) string {
	return fmt.Sprintf("%s/%s", LegsPath, callID)
}

func NodePath(nodeID string) string {
	return fmt.Sprintf("%s/%s", NodesPath, nodeID)
}

// HealthOK is a trivial liveness handler (signaling-side probe fallback).
func HealthOK(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}
