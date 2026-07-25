package main

import (
	"crypto/rand"
	"embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math/big"
	"net"
	"os"
	"strconv"
	"strings"
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

	// Per-miner AxeOS telemetry cache, keyed by IP (30s TTL). Feeds both the
	// fleet overview and the extended miner stats.
	fleetCache map[string]miner.MinerTelemetry
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
	// RELAY_STATIC_DIR (set by relay-supervise to /boot/www when present) lets the
	// dashboard be hot-swapped over SSH without rebuilding the binary.
	e.apiServer.StaticDir = os.Getenv("RELAY_STATIC_DIR")
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
	// Mark configured if config.json is complete, so a reboot goes straight to
	// the dashboard instead of re-showing the first-run setup — even if mining
	// hasn't started yet (e.g. the node/pool is briefly unreachable).
	if e.isConfigured() {
		e.apiServer.SetConfigured(true)
	}
	return nil
}

// isConfigured reports whether config.json holds a usable mining config.
func (e *Engine) isConfigured() bool {
	if e.cfg.MiningMode == "proxy" {
		return e.cfg.Proxy.URL != ""
	}
	return e.cfg.Mining.PayoutAddress != ""
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
	if p.PassThrough {
		return e.startProxyPassThrough()
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

// startProxyPassThrough runs true per-miner pass-through: each connecting miner
// gets its OWN upstream connection authorized with the miner's own worker name,
// so the pool sees every worker separately and vardiffs each device. Contrast
// startProxy (shared), where all miners are aggregated under one pool worker.
func (e *Engine) startProxyPassThrough() error {
	p := e.cfg.Proxy
	e.log.Infof("engine", "starting stratum (proxy, pass-through) → %s (per-miner worker identity)", p.URL)

	coinDef := coin.Get(e.cfg.Mining.Coin)
	srv := stratum.NewServer(&e.cfg.Stratum, &e.cfg.Mining, &e.cfg.Vardiff, nil, e.log, coinDef)
	e.svcMu.Lock()
	e.stratum = srv
	e.svcMu.Unlock()

	// 0 = default version mask (1fffe000); the per-miner upstream negotiates its
	// own rolling mask on connect.
	srv.SetPerMinerMode(0)

	// OnMinerSubscribe: when a miner subscribes, open its dedicated upstream and
	// subscribe (no auth yet) so the POOL's extranonce1/size are delivered in the
	// miner's subscribe response. This is what makes set_extranonce-blind firmware
	// (e.g. LuckyMiner v1.0.0) work in pass-through — they never see a late
	// mining.set_extranonce, they get the real values up front. Password/worker
	// aren't known yet, so authorize with the configured wallet as a placeholder;
	// OnMinerAuthorizeUpstream re-authorizes with the miner's own worker.
	pw := p.Password
	if pw == "" {
		pw = "x"
	}
	srv.OnMinerSubscribe = func(session *stratum.Session) (*upstream.Client, error) {
		uc := upstream.NewClient(p.URL, p.WorkerName, pw, e.log)
		if err := uc.ConnectNoAuth(); err != nil {
			return nil, fmt.Errorf("upstream subscribe: %w", err)
		}
		return uc, nil
	}

	// OnMinerAuthorizeUpstream: authorize the already-subscribed upstream with the
	// pool-facing username <wallet>.<miner-worker>, so the pool credits the
	// configured wallet AND tracks each device separately — without the wallet
	// needing to be baked into each miner's config. Names already wallet-qualified
	// pass through unchanged.
	srv.OnMinerAuthorizeUpstream = func(session *stratum.Session, worker, password string) (bool, error) {
		uc := session.DedicatedUpstream()
		if uc == nil {
			return false, fmt.Errorf("no dedicated upstream")
		}
		upWorker := worker
		if wallet := p.WorkerName; wallet != "" && !strings.HasPrefix(worker, wallet) {
			sub := strings.TrimSpace(worker)
			if sub == "" {
				sub = "0"
			}
			upWorker = wallet + "." + sub
		}
		wpass := password
		if wpass == "" {
			wpass = pw
		}
		ok, err := uc.AuthorizeWorker(upWorker, wpass)
		if err == nil && ok {
			e.log.Infof("engine", "miner %s → upstream worker %s (en1=%s en2=%d)",
				worker, upWorker, uc.Extranonce1(), uc.LocalEN2Size())
		}
		return ok, err
	}

	// Per-miner jobs are handled inside each session, so the engine learns the
	// network difficulty/height from this server-level hook rather than a single
	// upstream's OnJob (which doesn't exist in pass-through).
	srv.OnUpstreamJobInfo = func(nbits string, cleanJobs bool) {
		e.updateNetworkDiffFromNBits(nbits)
		if cleanJobs {
			e.netMu.Lock()
			e.blockHeight++
			e.netMu.Unlock()
		}
	}

	e.wireStratumCallbacks()

	if err := srv.Start(); err != nil {
		return err
	}
	e.log.Info("engine", "stratum started (proxy / pass-through)")
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
	cost := newCfg.App.ElectricityCost
	newCfg.App = e.cfg.App // preserve theme etc.
	if logLevel != "" {
		newCfg.App.LogLevel = logLevel
	}
	newCfg.App.ElectricityCost = cost // from the settings form ($/kWh; 0 = unset)
	if err := newCfg.Validate(); err != nil {
		return err
	}

	// Diff against the running config to decide how disruptive the change is.
	old := *e.cfg
	nodeChanged := newCfg.Node != old.Node
	// A stratum restart is only needed for changes that define the mining
	// session itself. Everything else applies without kicking miners.
	restart := newCfg.MiningMode != old.MiningMode ||
		newCfg.Mining.Coin != old.Mining.Coin ||
		newCfg.Stratum.Port != old.Stratum.Port ||
		newCfg.Proxy != old.Proxy ||
		nodeChanged
	payoutChanged := newCfg.Mining.PayoutAddress != old.Mining.PayoutAddress

	if err := e.cfg.Update(newCfg); err != nil {
		return err
	}
	if e.log != nil {
		e.log.SetLevel(e.cfg.App.LogLevel) // live — no restart
	}
	if nodeChanged && e.nodeClient != nil {
		e.nodeClient.Close()
		e.nodeClient = node.NewClient(
			e.cfg.Node.Host, e.cfg.Node.Port,
			e.cfg.Node.Username, e.cfg.Node.Password, e.cfg.Node.UseSSL,
		)
	}

	if restart {
		e.StopStratum()
		if err := e.StartStratum(); err != nil {
			// Config is already persisted; a start failure (node/pool not
			// reachable yet) shouldn't read as "save failed".
			if e.log != nil {
				e.log.Errorf("engine", "config saved; mining not started yet: %v", err)
			}
		}
	} else if payoutChanged {
		// Live payout update — miners keep hashing, stats intact.
		e.svcMu.RLock()
		srv := e.stratum
		e.svcMu.RUnlock()
		if srv != nil && srv.IsRunning() {
			srv.UpdatePayoutAddress(e.cfg.Mining.PayoutAddress)
		}
		if e.log != nil {
			e.log.Info("engine", "payout address updated live (no restart)")
		}
	} else if e.log != nil {
		e.log.Info("engine", "settings updated (no mining restart needed)")
	}

	if e.isConfigured() {
		e.apiServer.SetConfigured(true)
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
		ds.PoolPingMs = srv.AvgUpstreamLatencyMs()
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
	tel := e.fleetTelemetry()
	for i := range miners {
		miners[i].Hashrate = e.stats.EstimateMinerHashrate(miners[i].ID)
		if l, ok := live[miners[i].ID]; ok {
			miners[i].CurrentDiff = l.CurrentDiff
		}
		// Merge AxeOS telemetry by source IP.
		host, _, err := net.SplitHostPort(miners[i].IPAddress)
		if err != nil {
			host = miners[i].IPAddress
		}
		if t, ok := tel[host]; ok && t.Responded {
			miners[i].Telemetry = true
			miners[i].Temp = t.Temp
			miners[i].VrTemp = t.VrTemp
			miners[i].Power = t.Power
			miners[i].Voltage = t.Voltage
			miners[i].ASICModel = t.ASICModel
			miners[i].Firmware = t.Version
			miners[i].PingMs = t.PingMs
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

// connectedIPs returns the distinct source IPs of connected miners.
func (e *Engine) connectedIPs() []string {
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
	return ips
}

// fleetTelemetry returns cached per-miner AxeOS telemetry (30s TTL), querying
// the currently-connected miners when stale. Shared by GetFleetOverview and
// GetMiners so the miners are polled at most once per 30s.
func (e *Engine) fleetTelemetry() map[string]miner.MinerTelemetry {
	e.fleetMu.Lock()
	if e.fleetCache == nil || time.Since(e.fleetTime) > 30*time.Second {
		e.fleetMu.Unlock()
		tel := e.discovery.QueryFleetTelemetry(e.connectedIPs())
		e.fleetMu.Lock()
		e.fleetCache = tel
		e.fleetTime = time.Now()
	}
	tel := e.fleetCache
	e.fleetMu.Unlock()
	return tel
}

// GetFleetOverview derives total power, efficiency (J/TH) and est. daily cost
// from the per-miner telemetry.
func (e *Engine) GetFleetOverview() map[string]interface{} {
	tel := e.fleetTelemetry()
	var watts float64
	responded := 0
	for _, t := range tel {
		if t.Responded {
			responded++
			watts += t.Power
		}
	}
	hashrate := e.stats.EstimateHashrate()
	cost := e.cfg.App.ElectricityCost
	daily := 0.0
	if watts > 0 && cost > 0 {
		daily = watts * 24 / 1000 * cost
	}
	eff := 0.0
	if watts > 0 && hashrate > 0 {
		eff = watts / (hashrate / 1e12)
	}
	return map[string]interface{}{
		"totalWatts":      watts,
		"responded":       responded,
		"queried":         len(tel),
		"efficiency":      eff,
		"dailyCost":       daily,
		"electricityCost": cost,
	}
}

// fan control files shared with relayfan (the appliance fan daemon):
// relayfan reads fanControlFile each tick and writes fanStatusFile each tick.
const (
	fanControlFile = "/run/relay/fan.mode"
	fanStatusFile  = "/run/relay/fan.status"
)

// GetFanStatus reads relayfan's status file (mode/duty/tempC/rpm). If relayfan
// isn't running (fan-less unit, or non-appliance host), reports unavailable.
func (e *Engine) GetFanStatus() map[string]interface{} {
	b, err := os.ReadFile(fanStatusFile)
	if err != nil {
		return map[string]interface{}{"available": false}
	}
	var st map[string]interface{}
	if json.Unmarshal(b, &st) != nil {
		return map[string]interface{}{"available": false}
	}
	st["available"] = true
	return st
}

// SetFanMode validates a desired fan mode ("auto", "off", or "0".."100") and
// writes it to the control file for relayfan to pick up on its next tick.
func (e *Engine) SetFanMode(mode string) error {
	mode = strings.ToLower(strings.TrimSpace(mode))
	switch mode {
	case "auto", "off":
	default:
		n, err := strconv.Atoi(mode)
		if err != nil || n < 0 || n > 100 {
			return fmt.Errorf("fan mode must be auto, off, or a duty 0-100")
		}
	}
	if err := os.MkdirAll("/run/relay", 0o755); err != nil {
		return err
	}
	tmp := fanControlFile + ".tmp"
	if err := os.WriteFile(tmp, []byte(mode+"\n"), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, fanControlFile)
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
