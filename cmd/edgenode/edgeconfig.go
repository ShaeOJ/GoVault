package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// defaultAdminToken is used when no token is set in edge.json.
// Change this before building your production binaries.
const defaultAdminToken = "firepool-admin-2026"

// defaultBeaconURL is the admin panel URL edge nodes register with by default.
// Operators can override this in edge.json by setting beaconUrl to "" to disable.
const defaultBeaconURL = "http://174.4.45.33:9090"

// EdgeEndpoint describes one upstream-pool ↔ local-stratum proxy pair.
type EdgeEndpoint struct {
	Coin        string `json:"coin"`        // display symbol, e.g. "BC2"
	CoinID      string `json:"coinId"`      // internal coin ID, e.g. "bc2"
	Mode        string `json:"mode"`        // "prop" or "solo"
	LocalPort   int    `json:"localPort"`   // port miners connect to on this machine
	UpstreamURL string `json:"upstreamUrl"` // stratum+tcp://firepool.ca:PORT
	WorkerName  string `json:"workerName"`  // operator address for upstream auth
	Password    string `json:"password"`    // usually "x"
	Enabled     bool   `json:"enabled"`
}

// EdgeConfig holds edge-node-specific settings stored separately from the
// main stratum config. Lives at data/edge.json next to the binary.
type EdgeConfig struct {
	NodeID     string         `json:"nodeId"`
	NodeName   string         `json:"nodeName"`
	APIPort    int            `json:"apiPort"`
	Endpoints  []EdgeEndpoint `json:"endpoints"`
	AdminToken string         `json:"adminToken"`

	// BeaconURL is the URL of the admin panel (e.g. "http://firepool.ca:9090").
	// When set, the edge node registers itself with the admin panel on startup.
	BeaconURL string `json:"beaconUrl,omitempty"`

	// SelfURL is the URL the admin panel should use to reach this node
	// (e.g. "http://eu.firepool.ca:8080"). When empty, the edge node attempts
	// to auto-detect its public IP and constructs the URL from that.
	SelfURL string `json:"selfUrl,omitempty"`
}

// ActiveEndpoints returns the subset of endpoints that are enabled.
func (e *EdgeConfig) ActiveEndpoints() []EdgeEndpoint {
	var active []EdgeEndpoint
	for _, ep := range e.Endpoints {
		if ep.Enabled {
			active = append(active, ep)
		}
	}
	return active
}

func LoadEdgeConfig() *EdgeConfig {
	cfg := &EdgeConfig{
		NodeID:     generateNodeID(),
		NodeName:   "edge",
		APIPort:    9090,
		AdminToken: defaultAdminToken,
		BeaconURL:  defaultBeaconURL,
	}

	path := edgeConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		saveEdgeConfig(path, cfg)
		return cfg
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return cfg
	}

	if cfg.NodeID == "" {
		cfg.NodeID = generateNodeID()
		saveEdgeConfig(path, cfg)
	}
	if cfg.APIPort == 0 {
		cfg.APIPort = 9090
	}
	if cfg.AdminToken == "" {
		cfg.AdminToken = defaultAdminToken
		saveEdgeConfig(path, cfg)
	}

	return cfg
}

func edgeConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "data/edge.json"
	}
	return filepath.Join(filepath.Dir(exe), "data", "edge.json")
}

func saveEdgeConfig(path string, cfg *EdgeConfig) {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return
	}
	os.Rename(tmp, path)
}

func generateNodeID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
