package api

import (
	"encoding/json"
	"io"
	"net"
	"net/http"
	"runtime"
	"strings"
	"time"
)

type handlers struct {
	provider StatsProvider
	server   *Server
}

func (h *handlers) health(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]interface{}{
		"ok":       true,
		"nodeId":   h.provider.NodeID(),
		"nodeName": h.provider.NodeName(),
		"uptime":   int64(h.provider.Uptime().Seconds()),
	})
}

func (h *handlers) stats(w http.ResponseWriter, r *http.Request) {
	ds := h.provider.GetDashboardStats()
	cfg := h.provider.GetConfig()

	jsonOK(w, map[string]interface{}{
		"node": map[string]interface{}{
			"id":     h.provider.NodeID(),
			"name":   h.provider.NodeName(),
			"uptime": int64(h.provider.Uptime().Seconds()),
		},
		"stats":       ds,
		"stratumPort": cfg.Stratum.Port,
	})
}

func (h *handlers) miners(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, h.provider.GetMiners())
}

func (h *handlers) pairs(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, h.provider.GetPairStatus())
}

func (h *handlers) getConfig(w http.ResponseWriter, r *http.Request) {
	cfg := h.provider.GetConfig()
	// Node/proxy are returned without passwords so the setup form can pre-fill
	// safely; a blank password field on save means "keep the existing one".
	jsonOK(w, map[string]interface{}{
		"miningMode": cfg.MiningMode,
		"mining":     cfg.Mining,
		"stratum":    cfg.Stratum,
		"vardiff":    cfg.Vardiff,
		"node": map[string]interface{}{
			"host":   cfg.Node.Host,
			"port":   cfg.Node.Port,
			"username": cfg.Node.Username,
			"useSSL": cfg.Node.UseSSL,
		},
		"proxy": map[string]interface{}{
			"url":        cfg.Proxy.URL,
			"workerName": cfg.Proxy.WorkerName,
		},
		"app": map[string]interface{}{
			"logLevel":        cfg.App.LogLevel,
			"electricityCost": cfg.App.ElectricityCost,
		},
	})
}

func (h *handlers) configured(w http.ResponseWriter, r *http.Request) {
	isConfigured := false
	if h.server != nil {
		isConfigured = h.server.configured
	}
	jsonOK(w, map[string]bool{"configured": isConfigured})
}

func (h *handlers) setup(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodOptions {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.WriteHeader(http.StatusNoContent)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req SetupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON: "+err.Error())
		return
	}

	// Validate required fields.
	req.NodeName = strings.TrimSpace(req.NodeName)
	if req.NodeName == "" {
		req.NodeName = "edge"
	}
	if req.APIPort <= 0 || req.APIPort > 65535 {
		req.APIPort = 9090
	}
	var enabled []PairEndpoint
	for _, ep := range req.Endpoints {
		if ep.Enabled {
			enabled = append(enabled, ep)
		}
	}
	if len(enabled) == 0 {
		jsonError(w, "at least one endpoint must be enabled")
		return
	}
	req.Endpoints = enabled

	if h.server == nil || h.server.OnSetup == nil {
		jsonError(w, "setup handler not available")
		return
	}

	if err := h.server.OnSetup(req); err != nil {
		jsonError(w, err.Error())
		return
	}

	// Build firewall commands for the response — OS-appropriate.
	var fwCmds []string
	if runtime.GOOS == "windows" {
		for _, ep := range req.Endpoints {
			fwCmds = append(fwCmds,
				`New-NetFirewallRule -DisplayName "GoVault `+ep.Coin+` `+ep.Mode+`" -Direction Inbound -Protocol TCP -LocalPort `+itoa(ep.LocalPort)+` -Action Allow`)
		}
	} else {
		fwCmds = append(fwCmds, "sudo ufw allow 22/tcp")
		for _, ep := range req.Endpoints {
			fwCmds = append(fwCmds,
				"sudo ufw allow "+itoa(ep.LocalPort)+"/tcp   # "+ep.Coin+" "+ep.Mode)
		}
		fwCmds = append(fwCmds, "sudo ufw --force enable")
	}

	jsonOK(w, map[string]interface{}{
		"ok":               true,
		"firewallCommands": fwCmds,
	})
}

func (h *handlers) network(w http.ResponseWriter, r *http.Request) {
	// Local IPs — all non-loopback IPv4 addresses on this machine.
	var localIPs []string
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagLoopback != 0 || iface.Flags&net.FlagUp == 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.IsLoopback() || ip.To4() == nil {
				continue
			}
			localIPs = append(localIPs, ip.String())
		}
	}

	// Public IP — query ipify with a short timeout; non-fatal if it fails.
	publicIP := ""
	client := &http.Client{Timeout: 4 * time.Second}
	if resp, err := client.Get("https://api.ipify.org"); err == nil {
		defer resp.Body.Close()
		if b, err := io.ReadAll(resp.Body); err == nil {
			publicIP = strings.TrimSpace(string(b))
		}
	}

	jsonOK(w, map[string]interface{}{
		"localIPs": localIPs,
		"publicIP": publicIP,
	})
}

// itoa converts an int to string without importing strconv in handlers.
func itoa(n int) string {
	b := make([]byte, 0, 6)
	if n == 0 {
		return "0"
	}
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

// jsonOK writes v as JSON with CORS headers.
func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(v)
}

// jsonError writes an error JSON response.
func jsonError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// adminAuth checks the X-Admin-Token header. Returns false and writes an error
// response if the token is missing, not configured, or incorrect.
func (h *handlers) adminAuth(w http.ResponseWriter, r *http.Request) bool {
	if h.server == nil || h.server.AdminToken == "" {
		http.NotFound(w, r)
		return false
	}
	if r.Header.Get("X-Admin-Token") != h.server.AdminToken {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return false
	}
	return true
}

func (h *handlers) adminSettings(w http.ResponseWriter, r *http.Request) {
	if !h.adminAuth(w, r) {
		return
	}
	cfg := h.provider.GetConfig()
	if r.Method == http.MethodGet {
		jsonOK(w, map[string]interface{}{
			"maxConn":        cfg.Stratum.MaxConn,
			"maxConnPerIP":   cfg.Stratum.MaxConnPerIP,
			"authTimeoutSec": cfg.Stratum.AuthTimeoutSec,
			"logLevel":       cfg.App.LogLevel,
		})
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var req SettingsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		jsonError(w, "invalid JSON")
		return
	}
	if h.server.OnSettings == nil {
		jsonError(w, "not available")
		return
	}
	if err := h.server.OnSettings(req); err != nil {
		jsonError(w, err.Error())
		return
	}
	cfg = h.provider.GetConfig()
	jsonOK(w, map[string]interface{}{
		"maxConn":        cfg.Stratum.MaxConn,
		"maxConnPerIP":   cfg.Stratum.MaxConnPerIP,
		"authTimeoutSec": cfg.Stratum.AuthTimeoutSec,
		"logLevel":       cfg.App.LogLevel,
	})
}

func (h *handlers) adminRestart(w http.ResponseWriter, r *http.Request) {
	if !h.adminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	jsonOK(w, map[string]bool{"ok": true})
	if h.server.OnRestart != nil {
		go func() {
			time.Sleep(200 * time.Millisecond)
			h.server.OnRestart()
		}()
	}
}

func (h *handlers) adminNginx(w http.ResponseWriter, r *http.Request) {
	if !h.adminAuth(w, r) {
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Extend write deadline — nginx install + certbot can take 1-2 minutes.
	rc := http.NewResponseController(w)
	rc.SetWriteDeadline(time.Now().Add(5 * time.Minute))

	var req struct {
		Domain string `json:"domain"`
	}
	json.NewDecoder(r.Body).Decode(&req)
	req.Domain = strings.TrimSpace(req.Domain)

	if h.server.OnNginxSetup == nil {
		jsonError(w, "nginx setup not available")
		return
	}

	output, err := h.server.OnNginxSetup(req.Domain)
	if err != nil {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.WriteHeader(http.StatusInternalServerError)
		json.NewEncoder(w).Encode(map[string]interface{}{
			"error":  err.Error(),
			"output": output,
		})
		return
	}
	jsonOK(w, map[string]interface{}{"ok": true, "output": output})
}
