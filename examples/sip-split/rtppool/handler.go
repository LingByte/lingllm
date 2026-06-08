package rtppool

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/LingByte/lingllm/examples/sip-split/controlapi"
)

// RegisterHTTP mounts the signaling-side node registry API on mux.
func RegisterHTTP(mux *http.ServeMux, pool *Pool) {
	mux.HandleFunc(controlapi.NodesRegisterPath, pool.handleRegister)
	mux.HandleFunc(controlapi.NodesPath, pool.handleNodes)
	mux.HandleFunc(controlapi.NodesPath+"/", pool.handleNodeByID)
}

func (p *Pool) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req controlapi.RegisterNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, controlapi.ErrorBody{Error: "bad json"})
		return
	}
	resp, err := p.Register(req)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, controlapi.ErrorBody{Error: err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (p *Pool) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != controlapi.NodesPath {
		p.handleNodeByID(w, r)
		return
	}
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": p.NodeInfos()})
}

func (p *Pool) handleNodeByID(w http.ResponseWriter, r *http.Request) {
	nodeID := strings.TrimPrefix(r.URL.Path, controlapi.NodesPath+"/")
	nodeID = strings.Trim(strings.TrimSpace(nodeID), "/")
	if nodeID == "" {
		p.handleNodes(w, r)
		return
	}
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !p.Unregister(nodeID) {
		writeJSON(w, http.StatusNotFound, controlapi.ErrorBody{Error: "node not found"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
