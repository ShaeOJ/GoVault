package main

import (
	"bytes"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"govault/internal/api"
	"govault/internal/coin"
	"govault/internal/config"
	"govault/internal/database"
	"govault/internal/logger"
	"govault/internal/miner"
	"govault/internal/stratum"
	"govault/internal/upstream"
)

// proxyPair manages one upstream-pool ↔ local-stratum connection.
type proxyPair struct {
	ep      EdgeEndpoint
	coinDef *coin.CoinDef

	mu       sync.RWMutex
	upstream *upstream.Client
	srv      *stratum.Server

	netMu   sync.RWMutex
	netDiff float64
	height  int64
}

func (p *proxyPair) sessionCount() int {
	p.mu.RLock()
	srv := p.srv
	p.mu.RUnlock()
	if srv == nil {
		return 0
	}
	return srv.SessionCount()
}

// Engine orchestrates all edge node subsystems: multiple upstream pool connections,
// local stratum servers (one per coin/mode), stats aggregation, and HTTP dashboard.
// It is the headless equivalent of app.go — no Wails dependency.
type Engine struct {
	cfg     *config.Config
	edgeCfg *EdgeConfig
	log     *logger.Logger

	registry *miner.Registry
	stats    *miner.StatsAggregator

	pairs []*proxyPair // set at Start(), read-only after

	db     *database.DB
	buffer *database.Buffer

	stopStats     chan struct{}
	stopStatsOnce sync.Once

	apiServer *api.Server
	startedAt time.Time
	staticFS  embed.FS
}

// NewEngine initialises the engine from config files found next to the binary.
func NewEngine(staticFS embed.FS, apiPort int, logLevelOverride, adminTokenOverride string) (*Engine, error) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("WARNING: config load failed (%v), using defaults\n", err)
		cfg = config.Defaults()
	}

	if logLevelOverride != "" {
		cfg.App.LogLevel = logLevelOverride
	}

	edgeCfg := LoadEdgeConfig()
	if apiPort > 0 {
		edgeCfg.APIPort = apiPort
	}
	if adminTokenOverride != "" {
		edgeCfg.AdminToken = adminTokenOverride
	}

	return &Engine{
		cfg:       cfg,
		edgeCfg:   edgeCfg,
		registry:  miner.NewRegistry(),
		stats:     miner.NewStatsAggregator(),
		stopStats: make(chan struct{}),
		staticFS:  staticFS,
	}, nil
}

// Start brings up all subsystems in order:
//  1. Logger
//  2. HTTP dashboard (always — serves setup wizard if unconfigured)
//  3. If configured: database, proxy pairs, stats loop
func (e *Engine) Start() error {
	e.startedAt = time.Now()

	log, err := logger.New(e.cfg.LogDir(), e.cfg.App.LogLevel)
	if err != nil {
		fmt.Printf("WARNING: logger init failed: %v\n", err)
	}
	e.log = log

	// Start HTTP server first — always, regardless of configuration state.
	e.apiServer = api.NewServer(e.edgeCfg.APIPort, e, e.staticFS, e.log)
	e.apiServer.OnSetup = func(req api.SetupRequest) error {
		return e.applySetup(req)
	}

	e.apiServer.AdminToken = e.edgeCfg.AdminToken

	e.apiServer.OnSettings = func(req api.SettingsRequest) error {
		if req.MaxConn > 0 {
			e.cfg.Stratum.MaxConn = req.MaxConn
		}
		if req.MaxConnPerIP >= 0 {
			e.cfg.Stratum.MaxConnPerIP = req.MaxConnPerIP
		}
		if req.AuthTimeoutSec > 0 {
			e.cfg.Stratum.AuthTimeoutSec = req.AuthTimeoutSec
		}
		if req.LogLevel != "" {
			e.cfg.App.LogLevel = req.LogLevel
		}
		return e.cfg.Save()
	}

	e.apiServer.OnRestart = func() {
		e.log.Info("admin", "remote restart requested — shutting down for systemd restart")
		go func() {
			e.Stop()
			os.Exit(0)
		}()
	}

	e.apiServer.OnNginxSetup = func(domain string) (string, error) {
		return RunNginxSetup(domain, e.edgeCfg.APIPort, dataDirectory())
	}

	if err := e.apiServer.Start(); err != nil {
		return fmt.Errorf("HTTP dashboard on port %d: %w", e.edgeCfg.APIPort, err)
	}

	active := e.edgeCfg.ActiveEndpoints()
	if len(active) == 0 {
		// Setup mode: no endpoints configured yet.
		fmt.Printf("GoVault Edge Node — setup mode\n")
		fmt.Printf("Open the dashboard in your browser to complete setup:\n")
		printDashboardURLs(e.edgeCfg.APIPort)
		if e.log != nil {
			e.log.Infof("edge", "no endpoints configured — serving setup wizard on port %d", e.edgeCfg.APIPort)
		}
		return nil
	}

	e.apiServer.SetConfigured(true)
	e.log.Infof("edge", "GoVault Edge Node %q starting (%d endpoints)", e.edgeCfg.NodeName, len(active))

	if err := e.initDB(); err != nil {
		e.log.Errorf("edge", "database open failed: %v (stats won't persist)", err)
	}

	if err := e.startAllPairs(); err != nil {
		return err
	}

	go e.statsLoop()
	fmt.Printf("GoVault Edge Node running — dashboard:\n")
	printDashboardURLs(e.edgeCfg.APIPort)
	e.log.Infof("edge", "dashboard → http://0.0.0.0:%d", e.edgeCfg.APIPort)

	if e.edgeCfg.BeaconURL != "" {
		go e.beacon()
	}

	return nil
}

// initDB opens the database and loads persisted stats.
func (e *Engine) initDB() error {
	db, err := database.Open(e.cfg.DBPath())
	if err != nil {
		return err
	}
	e.db = db
	e.buffer = database.NewBuffer(db)
	e.loadStatsFromDB()
	e.log.Infof("edge", "database opened at %s", e.cfg.DBPath())
	return nil
}

// startAllPairs starts one upstream + local stratum server per active endpoint.
func (e *Engine) startAllPairs() error {
	active := e.edgeCfg.ActiveEndpoints()
	var started []*proxyPair
	for _, ep := range active {
		pair, err := e.startPair(ep)
		if err != nil {
			// Stop already-started pairs before returning the error.
			for _, p := range started {
				p.mu.Lock()
				uc, srv := p.upstream, p.srv
				p.upstream, p.srv = nil, nil
				p.mu.Unlock()
				if uc != nil {
					uc.Stop()
				}
				if srv != nil {
					srv.Stop()
				}
			}
			return fmt.Errorf("%s/%s on :%d: %w", ep.Coin, ep.Mode, ep.LocalPort, err)
		}
		started = append(started, pair)
		e.log.Infof("edge", "%s/%s  :%d → %s", ep.Coin, ep.Mode, ep.LocalPort, ep.UpstreamURL)
	}
	e.pairs = started
	return nil
}

// applySetup writes config files from a web setup request, then starts proxy pairs.
func (e *Engine) applySetup(req api.SetupRequest) error {
	// Build EdgeEndpoints from the request.
	endpoints := make([]EdgeEndpoint, 0, len(req.Endpoints))
	for _, ep := range req.Endpoints {
		if !ep.Enabled {
			continue
		}
		password := ep.Password
		if password == "" {
			password = "x"
		}
		endpoints = append(endpoints, EdgeEndpoint{
			Coin:        ep.Coin,
			CoinID:      ep.CoinID,
			Mode:        ep.Mode,
			LocalPort:   ep.LocalPort,
			UpstreamURL: ep.UpstreamURL,
			WorkerName:  ep.WorkerName,
			Password:    password,
			Enabled:     true,
		})
	}

	// Update in-memory edge config.
	e.edgeCfg.NodeName = req.NodeName
	e.edgeCfg.APIPort = req.APIPort
	e.edgeCfg.Endpoints = endpoints
	e.edgeCfg.BeaconURL = req.BeaconURL
	e.edgeCfg.SelfURL = req.SelfURL

	// Persist edge.json.
	saveEdgeConfig(edgeConfigPath(), e.edgeCfg)

	// Write config.json defaults if it doesn't exist yet.
	configPath := configFilePath()
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		if err := writeDefaultConfig(configPath); err != nil && e.log != nil {
			e.log.Errorf("edge", "write config.json: %v", err)
		}
	}

	// Open database now that we know data dir exists.
	if e.db == nil {
		if err := e.initDB(); err != nil && e.log != nil {
			e.log.Errorf("edge", "database open after setup: %v", err)
		}
	}

	// Start proxy pairs.
	if err := e.startAllPairs(); err != nil {
		return err
	}

	go e.statsLoop()
	e.apiServer.SetConfigured(true)
	if e.log != nil {
		e.log.Infof("edge", "setup complete — %d pair(s) running", len(e.pairs))
	}
	if e.edgeCfg.BeaconURL != "" {
		go e.beacon()
	}
	return nil
}

// Stop performs a graceful shutdown of all subsystems.
func (e *Engine) Stop() {
	e.stopStatsOnce.Do(func() { close(e.stopStats) })

	if e.apiServer != nil {
		e.apiServer.Stop()
	}

	for _, pair := range e.pairs {
		pair.mu.Lock()
		uc, srv := pair.upstream, pair.srv
		pair.upstream, pair.srv = nil, nil
		pair.mu.Unlock()
		if uc != nil {
			uc.Stop()
		}
		if srv != nil {
			srv.Stop()
		}
	}

	if e.buffer != nil {
		e.buffer.Stop()
	}
	e.saveCumulativeStats()
	if e.db != nil {
		e.db.Close()
	}
	if e.log != nil {
		e.log.Info("edge", "edge node stopped")
		e.log.Close()
	}
}

// startPair starts the local stratum server for one endpoint in per-miner
// pass-through mode. Each miner that connects gets their own dedicated upstream
// connection authenticated with their own credentials, so they appear as
// independent connections to the upstream pool (firepool shows each miner
// individually rather than a single edge-node connection).
func (e *Engine) startPair(ep EdgeEndpoint) (*proxyPair, error) {
	coinDef := coin.Get(ep.CoinID)
	pair := &proxyPair{ep: ep, coinDef: coinDef}

	// Per-pair copies of configs so each server has the right coin ID and port.
	stratumCfg := e.cfg.Stratum
	stratumCfg.Port = ep.LocalPort

	miningCfg := e.cfg.Mining
	miningCfg.Coin = ep.CoinID

	srv := stratum.NewServer(
		&stratumCfg,
		&miningCfg,
		&e.cfg.Vardiff,
		nil, // no local node — upstream pool provides jobs
		e.log,
		coinDef,
	)

	pair.mu.Lock()
	pair.srv = srv
	pair.mu.Unlock()

	// Enable per-miner pass-through mode (0 = use default 1fffe000 version mask).
	srv.SetPerMinerMode(0)

	// OnMinerConnect: called when a miner authorizes. Creates and connects a
	// dedicated upstream TCP connection for this specific miner using their own
	// wallet/worker credentials.
	srv.OnMinerConnect = func(session *stratum.Session, worker, password string) (*upstream.Client, error) {
		if password == "" {
			password = "x"
		}
		uc := upstream.NewClient(ep.UpstreamURL, worker, password, e.log)
		if err := uc.Connect(); err != nil {
			return nil, fmt.Errorf("upstream connect for %s: %w", worker, err)
		}
		e.log.Infof("edge", "[%s/%s] miner %s connected upstream (en1=%s en2=%d vroll=%v)",
			ep.Coin, ep.Mode, worker, uc.Extranonce1(), uc.LocalEN2Size(), uc.VersionRolling())

		// Update pair net-diff from this miner's first job notification.
		// Wrapped here so the engine callback can update pair-level display.
		origOnJob := uc.OnJob
		uc.OnJob = func(params *upstream.JobParams) {
			e.updatePairNetDiff(pair, params.NBits)
			if params.CleanJobs {
				pair.netMu.Lock()
				pair.height++
				pair.netMu.Unlock()
			}
			if origOnJob != nil {
				origOnJob(params)
			}
		}

		return uc, nil
	}

	e.wireCallbacks(pair)

	if err := srv.Start(); err != nil {
		return nil, fmt.Errorf("stratum start: %w", err)
	}

	e.log.Infof("edge", "[%s/%s] per-miner pass-through listening on :%d → %s",
		ep.Coin, ep.Mode, ep.LocalPort, ep.UpstreamURL)

	return pair, nil
}

// wireCallbacks sets stratum event callbacks that update registry, stats, and DB.
func (e *Engine) wireCallbacks(pair *proxyPair) {
	srv := pair.srv
	ep := pair.ep

	srv.OnMinerConnected = func(info stratum.MinerInfo) {
		e.registry.Register(miner.MinerInfo{
			ID:          info.ID,
			WorkerName:  info.WorkerName,
			UserAgent:   info.UserAgent,
			IPAddress:   info.IPAddress,
			ConnectedAt: info.ConnectedAt,
			CurrentDiff: info.CurrentDiff,
			Coin:        ep.Coin,
		})
		if e.db != nil {
			e.db.UpsertMinerSession(database.MinerSessionEntry{
				SessionID:   info.ID,
				Worker:      info.WorkerName,
				IPAddress:   info.IPAddress,
				ConnectedAt: info.ConnectedAt.Unix(),
			})
		}
		e.log.Infof("edge", "[%s/%s] miner connected: %s  ip=%s  ua=%s",
			ep.Coin, ep.Mode, info.WorkerName, info.IPAddress, info.UserAgent)
	}

	srv.OnMinerDisconnected = func(id string) {
		e.registry.Unregister(id)
		if e.db != nil {
			e.db.DisconnectMiner(id, time.Now().Unix())
		}
	}

	srv.OnShareAccepted = func(minerID string, sessionDiff, actualDiff float64) {
		e.registry.RecordShare(minerID, actualDiff, true)
		e.stats.RecordShare(minerID, sessionDiff, true)
		e.stats.RecordBestDifficulty(actualDiff)
		if e.buffer != nil {
			e.buffer.AddShare(database.ShareEntry{
				Timestamp:   time.Now().Unix(),
				MinerID:     minerID,
				Difficulty:  actualDiff,
				SessionDiff: sessionDiff,
				Accepted:    true,
			})
		}
	}

	srv.OnShareRejected = func(minerID string, reason string) {
		e.registry.RecordShare(minerID, 0, false)
		e.stats.RecordShare(minerID, 0, false)
		if e.buffer != nil {
			e.buffer.AddShare(database.ShareEntry{
				Timestamp:    time.Now().Unix(),
				MinerID:      minerID,
				Accepted:     false,
				RejectReason: reason,
			})
		}
	}

	srv.OnBlockFound = func(hash string, height int64, accepted bool) {
		if accepted {
			e.stats.RecordBlock()
			if e.db != nil {
				e.db.InsertBlock(database.BlockEntry{
					Timestamp: time.Now().Unix(),
					Height:    height,
					Hash:      hash,
				})
			}
			e.log.Infof("edge", "[%s/%s] BLOCK FOUND! hash=%s height=%d", ep.Coin, ep.Mode, hash, height)
		} else {
			e.log.Warnf("edge", "[%s/%s] block candidate rejected hash=%s", ep.Coin, ep.Mode, hash)
		}
	}

	srv.LookupWorkerDiff = func(workerName string) float64 {
		if e.db != nil {
			diff, _ := e.db.GetWorkerDiff(workerName)
			return diff
		}
		return 0
	}

	srv.OnDiffChanged = func(workerName string, diff float64) {
		if e.db != nil && workerName != "" {
			e.db.SaveWorkerDiff(workerName, diff)
		}
	}
}

// statsLoop runs background housekeeping.
func (e *Engine) statsLoop() {
	hashrateTick := time.NewTicker(60 * time.Second)
	defer hashrateTick.Stop()
	cumulativeTick := time.NewTicker(5 * time.Minute)
	defer cumulativeTick.Stop()
	pruneTick := time.NewTicker(1 * time.Hour)
	defer pruneTick.Stop()

	for {
		select {
		case <-e.stopStats:
			return
		case <-hashrateTick.C:
			hashrate := e.stats.EstimateHashrate()
			e.stats.RecordHashrate(hashrate)
			if e.db != nil {
				e.db.InsertHashrate(time.Now().Unix(), hashrate)
			}
			for _, m := range e.registry.GetAll() {
				hr := e.stats.EstimateMinerHashrate(m.ID)
				e.registry.UpdateHashrate(m.ID, hr)
			}
		case <-cumulativeTick.C:
			e.saveCumulativeStats()
		case <-pruneTick.C:
			e.pruneOldData()
		}
	}
}

func (e *Engine) updatePairNetDiff(pair *proxyPair, nbitsHex string) {
	if nbitsHex == "" {
		return
	}
	target := stratum.CompactToBig(nbitsHex)
	if target.Sign() <= 0 {
		return
	}
	pdiff1 := stratum.Pdiff1Target()
	f := new(big.Float).SetInt(pdiff1)
	f.Quo(f, new(big.Float).SetInt(target))
	nd, _ := f.Float64()
	pair.netMu.Lock()
	pair.netDiff = nd
	pair.netMu.Unlock()
}

func (e *Engine) loadStatsFromDB() {
	if e.db == nil {
		return
	}
	cumulative, err := e.db.LoadCumulativeStats()
	if err != nil {
		return
	}
	since := time.Now().Add(-7 * 24 * time.Hour).Unix()
	history, _ := e.db.LoadHashrateHistory(since)
	var points []miner.HashratePoint
	for _, h := range history {
		points = append(points, miner.HashratePoint{Timestamp: h.Timestamp, Hashrate: h.Hashrate})
	}
	e.stats.LoadFromDB(
		cumulative.TotalAccepted,
		cumulative.TotalRejected,
		cumulative.BlocksFound,
		cumulative.BestDifficulty,
		points,
	)
}

func (e *Engine) saveCumulativeStats() {
	if e.db == nil {
		return
	}
	accepted, rejected, blocks, bestDiff := e.stats.GetCumulativeStats()
	e.db.SaveCumulativeStats(database.CumulativeStats{
		TotalAccepted:  accepted,
		TotalRejected:  rejected,
		BestDifficulty: bestDiff,
		BlocksFound:    blocks,
	})
}

func (e *Engine) pruneOldData() {
	if e.db == nil {
		return
	}
	e.db.PruneShares(30 * 24 * time.Hour)
	e.db.PruneHashrate(30 * 24 * time.Hour)
}

// localIPs returns all non-loopback IPv4 addresses for this machine.
func localIPs() []string {
	var ips []string
	ifaces, err := net.Interfaces()
	if err != nil {
		return ips
	}
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, err := iface.Addrs()
		if err != nil {
			continue
		}
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip == nil || ip.To4() == nil {
				continue
			}
			ips = append(ips, ip.String())
		}
	}
	return ips
}

// beacon registers this edge node with the admin panel configured in BeaconURL.
// It retries with backoff until successful or the node shuts down.
func (e *Engine) beacon() {
	selfURL := e.edgeCfg.SelfURL
	if selfURL == "" {
		// Auto-detect public IP via a lightweight HTTP lookup.
		selfURL = e.detectSelfURL()
		if selfURL == "" {
			e.log.Errorf("edge", "beacon: could not determine self URL — set selfUrl in edge.json")
			return
		}
	}

	payload, _ := json.Marshal(map[string]string{
		"name":  e.edgeCfg.NodeName,
		"url":   selfURL,
		"token": e.edgeCfg.AdminToken,
	})

	adminURL := strings.TrimSuffix(e.edgeCfg.BeaconURL, "/") + "/api/nodes"
	client := &http.Client{Timeout: 10 * time.Second}

	backoff := 5 * time.Second
	for attempt := 1; attempt <= 10; attempt++ {
		resp, err := client.Post(adminURL, "application/json", bytes.NewReader(payload))
		if err != nil {
			e.log.Warnf("edge", "beacon: attempt %d failed: %v (retry in %v)", attempt, err, backoff)
		} else {
			body, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			if resp.StatusCode == 200 {
				e.log.Infof("edge", "beacon: registered with admin panel at %s as %q (self=%s)", adminURL, e.edgeCfg.NodeName, selfURL)
				return
			}
			// 400 with "already exists" is fine — we're already registered.
			if resp.StatusCode == 400 && strings.Contains(string(body), "already exists") {
				e.log.Infof("edge", "beacon: node %q already registered at admin panel", e.edgeCfg.NodeName)
				return
			}
			e.log.Warnf("edge", "beacon: attempt %d got HTTP %d: %s (retry in %v)", attempt, resp.StatusCode, string(body), backoff)
		}
		time.Sleep(backoff)
		if backoff < 60*time.Second {
			backoff *= 2
		}
	}
	e.log.Errorf("edge", "beacon: gave up registering with admin panel after 10 attempts")
}

// detectSelfURL attempts to determine the public-facing URL for this node.
// It tries ipify, then falls back to the first non-loopback local IP.
func (e *Engine) detectSelfURL() string {
	client := &http.Client{Timeout: 5 * time.Second}
	for _, endpoint := range []string{
		"https://api.ipify.org",
		"https://checkip.amazonaws.com",
	} {
		resp, err := client.Get(endpoint)
		if err != nil {
			continue
		}
		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			continue
		}
		ip := strings.TrimSpace(string(body))
		if net.ParseIP(ip) != nil {
			return fmt.Sprintf("http://%s:%d", ip, e.edgeCfg.APIPort)
		}
	}
	// Fall back to first non-loopback local IP
	for _, ip := range localIPs() {
		return fmt.Sprintf("http://%s:%d", ip, e.edgeCfg.APIPort)
	}
	return ""
}

// printDashboardURLs prints the dashboard URL for each local IP.
func printDashboardURLs(port int) {
	ips := localIPs()
	if len(ips) == 0 {
		fmt.Printf("  http://localhost:%d\n", port)
		return
	}
	for _, ip := range ips {
		fmt.Printf("  http://%s:%d\n", ip, port)
	}
}

// configFilePath returns the path to data/config.json next to the binary.
func configFilePath() string {
	exe, err := os.Executable()
	if err != nil {
		return filepath.Join("data", "config.json")
	}
	return filepath.Join(filepath.Dir(exe), "data", "config.json")
}

// writeDefaultConfig writes a minimal config.json with sensible defaults.
func writeDefaultConfig(path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	defaultCfg := map[string]interface{}{
		"miningMode": "proxy",
		"stratum": map[string]interface{}{
			"port":           3333,
			"maxConn":        500,
			"maxConnPerIP":   50,
			"authTimeoutSec": 30,
			"autoStart":      true,
		},
		"vardiff": map[string]interface{}{
			"minDiff":         0.001,
			"startDiff":       1000,
			"maxDiff":         0,
			"targetTimeSec":   15,
			"retargetTimeSec": 90,
			"variancePct":     30,
		},
		"app": map[string]interface{}{
			"logLevel":        "info",
			"electricityCost": 0.10,
		},
	}
	return writeJSON(path, defaultCfg)
}

// --- api.StatsProvider interface ---

func (e *Engine) GetDashboardStats() miner.DashboardStats {
	totalMiners := 0
	anyRunning := false
	var netDiff float64
	var height int64

	for _, pair := range e.pairs {
		totalMiners += pair.sessionCount()

		pair.mu.RLock()
		running := pair.srv != nil && pair.srv.IsRunning()
		pair.mu.RUnlock()
		if running {
			anyRunning = true
		}

		// Use the first pair that has a valid network diff.
		pair.netMu.RLock()
		if pair.netDiff > 0 && netDiff == 0 {
			netDiff = pair.netDiff
			height = pair.height
		}
		pair.netMu.RUnlock()
	}

	ds := e.stats.GetDashboardStats(totalMiners, netDiff, 0, height, anyRunning)
	ds.MiningMode = "proxy"

	// Aggregate proxy share counters across all pairs.
	var fwd, accepted, rejected uint64
	for _, pair := range e.pairs {
		pair.mu.RLock()
		srv := pair.srv
		pair.mu.RUnlock()
		if srv != nil && srv.IsProxyMode() {
			diag := srv.GetProxyDiagnostics()
			fwd += diag.SharesFwd
			accepted += diag.SharesAccepted
			rejected += diag.SharesRejected
		}
	}
	ds.ProxySharesFwd = fwd
	ds.ProxySharesAccepted = accepted
	ds.ProxySharesRejected = rejected

	return ds
}

func (e *Engine) GetMiners() []miner.MinerInfo {
	miners := e.registry.GetAll()

	// Build a live-session map across all pairs.
	live := make(map[string]stratum.MinerInfo)
	for _, pair := range e.pairs {
		pair.mu.RLock()
		srv := pair.srv
		pair.mu.RUnlock()
		if srv != nil && srv.IsRunning() {
			for _, s := range srv.GetSessions() {
				live[s.ID] = s
			}
		}
	}

	for i := range miners {
		miners[i].Hashrate = e.stats.EstimateMinerHashrate(miners[i].ID)
		if s, ok := live[miners[i].ID]; ok {
			miners[i].CurrentDiff = s.CurrentDiff
		}
	}
	return miners
}

func (e *Engine) GetPairStatus() []api.PairStatus {
	result := make([]api.PairStatus, 0, len(e.pairs))
	for _, pair := range e.pairs {
		pair.mu.RLock()
		uc := pair.upstream
		srv := pair.srv
		pair.mu.RUnlock()

		pair.netMu.RLock()
		nd := pair.netDiff
		h := pair.height
		pair.netMu.RUnlock()

		ps := api.PairStatus{
			Coin:        pair.ep.Coin,
			CoinID:      pair.ep.CoinID,
			Mode:        pair.ep.Mode,
			LocalPort:   pair.ep.LocalPort,
			UpstreamURL: pair.ep.UpstreamURL,
			NetworkDiff: nd,
			BlockHeight: h,
		}
		if srv != nil {
			ps.MinerCount = srv.SessionCount()
			ps.Running = srv.IsRunning()
			for _, s := range srv.GetSessions() {
				ps.TotalHashrate += e.stats.EstimateMinerHashrate(s.ID)
			}
		}
		if uc != nil {
			ps.Connected = uc.IsConnected()
			ps.Authorized = uc.IsAuthorized()
			ps.UpstreamDiff = uc.UpstreamDifficulty()
		} else {
			// Per-miner mode: no shared upstream — report connected when miners are active.
			ps.Connected = ps.MinerCount > 0
			ps.Authorized = ps.MinerCount > 0
		}
		result = append(result, ps)
	}
	return result
}

func (e *Engine) GetConfig() *config.Config { return e.cfg }
func (e *Engine) Uptime() time.Duration     { return time.Since(e.startedAt) }
func (e *Engine) NodeID() string            { return e.edgeCfg.NodeID }
func (e *Engine) NodeName() string          { return e.edgeCfg.NodeName }
func (e *Engine) IsRunning() bool {
	for _, pair := range e.pairs {
		pair.mu.RLock()
		running := pair.srv != nil && pair.srv.IsRunning()
		pair.mu.RUnlock()
		if running {
			return true
		}
	}
	return false
}
