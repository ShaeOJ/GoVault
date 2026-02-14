package miner

import (
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// DiscoveredMiner is a miner found on the local network.
type DiscoveredMiner struct {
	IP          string  `json:"ip"`
	Hostname    string  `json:"hostname"`
	Model       string  `json:"model"`
	Hashrate    float64 `json:"hashrate"`
	Temperature float64 `json:"temperature"`
	CurrentPool string  `json:"currentPool"`
	Firmware    string  `json:"firmware"`
}

// axeOSSystemInfo maps the AxeOS /api/system/info response.
type axeOSSystemInfo struct {
	Power        float64 `json:"power"`
	Voltage      float64 `json:"voltage"`
	Current      float64 `json:"current"`
	Temp         float64 `json:"temp"`
	VrTemp       float64 `json:"vrTemp"`
	HashRate     float64 `json:"hashRate"`
	BestDiff     string  `json:"bestDiff"`
	FreeHeap     int     `json:"freeHeap"`
	Hostname     string  `json:"hostname"`
	ASICModel    string  `json:"ASICModel"`
	StratumURL   string  `json:"stratumURL"`
	StratumPort  int     `json:"stratumPort"`
	StratumUser  string  `json:"stratumUser"`
	Version      string  `json:"version"`
	BoardVersion string  `json:"boardVersion"`
}

// Discovery scans the local network for compatible mining devices.
type Discovery struct {
	client  *http.Client
	results []DiscoveredMiner
	mu      sync.Mutex
}

func NewDiscovery() *Discovery {
	return &Discovery{
		client: &http.Client{
			Timeout: 2 * time.Second,
		},
	}
}

// ScanSubnet scans the local /24 subnet for AxeOS devices.
func (d *Discovery) ScanSubnet() []DiscoveredMiner {
	localIP := getLocalIP()
	if localIP == "" {
		return nil
	}

	// Get /24 subnet
	parts := strings.Split(localIP, ".")
	if len(parts) != 4 {
		return nil
	}
	subnet := strings.Join(parts[:3], ".")

	var results []DiscoveredMiner
	var mu sync.Mutex
	var wg sync.WaitGroup

	// Scan all IPs in the subnet concurrently (limited to 50 at a time)
	sem := make(chan struct{}, 50)

	for i := 1; i <= 254; i++ {
		ip := fmt.Sprintf("%s.%d", subnet, i)
		if ip == localIP {
			continue
		}

		wg.Add(1)
		sem <- struct{}{}
		go func(ip string) {
			defer wg.Done()
			defer func() { <-sem }()

			miner, err := d.ProbeHost(ip)
			if err == nil && miner != nil {
				mu.Lock()
				results = append(results, *miner)
				mu.Unlock()
			}
		}(ip)
	}

	wg.Wait()

	d.mu.Lock()
	d.results = results
	d.mu.Unlock()

	return results
}

// ProbeHost checks if an IP is running AxeOS by querying /api/system/info.
func (d *Discovery) ProbeHost(ip string) (*DiscoveredMiner, error) {
	// Quick TCP check first
	conn, err := net.DialTimeout("tcp", ip+":80", 1*time.Second)
	if err != nil {
		return nil, err
	}
	conn.Close()

	url := fmt.Sprintf("http://%s/api/system/info", ip)
	resp, err := d.client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var info axeOSSystemInfo
	if err := json.Unmarshal(body, &info); err != nil {
		return nil, err
	}

	// Validate it looks like an AxeOS device
	if info.ASICModel == "" && info.HashRate == 0 {
		return nil, fmt.Errorf("not a mining device")
	}

	currentPool := info.StratumURL
	if info.StratumPort > 0 {
		currentPool = fmt.Sprintf("%s:%d", info.StratumURL, info.StratumPort)
	}

	return &DiscoveredMiner{
		IP:          ip,
		Hostname:    info.Hostname,
		Model:       info.ASICModel,
		Hashrate:    info.HashRate / 1e9, // Convert to GH/s
		Temperature: info.Temp,
		CurrentPool: currentPool,
		Firmware:    info.Version,
	}, nil
}

// ConfigureMiner sends new pool settings to an AxeOS device.
func (d *Discovery) ConfigureMiner(ip, stratumURL string, stratumPort int, stratumUser string) error {
	payload := map[string]interface{}{
		"stratumURL":  stratumURL,
		"stratumPort": stratumPort,
		"stratumUser": stratumUser,
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	url := fmt.Sprintf("http://%s/api/system", ip)
	req, err := http.NewRequest("PATCH", url, strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := d.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("configure failed with status %d", resp.StatusCode)
	}

	return nil
}

// getLocalIP returns the machine's local IPv4 address.
func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}

	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}

	return ""
}

// GetLocalIP exports the local IP for use in QR code generation etc.
func GetLocalIP() string {
	return getLocalIP()
}
