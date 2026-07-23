package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sync"
	"time"
)

type geoCoord struct{ lat, lng float64 }

type Poller struct {
	registry *Registry
	client   *http.Client
	stop     chan struct{}
	wg       sync.WaitGroup
	geoMu    sync.Mutex
	geoCache map[string]*geoCoord // nil = pending/failed, non-nil = resolved
}

func NewPoller(registry *Registry) *Poller {
	return &Poller{
		registry: registry,
		client:   &http.Client{Timeout: 5 * time.Second},
		stop:     make(chan struct{}),
		geoCache: make(map[string]*geoCoord),
	}
}

// geocode looks up lat/lng for an IP via ipapi.co, caching the result.
// Returns nil if the IP is empty, local, or the lookup fails.
func (p *Poller) geocode(ip string) *geoCoord {
	if ip == "" || ip == "localhost" {
		return nil
	}
	p.geoMu.Lock()
	v, seen := p.geoCache[ip]
	p.geoMu.Unlock()
	if seen {
		return v
	}
	// Mark as in-flight to prevent concurrent duplicate requests.
	p.geoMu.Lock()
	p.geoCache[ip] = nil
	p.geoMu.Unlock()

	// Small delay to avoid hitting ipapi.co rate limits when multiple nodes
	// geocode in the same poll cycle.
	time.Sleep(500 * time.Millisecond)

	gc := &http.Client{Timeout: 4 * time.Second}
	resp, err := gc.Get("https://ipapi.co/" + ip + "/json/")
	if err != nil {
		// Remove from cache so it retries next poll.
		p.geoMu.Lock()
		delete(p.geoCache, ip)
		p.geoMu.Unlock()
		return nil
	}
	defer resp.Body.Close()
	var d struct {
		Latitude  float64 `json:"latitude"`
		Longitude float64 `json:"longitude"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&d); err != nil || d.Latitude == 0 {
		// Remove from cache so it retries next poll.
		p.geoMu.Lock()
		delete(p.geoCache, ip)
		p.geoMu.Unlock()
		return nil
	}
	coord := &geoCoord{lat: d.Latitude, lng: d.Longitude}
	p.geoMu.Lock()
	p.geoCache[ip] = coord
	p.geoMu.Unlock()
	return coord
}

func (p *Poller) Start() {
	for i := 0; i < p.registry.Len(); i++ {
		i := i
		p.wg.Add(1)
		go func() {
			defer p.wg.Done()
			p.runLoop(i)
		}()
	}
}

func (p *Poller) Stop() {
	close(p.stop)
	p.wg.Wait()
}

// AddNode starts a polling goroutine for a newly registered node.
func (p *Poller) AddNode(entry NodeEntry) {
	idx := p.registry.Len() - 1 // just appended
	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		p.runLoop(idx)
	}()
}

func (p *Poller) runLoop(idx int) {
	p.pollNode(idx)
	tick := time.NewTicker(10 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-p.stop:
			return
		case <-tick.C:
			p.pollNode(idx)
		}
	}
}

func (p *Poller) pollNode(idx int) {
	var entry NodeEntry
	p.registry.Update(idx, func(s *NodeState) { entry = s.Entry })

	// Ping via /health
	t0 := time.Now()
	resp, err := p.client.Get(entry.URL + "/health")
	pingMs := time.Since(t0).Milliseconds()
	if err != nil {
		p.registry.Update(idx, func(s *NodeState) {
			s.failStreak++
			s.PingMs = -1
			s.Error = err.Error()
			if s.failStreak >= 2 {
				s.Online = false
			}
		})
		return
	}
	var health map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&health)
	resp.Body.Close()

	nodeID, _ := health["nodeId"].(string)
	nodeName, _ := health["nodeName"].(string)
	uptimeF, _ := health["uptime"].(float64)

	// Stats
	var hashrate float64
	var activeMiners int
	var sharesFwd, sharesAcc, sharesRej, blocksFound int64
	var bestDiff, netDiff float64
	if resp2, err := p.client.Get(entry.URL + "/api/stats"); err == nil {
		var statsEnv map[string]json.RawMessage
		json.NewDecoder(resp2.Body).Decode(&statsEnv)
		resp2.Body.Close()
		if raw, ok := statsEnv["stats"]; ok {
			var st map[string]interface{}
			json.Unmarshal(raw, &st)
			hashrate, _ = st["totalHashrate"].(float64)
			minersF, _ := st["activeMiners"].(float64)
			activeMiners = int(minersF)
			fwdF, _ := st["proxySharesFwd"].(float64)
			sharesFwd = int64(fwdF)
			accF, _ := st["proxySharesAccepted"].(float64)
			sharesAcc = int64(accF)
			rejF, _ := st["proxySharesRejected"].(float64)
			sharesRej = int64(rejF)
			bestDiff, _ = st["bestDifficulty"].(float64)
			blocksF, _ := st["blocksFound"].(float64)
			blocksFound = int64(blocksF)
			netDiff, _ = st["networkDifficulty"].(float64)
		}
	}

	// Network info
	var publicIP string
	var localIPs []string
	if resp3, err := p.client.Get(entry.URL + "/api/network"); err == nil {
		var net map[string]interface{}
		json.NewDecoder(resp3.Body).Decode(&net)
		resp3.Body.Close()
		publicIP, _ = net["publicIP"].(string)
		if publicIP == "" {
			if u, err := url.Parse(entry.URL); err == nil {
				if host := u.Hostname(); host != "" && host != "localhost" {
					publicIP = host
				}
			}
		}
		if arr, ok := net["localIPs"].([]interface{}); ok {
			for _, v := range arr {
				if s, ok := v.(string); ok {
					localIPs = append(localIPs, s)
				}
			}
		}
	}

	// Admin settings (may fail if token not set)
	maxConn := 0
	maxConnPerIP := 0
	authTimeoutSec := 0
	logLevel := ""
	tokenOK := false
	if entry.Token != "" {
		req, _ := http.NewRequest(http.MethodGet, entry.URL+"/api/admin/settings", nil)
		req.Header.Set("X-Admin-Token", entry.Token)
		if resp4, err := p.client.Do(req); err == nil {
			if resp4.StatusCode == 200 {
				var settings map[string]interface{}
				json.NewDecoder(resp4.Body).Decode(&settings)
				maxConnF, _ := settings["maxConn"].(float64)
				maxConn = int(maxConnF)
				maxConnPerIPF, _ := settings["maxConnPerIP"].(float64)
				maxConnPerIP = int(maxConnPerIPF)
				authTimeoutSecF, _ := settings["authTimeoutSec"].(float64)
				authTimeoutSec = int(authTimeoutSecF)
				logLevel, _ = settings["logLevel"].(string)
				tokenOK = true
			}
			resp4.Body.Close()
		}
	}

	// Geocode outside the registry lock — the HTTP request can take up to 4s
	// and registry.Update holds a write lock that blocks all reads.
	coord := p.geocode(publicIP)

	p.registry.Update(idx, func(s *NodeState) {
		s.Online = true
		s.failStreak = 0
		s.PingMs = pingMs
		// Rolling average over last 10 successful pings.
		s.pingHistory = append(s.pingHistory, pingMs)
		if len(s.pingHistory) > 10 {
			s.pingHistory = s.pingHistory[len(s.pingHistory)-10:]
		}
		var sum int64
		for _, v := range s.pingHistory {
			sum += v
		}
		s.AvgPingMs = sum / int64(len(s.pingHistory))
		s.LastSeen = time.Now()
		s.Error = ""
		s.NodeID = nodeID
		s.NodeName = nodeName
		s.UptimeSec = int64(uptimeF)
		s.Hashrate = hashrate
		s.ActiveMiners = activeMiners
		s.SharesFwd = sharesFwd
		s.SharesAcc = sharesAcc
		s.SharesRej = sharesRej
		s.BestDiff = bestDiff
		s.BlocksFound = blocksFound
		s.NetworkDiff = netDiff
		s.PublicIP = publicIP
		s.LocalIPs = localIPs
		if coord != nil {
			s.Lat = coord.lat
			s.Lng = coord.lng
		}
		s.MaxConn = maxConn
		s.MaxConnPerIP = maxConnPerIP
		s.AuthTimeoutSec = authTimeoutSec
		s.LogLevel = logLevel
		s.TokenOK = tokenOK
	})
}

// FetchDetail fetches pairs and miners for a specific node on demand.
func (p *Poller) FetchDetail(entry NodeEntry) ([]PairSnapshot, []MinerSnapshot, error) {
	var pairs []PairSnapshot
	var miners []MinerSnapshot

	if resp, err := p.client.Get(entry.URL + "/api/pairs"); err == nil {
		json.NewDecoder(resp.Body).Decode(&pairs)
		resp.Body.Close()
	} else {
		return nil, nil, fmt.Errorf("pairs: %w", err)
	}

	if resp, err := p.client.Get(entry.URL + "/api/miners"); err == nil {
		json.NewDecoder(resp.Body).Decode(&miners)
		resp.Body.Close()
	}

	return pairs, miners, nil
}
