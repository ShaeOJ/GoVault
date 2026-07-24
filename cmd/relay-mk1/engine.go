package main

import (
	"crypto/rand"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"os"
	"time"

	"govault/internal/api"
	"govault/internal/coin"
	"govault/internal/config"
	"govault/internal/database"
	"govault/internal/logger"
	"govault/internal/miner"
	"govault/internal/node"
	"govault/internal/stratum"
	"govault/internal/upstream"

	"sync"
)

// Engine is the headless equivalent of the desktop app (app.go) without Wails.
// It runs the full GoVault mining engine — solo (against a coin node's RPC) or
// proxy (to an upstream pool), selected by config.MiningMode — and serves the
// monitoring dashboard from internal/api. Configuration is file-driven
// (config.json); no FirePool beacon, no edge.json.
type Engine struct {
	cfg *config.Config
	log *logger.Logger

	nodeClient *node.Client
	monitor    *node.ChainMonitor
	stratum    *stratum.Server
	upstream   *upstream.Client
	registry   *miner.Registry
	stats      *miner.StatsAggregator
	discovery  *miner.Discovery

	// svcMu guards stratum/upstream/monitor pointers.
	svcMu sync.RWMutex

	// Fleet power cache (queries each miner's AxeOS API; 30s TTL).
	fleetCache miner.FleetPowerStats
	fleetTime  time.Time
	fleetMu    sync.Mutex

	db     *database.DB
	buffer *database.Buffer

	networkDiff     float64
	networkHashrate float64
	blockHeight     int64
	netMu           sync.RWMutex

	stopStats     chan struct{}
	stopStatsOnce sync.Once

	apiServer *api.Server
	apiPort   int
	startedAt time.Time
	staticFS  embed.FS

	nodeID   string
	nodeName string
}

// NewEngine initialises the engine from config.json found next to the binary.
func NewEngine(staticFS embed.FS, apiPort int, logLevel string) (*Engine, error) {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("WARNING: config load failed (%v), using defaults\n", err)
		cfg = config.Defaults()
	}
	if logLevel != "" {
		cfg.App.LogLevel = logLevel
	}
	if apiPort <= 0 {
		apiPort = 8080
	}

	host, _ := os.Hostname()
	if host == "" {
		host = "govault-node"
	}

	return &Engine{
		cfg:       cfg,
		registry:  miner.NewRegistry(),
		stats:     miner.NewStatsAggregator(),
		discovery: miner.NewDiscovery(),
		stopStats: make(chan struct{}),
		staticFS:  staticFS,
		apiPort:   apiPort,
		nodeID:    generateNodeID(),
		nodeName:  host,
	}, nil
}

// Start brings up logger, DB, node client, the HTTP dashboard (always, so it
// serves even before mining is configured), the stats loop, and the stratum
// server in solo or proxy mode.
func (e *Engine) Start() error {
	e.startedAt = time.Now()

	log, err := logger.New(e.cfg.LogDir(), e.cfg.App.LogLevel)
	if err != nil {
		fmt.Printf("WARNING: logger init failed: %v\n", err)
	}
	e.log = log

	if db, err := database.Open(e.cfg.DBPath()); err != nil {
		if e.log != nil {
			e.log.Errorf("engine", "database open failed: %v (stats won't persist)", err)
		}
	} else {
		e.db = db
		e.buffer = database.NewBuffer(db)
		e.loadStatsFromDB()
	}

	e.nodeClient = node.NewClient(
		e.cfg.Node.Host, e.cfg.Node.Port,
		e.cfg.Node.Username, e.cfg.Node.Password, e.cfg.Node.UseSSL,
	)

	// HTTP dashboard first — always available.
	e.apiServer = api.NewServer(e.apiPort, e, e.staticFS, e.log)
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
			if e.log != nil {
				e.log.SetLevel(req.LogLevel)
			}
		}
		return e.cfg.Save()
	}
	e.apiServer.OnSetup = func(req api.SetupRequest) error { return e.applySetup(req) }
	e.apiServer.OnConfigUpdate = func(newCfg *config.Config) error { return e.applyConfig(newCfg) }
	e.apiServer.OnRestart = func() {
		if e.log != nil {
			e.log.Info("engine", "restart requested — exiting for supervisor restart")
		}
		go func() { e.Stop(); os.Exit(0) }()
	}
	if err := e.apiServer.Start(); err != nil {
		return fmt.Errorf("HTTP dashboard on port %d: %w", e.apiPort, err)
	}

	go e.statsLoop()

	// Auto-start mining if configured.
	if err := e.StartStratum(); err != nil {
		if e.log != nil {
			e.log.Errorf("engine", "stratum not started: %v (configure via dashboard :%d)", err, e.apiPort)
		} else {
			fmt.Printf("stratum not started: %v (configure via dashboard :%d)\n", err, e.apiPort)
		}
	}
	return nil
}

// Stop tears everything down and persists stats.
func (e *Engine) Stop() {
	e.stopStatsOnce.Do(func() { close(e.stopStats) })
	e.StopStratum()
	if e.buffer != nil {
		e.buffer.Stop()
	}
	e.saveCumulativeStats()
	if e.db != nil {
		e.db.Close()
	}
	if e.apiServer != nil {
		e.apiServer.Stop()
	}
	if e.cfg != nil {
		e.cfg.Save()
	}
	if e.log != nil {
		e.log.Info("engine", "govault-node stopping")
		e.log.Close()
	}
}

// === Stratum control ===

func (e *Engine) StartStratum() error {
	e.svcMu.RLock()
	srv := e.stratum
	e.svcMu.RUnlock()
	if srv != nil && srv.IsRunning() {
		return fmt.Errorf("stratum already running")
	}
	mode := e.cfg.MiningMode
	if mode == "" {
		mode = "solo"
	}
	if mode == "proxy" {
		return e.startProxy()
	}
	return e.startSolo()
}

func (e *Engine) startSolo() error {
	if e.cfg.Mining.PayoutAddress == "" {
		return fmt.Errorf("solo mode: payout address not configured")
	}
	coinDef := coin.Get(e.cfg.Mining.Coin)
	e.log.Infof("engine", "starting stratum (solo) for %s (%s)", coinDef.Name, coinDef.Symbol)

	srv := stratum.NewServer(&e.cfg.Stratum, &e.cfg.Mining, &e.cfg.Vardiff, e.nodeClient, e.log, coinDef)
	e.svcMu.Lock()
	e.stratum = srv
	e.svcMu.Unlock()
	e.wireStratumCallbacks()

	// Pre-fetch first template (quick client) so reconnecting miners get work fast.
	quick := node.NewQuickClient(e.cfg.Node.Host, e.cfg.Node.Port, e.cfg.Node.Username, e.cfg.Node.Password, e.cfg.Node.UseSSL)
	if tmpl, err := quick.GetBlockTemplate(coinDef.GBTRules); err != nil {
		e.log.Errorf("engine", "initial block template fetch failed: %v (miners wait for next poll)", err)
	} else {
		srv.NewBlockTemplate(tmpl)
		e.netMu.Lock()
		e.blockHeight = tmpl.Height
		e.netMu.Unlock()
	}

	if err := srv.Start(); err != nil {
		return err
	}

	mon := node.NewChainMonitor(e.nodeClient, 500*time.Millisecond, coinDef.GBTRules)
	mon.SetRefreshInterval(10 * time.Second)
	mon.OnNewBlock = func(tmpl *node.BlockTemplate) {
		e.svcMu.RLock()
		s := e.stratum
		e.svcMu.RUnlock()
		if s != nil {
			s.NewBlockTemplate(tmpl)
		}
		e.netMu.Lock()
		e.blockHeight = tmpl.Height
		e.netMu.Unlock()
	}
	mon.OnTemplateRefresh = func(tmpl *node.BlockTemplate) {
		e.svcMu.RLock()
		s := e.stratum
		e.svcMu.RUnlock()
		if s != nil {
			s.RefreshBlockTemplate(tmpl)
		}
	}
	mon.SetOnError(func(err error) { e.log.Errorf("engine", "chain monitor: %v", err) })

	e.svcMu.Lock()
	e.monitor = mon
	e.svcMu.Unlock()
	mon.Start()

	e.log.Info("engine", "stratum started (solo mode)")
	return nil
}

func (e *Engine) startProxy() error {
	p := e.cfg.Proxy
	if p.URL == "" {
		return fmt.Errorf("proxy mode: upstream URL not configured")
	}
	if p.WorkerName == "" {
		return fmt.Errorf("proxy mode: worker/wallet not configured")
	}
	password := p.Password
	if password == "" {
		password = "x"
	}
	// Shared proxy (matches the desktop app): the relay holds ONE upstream
	// connection authorized with the configured wallet/worker; every miner's
	// shares are forwarded through it. Jobs are broadcast to all miners, so a
	// device only needs a worker name and always gets work — robust, unlike
	// per-miner mode where a miner is rejected if its own pool connection fails.
	e.log.Infof("engine", "starting stratum (proxy, shared) → %s worker=%s", p.URL, p.WorkerName)

	uc := upstream.NewClient(p.URL, p.WorkerName, password, e.log)
	if err := uc.Connect(); err != nil {
		return fmt.Errorf("upstream connect: %w", err)
	}
	e.svcMu.Lock()
	e.upstream = uc
	e.svcMu.Unlock()

	coinDef := coin.Get(e.cfg.Mining.Coin)
	srv := stratum.NewServer(&e.cfg.Stratum, &e.cfg.Mining, &e.cfg.Vardiff, nil, e.log, coinDef)
	e.svcMu.Lock()
	e.stratum = srv
	e.svcMu.Unlock()

	var vMask uint32
	if uc.VersionRolling() && uc.VersionMask() != "" {
		if b, err := hex.DecodeString(uc.VersionMask()); err == nil && len(b) == 4 {
			vMask = binary.BigEndian.Uint32(b)
		}
	}
	srv.SetProxyMode(uc.Extranonce1(), uc.LocalEN2Size(), uc.PrefixBytes(), vMask)
	srv.SetUpstreamDifficulty(uc.UpstreamDifficulty())

	e.wireStratumCallbacks()

	uc.OnJob = func(params *upstream.JobParams) {
		e.svcMu.RLock()
		s := e.stratum
		e.svcMu.RUnlock()
		if s != nil {
			s.BroadcastUpstreamJob(params)
		}
		e.updateNetworkDiffFromNBits(params.NBits)
		if params.CleanJobs {
			e.netMu.Lock()
			e.blockHeight++
			e.netMu.Unlock()
		}
	}
	uc.OnDifficulty = func(diff float64) {
		e.svcMu.RLock()
		s := e.stratum
		e.svcMu.RUnlock()
		if s != nil {
			s.SetUpstreamDifficulty(diff)
		}
	}
	uc.OnDisconnect = func(err error) { e.log.Errorf("engine", "upstream disconnected: %v (reconnecting)", err) }
	srv.OnShareForward = func(workerName, jobID, fullEN2, ntime, nonce, versionBits string) (bool, string) {
		return uc.SubmitShare(workerName, jobID, fullEN2, ntime, nonce, versionBits)
	}

	if err := srv.Start(); err != nil {
		uc.Stop()
		e.svcMu.Lock()
		e.upstream = nil
		e.svcMu.Unlock()
		return err
	}
	if early := uc.DrainEarlyJob(); early != nil {
		srv.BroadcastUpstreamJob(early)
		e.updateNetworkDiffFromNBits(early.NBits)
	}
	if nbits := uc.LastNBits(); nbits != "" {
		e.updateNetworkDiffFromNBits(nbits)
	}
	e.log.Info("engine", "stratum started (proxy / shared)")
	return nil
}

func (e *Engine) wireStratumCallbacks() {
	e.stratum.OnMinerConnected = func(info stratum.MinerInfo) {
		e.registry.Register(miner.MinerInfo{
			ID: info.ID, WorkerName: info.WorkerName, UserAgent: info.UserAgent,
			IPAddress: info.IPAddress, ConnectedAt: info.ConnectedAt, CurrentDiff: info.CurrentDiff,
		})
		if e.db != nil {
			e.db.UpsertMinerSession(database.MinerSessionEntry{
				SessionID: info.ID, Worker: info.WorkerName, IPAddress: info.IPAddress, ConnectedAt: info.ConnectedAt.Unix(),
			})
		}
	}
	e.stratum.OnMinerDisconnected = func(id string) {
		e.registry.Unregister(id)
		e.stats.EvictSession(id)
		if e.db != nil {
			e.db.DisconnectMiner(id, time.Now().Unix())
		}
	}
	e.stratum.OnShareAccepted = func(minerID string, sessionDiff, actualDiff float64) {
		e.registry.RecordShare(minerID, actualDiff, true)
		e.stats.RecordShare(minerID, sessionDiff, true)
		e.stats.RecordBestDifficulty(actualDiff)
		if e.buffer != nil {
			e.buffer.AddShare(database.ShareEntry{
				Timestamp: time.Now().Unix(), MinerID: minerID, Difficulty: actualDiff, SessionDiff: sessionDiff, Accepted: true,
			})
		}
	}
	e.stratum.OnShareRejected = func(minerID string, reason string) {
		e.registry.RecordShare(minerID, 0, false)
		e.stats.RecordShare(minerID, 0, false)
		if e.buffer != nil {
			e.buffer.AddShare(database.ShareEntry{
				Timestamp: time.Now().Unix(), MinerID: minerID, Accepted: false, RejectReason: reason,
			})
		}
	}
	e.stratum.OnBlockFound = func(hash string, height int64, accepted bool) {
		if accepted {
			e.stats.RecordBlock()
			if e.db != nil {
				e.db.InsertBlock(database.BlockEntry{Timestamp: time.Now().Unix(), Height: height, Hash: hash})
			}
			e.log.Infof("engine", "BLOCK ACCEPTED! hash=%s height=%d", hash, height)
		} else {
			e.log.Warnf("engine", "block candidate rejected hash=%s height=%d", hash, height)
		}
	}
	e.stratum.LookupWorkerDiff = func(workerName string) float64 {
		if e.db != nil {
			diff, _ := e.db.GetWorkerDiff(workerName)
			return diff
		}
		return 0
	}
	e.stratum.OnDiffChanged = func(workerName string, diff float64) {
		if e.db != nil && workerName != "" {
			e.db.SaveWorkerDiff(workerName, diff)
		}
	}
}

func (e *Engine) StopStratum() error {
	e.svcMu.Lock()
	uc := e.upstream
	e.upstream = nil
	mon := e.monitor
	e.monitor = nil
	srv := e.stratum
	e.stratum = nil
	e.svcMu.Unlock()

	if uc != nil {
		uc.Stop()
	}
	if mon != nil {
		mon.Stop()
	}
	if srv != nil {
		srv.Stop()
	}
	e.stats.ClearShareRecords()
	e.registry.Clear()
	return nil
}

// applySetup reconfigures from a dashboard setup request. The setup wizard is
// proxy-oriented (endpoints with an upstream URL); solo config is edited in
// config.json directly.
func (e *Engine) applySetup(req api.SetupRequest) error {
	if len(req.Endpoints) == 0 {
		return fmt.Errorf("no endpoints in setup request")
	}
	ep := req.Endpoints[0]
	e.cfg.MiningMode = "proxy"
	e.cfg.Proxy.URL = ep.UpstreamURL
	e.cfg.Proxy.WorkerName = ep.WorkerName
	e.cfg.Proxy.Password = ep.Password
	if ep.CoinID != "" {
		e.cfg.Mining.Coin = ep.CoinID
	}
	if ep.LocalPort > 0 {
		e.cfg.Stratum.Port = ep.LocalPort
	}
	if err := e.cfg.Save(); err != nil {
		return err
	}
	e.StopStratum()
	return e.StartStratum()
}

// applyConfig persists a full config from the web setup/settings form and
// restarts mining with it. Blank password fields keep the existing secret;
// App fields other than log level are preserved.
func (e *Engine) applyConfig(newCfg *config.Config) error {
	if newCfg.Node.Password == "" {
		newCfg.Node.Password = e.cfg.Node.Password
	}
	if newCfg.Proxy.Password == "" {
		newCfg.Proxy.Password = e.cfg.Proxy.Password
	}
	logLevel := newCfg.App.LogLevel
	newCfg.App = e.cfg.App
	if logLevel != "" {
		newCfg.App.LogLevel = logLevel
	}
	if err := newCfg.Validate(); err != nil {
		return err
	}
	nodeChanged := newCfg.Node != e.cfg.Node

	if err := e.cfg.Update(newCfg); err != nil {
		return err
	}
	if e.log != nil {
		e.log.SetLevel(e.cfg.App.LogLevel)
	}
	if nodeChanged && e.nodeClient != nil {
		e.nodeClient.Close()
		e.nodeClient = node.NewClient(
			e.cfg.Node.Host, e.cfg.Node.Port,
			e.cfg.Node.Username, e.cfg.Node.Password, e.cfg.Node.UseSSL,
		)
	}
	e.StopStratum()
	if err := e.StartStratum(); err != nil {
		// Config is already persisted; a start failure (node/pool not reachable
		// yet) shouldn't read as "save failed". Log it and let the dashboard
		// show the mining state — it'll start on the next reboot or edit.
		if e.log != nil {
			e.log.Errorf("engine", "config saved; mining not started yet: %v", err)
		}
	}
	return nil
}

// === api.StatsProvider ===

func (e *Engine) GetDashboardStats() miner.DashboardStats {
	e.svcMu.RLock()
	srv := e.stratum
	e.svcMu.RUnlock()

	active := 0
	running := false
	proxy := false
	if srv != nil {
		active = srv.SessionCount()
		running = srv.IsRunning()
		proxy = srv.IsProxyMode()
	}
	e.netMu.RLock()
	netDiff, netHash, height := e.networkDiff, e.networkHashrate, e.blockHeight
	e.netMu.RUnlock()

	ds := e.stats.GetDashboardStats(active, netDiff, netHash, height, running)
	ds.MiningMode = e.cfg.MiningMode
	if proxy {
		diag := srv.GetProxyDiagnostics()
		ds.UpstreamDiff = diag.UpstreamDiff
		ds.ProxySharesFwd = diag.SharesFwd
		ds.ProxySharesAccepted = diag.SharesAccepted
		ds.ProxySharesRejected = diag.SharesRejected
	}
	return ds
}

func (e *Engine) GetMiners() []miner.MinerInfo {
	miners := e.registry.GetAll()
	e.svcMu.RLock()
	srv := e.stratum
	e.svcMu.RUnlock()

	var live map[string]stratum.MinerInfo
	if srv != nil && srv.IsRunning() {
		sessions := srv.GetSessions()
		live = make(map[string]stratum.MinerInfo, len(sessions))
		for _, s := range sessions {
			live[s.ID] = s
		}
	}
	for i := range miners {
		miners[i].Hashrate = e.stats.EstimateMinerHashrate(miners[i].ID)
		if l, ok := live[miners[i].ID]; ok {
			miners[i].CurrentDiff = l.CurrentDiff
		}
	}
	return miners
}

func (e *Engine) GetPairStatus() []api.PairStatus {
	e.svcMu.RLock()
	srv := e.stratum
	uc := e.upstream
	e.svcMu.RUnlock()

	coinDef := coin.Get(e.cfg.Mining.Coin)
	mode := e.cfg.MiningMode
	if mode == "" {
		mode = "solo"
	}
	ps := api.PairStatus{
		Coin:      coinDef.Symbol,
		CoinID:    coinDef.CoinID,
		Mode:      mode,
		LocalPort: e.cfg.Stratum.Port,
	}
	if srv != nil {
		ps.Running = srv.IsRunning()
		ps.MinerCount = srv.SessionCount()
	}
	e.netMu.RLock()
	ps.NetworkDiff = e.networkDiff
	ps.BlockHeight = e.blockHeight
	e.netMu.RUnlock()
	ps.TotalHashrate = e.stats.EstimateHashrate()
	if mode == "proxy" {
		ps.UpstreamURL = e.cfg.Proxy.URL
		if uc != nil {
			ps.Connected = uc.IsConnected()
			ps.Authorized = uc.IsAuthorized()
			ps.UpstreamDiff = uc.UpstreamDifficulty()
		}
	}
	return []api.PairStatus{ps}
}

// GetHashrateHistory feeds the dashboard hashrate chart (period: 1h|6h|24h|7d).
func (e *Engine) GetHashrateHistory(period string) []miner.HashratePoint {
	return e.stats.GetHashrateHistory(period)
}

// GetFleetOverview queries connected miners' AxeOS APIs for power draw and
// derives efficiency (J/TH) and estimated daily cost. Cached 30s so the
// dashboard poll doesn't hammer the miners.
func (e *Engine) GetFleetOverview() map[string]interface{} {
	e.svcMu.RLock()
	srv := e.stratum
	e.svcMu.RUnlock()

	var ips []string
	if srv != nil && srv.IsRunning() {
		seen := make(map[string]bool)
		for _, s := range srv.GetSessions() {
			host, _, err := net.SplitHostPort(s.IPAddress)
			if err != nil {
				host = s.IPAddress
			}
			if host != "" && !seen[host] {
				seen[host] = true
				ips = append(ips, host)
			}
		}
	}

	e.fleetMu.Lock()
	if time.Since(e.fleetTime) > 30*time.Second {
		e.fleetMu.Unlock()
		power := e.discovery.QueryFleetPower(ips)
		e.fleetMu.Lock()
		e.fleetCache = power
		e.fleetTime = time.Now()
	}
	power := e.fleetCache
	e.fleetMu.Unlock()

	hashrate := e.stats.EstimateHashrate()
	cost := e.cfg.App.ElectricityCost
	daily := 0.0
	if power.TotalWatts > 0 && cost > 0 {
		daily = power.TotalWatts * 24 / 1000 * cost
	}
	eff := 0.0
	if power.TotalWatts > 0 && hashrate > 0 {
		eff = power.TotalWatts / (hashrate / 1e12)
	}
	return map[string]interface{}{
		"totalWatts":      power.TotalWatts,
		"responded":       power.Responded,
		"queried":         power.Queried,
		"efficiency":      eff,
		"dailyCost":       daily,
		"electricityCost": cost,
	}
}

func (e *Engine) GetConfig() *config.Config { return e.cfg }
func (e *Engine) Uptime() time.Duration     { return time.Since(e.startedAt) }
func (e *Engine) NodeID() string            { return e.nodeID }
func (e *Engine) NodeName() string          { return e.nodeName }

// === internal loops ===

func (e *Engine) statsLoop() {
	tick := time.NewTicker(2 * time.Second)
	defer tick.Stop()
	hashrate := time.NewTicker(60 * time.Second)
	defer hashrate.Stop()
	nodeRefresh := time.NewTicker(30 * time.Second)
	defer nodeRefresh.Stop()
	cumulative := time.NewTicker(5 * time.Minute)
	defer cumulative.Stop()
	prune := time.NewTicker(1 * time.Hour)
	defer prune.Stop()

	for {
		select {
		case <-e.stopStats:
			return
		case <-tick.C:
			// stats are pulled by the dashboard via /api/stats; nothing to push.
		case <-hashrate.C:
			hr := e.stats.EstimateHashrate()
			e.stats.RecordHashrate(hr)
			if e.db != nil {
				e.db.InsertHashrate(time.Now().Unix(), hr)
			}
			for _, m := range e.registry.GetAll() {
				e.registry.UpdateHashrate(m.ID, e.stats.EstimateMinerHashrate(m.ID))
			}
		case <-cumulative.C:
			e.saveCumulativeStats()
		case <-prune.C:
			e.pruneOldData()
		case <-nodeRefresh.C:
			e.refreshNodeInfo()
		}
	}
}

func (e *Engine) refreshNodeInfo() {
	if e.cfg.MiningMode == "proxy" || e.nodeClient == nil {
		return
	}
	info, err := e.nodeClient.GetMiningInfo()
	if err != nil {
		return
	}
	algo := coin.Get(e.cfg.Mining.Coin).MiningAlgo
	e.netMu.Lock()
	e.blockHeight = info.Blocks
	if algo != "" && len(info.Difficulties) > 0 {
		if d, ok := info.Difficulties[algo]; ok {
			e.networkDiff = d
		}
		if h, ok := info.NetworkHashesPSs[algo]; ok {
			e.networkHashrate = h
		}
	} else {
		e.networkDiff = info.Difficulty
		e.networkHashrate = info.NetworkHashPS
	}
	e.netMu.Unlock()
}

func (e *Engine) updateNetworkDiffFromNBits(nbitsHex string) {
	if nbitsHex == "" {
		return
	}
	if b, err := hex.DecodeString(nbitsHex); err != nil || len(b) != 4 {
		return
	}
	target := stratum.CompactToBig(nbitsHex)
	if target.Sign() <= 0 {
		return
	}
	nd := new(big.Float).SetInt(stratum.Pdiff1Target())
	nd.Quo(nd, new(big.Float).SetInt(target))
	f, _ := nd.Float64()
	e.netMu.Lock()
	e.networkDiff = f
	e.netMu.Unlock()
}

func (e *Engine) loadStatsFromDB() {
	if e.db == nil {
		return
	}
	cum, err := e.db.LoadCumulativeStats()
	if err != nil {
		return
	}
	since := time.Now().Add(-7 * 24 * time.Hour).Unix()
	history, _ := e.db.LoadHashrateHistory(since)
	var points []miner.HashratePoint
	for _, h := range history {
		points = append(points, miner.HashratePoint{Timestamp: h.Timestamp, Hashrate: h.Hashrate})
	}
	e.stats.LoadFromDB(cum.TotalAccepted, cum.TotalRejected, cum.BlocksFound, cum.BestDifficulty, points)
}

func (e *Engine) saveCumulativeStats() {
	if e.db == nil {
		return
	}
	accepted, rejected, blocks, best := e.stats.GetCumulativeStats()
	e.db.SaveCumulativeStats(database.CumulativeStats{
		TotalAccepted: accepted, TotalRejected: rejected, BestDifficulty: best, BlocksFound: blocks,
	})
}

func (e *Engine) pruneOldData() {
	if e.db == nil {
		return
	}
	maxAge := 30 * 24 * time.Hour
	e.db.PruneShares(maxAge)
	e.db.PruneHashrate(maxAge)
}

func generateNodeID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
