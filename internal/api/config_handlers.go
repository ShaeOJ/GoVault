package api

import (
	"encoding/json"
	"net/http"
	"strings"

	"govault/internal/coin"
	"govault/internal/config"
	"govault/internal/miner"
	"govault/internal/node"
	"govault/internal/upstream"
)

// hashrateProvider is optionally implemented by an engine to feed the dashboard
// hashrate chart. Kept separate from StatsProvider so edgenode need not add it.
type hashrateProvider interface {
	GetHashrateHistory(period string) []miner.HashratePoint
}

// hashrate handles GET /api/hashrate?period=1h|6h|24h|7d.
func (h *handlers) hashrate(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	if period == "" {
		period = "1h"
	}
	if hp, ok := h.provider.(hashrateProvider); ok {
		jsonOK(w, hp.GetHashrateHistory(period))
		return
	}
	jsonOK(w, []miner.HashratePoint{})
}

// These handlers back the appliance's full setup/settings page (cmd/relay-mk1):
// a complete solo/proxy config form plus connectivity tests. They are additive —
// edgenode leaves OnConfigUpdate nil, so POST /api/config simply reports that
// saving isn't supported there.

func cors(w http.ResponseWriter) {
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Access-Control-Allow-Methods", "POST, GET, OPTIONS")
	w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
}

// configRouter dispatches /api/config: GET returns the current config, POST/
// OPTIONS saves it.
func (h *handlers) configRouter(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet {
		h.getConfig(w, r)
		return
	}
	h.saveConfig(w, r)
}

// saveConfig handles POST /api/config: decode a full config and apply it.
func (h *handlers) saveConfig(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		cors(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if h.server == nil || h.server.OnConfigUpdate == nil {
		jsonError(w, "config save not supported on this node")
		return
	}
	var cfg config.Config
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		jsonError(w, "invalid JSON: "+err.Error())
		return
	}
	if err := h.server.OnConfigUpdate(&cfg); err != nil {
		jsonError(w, err.Error())
		return
	}
	if h.server != nil {
		h.server.SetConfigured(true)
	}
	jsonOK(w, map[string]interface{}{"ok": true})
}

// coins handles GET /api/coins: the supported coins for the setup dropdown.
func (h *handlers) coins(w http.ResponseWriter, r *http.Request) {
	var out []map[string]interface{}
	for _, id := range coin.List() {
		c := coin.Get(id)
		out = append(out, map[string]interface{}{
			"id":             c.CoinID,
			"symbol":         c.Symbol,
			"name":           c.Name,
			"defaultRPCPort": c.DefaultRPCPort,
			"defaultRPCUser": c.DefaultRPCUsername,
			"segwit":         c.SegWit,
		})
	}
	jsonOK(w, out)
}

// testNode handles POST /api/test-node: verify a coin node's RPC (solo mode).
func (h *handlers) testNode(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		cors(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var req struct {
		Host     string `json:"host"`
		Port     int    `json:"port"`
		Username string `json:"username"`
		Password string `json:"password"`
		UseSSL   bool   `json:"useSSL"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON: "+err.Error())
		return
	}
	client := node.NewQuickClient(req.Host, req.Port, req.Username, req.Password, req.UseSSL)
	if err := client.Ping(); err != nil {
		jsonOK(w, map[string]interface{}{"connected": false, "error": err.Error()})
		return
	}
	info, err := client.GetBlockchainInfo()
	if err != nil {
		jsonOK(w, map[string]interface{}{"connected": false, "error": err.Error()})
		return
	}
	jsonOK(w, map[string]interface{}{
		"connected":   true,
		"chain":       info.Chain,
		"blocks":      info.Blocks,
		"headers":     info.Headers,
		"syncPercent": info.VerificationProgress * 100,
		"syncing":     info.InitialBlockDownload,
	})
}

// testUpstream handles POST /api/test-upstream: verify an upstream pool (proxy).
func (h *handlers) testUpstream(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		cors(w)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	var req struct {
		URL      string `json:"url"`
		Worker   string `json:"worker"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON: "+err.Error())
		return
	}
	if strings.TrimSpace(req.URL) == "" {
		jsonError(w, "upstream URL required")
		return
	}
	pw := req.Password
	if pw == "" {
		pw = "x"
	}
	uc := upstream.NewClient(req.URL, req.Worker, pw, h.server.log)
	if err := uc.Connect(); err != nil {
		jsonOK(w, map[string]interface{}{"connected": false, "error": err.Error()})
		return
	}
	defer uc.Stop()
	jsonOK(w, map[string]interface{}{
		"connected":    true,
		"extranonce1":  uc.Extranonce1(),
		"upstreamDiff": uc.UpstreamDifficulty(),
	})
}
