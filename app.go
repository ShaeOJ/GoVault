package main

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/big"
	"net"
	"sync"
	"time"

	"govault/internal/coin"
	"govault/internal/config"
	"govault/internal/database"
	"govault/internal/logger"
	"govault/internal/miner"
	"govault/internal/node"
	"govault/internal/stratum"
	"govault/internal/upstream"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct bridges all backend subsystems to the Wails frontend.
type App struct {
	ctx context.Context

	config     *config.Config
	log        *logger.Logger
	nodeClient *node.Client
	monitor    *node.ChainMonitor
	stratum    *stratum.Server
	registry   *miner.Registry
	stats      *miner.StatsAggregator
	discovery  *miner.Discovery

	upstream *upstream.Client

	// Database persistence
	db     *database.DB
	buffer *database.Buffer

	// Cached node info
	networkDiff    float64
	networkHashrate float64
	blockHeight    int64

	// Fleet power cache (30s TTL)
	fleetPowerCache miner.FleetPowerStats
	fleetPowerTime  time.Time
	fleetPowerMu    sync.Mutex

	stopStats chan struct{}
}

// FleetOverview holds aggregated fleet stats for the Miners page.
type FleetOverview struct {
	TotalHashrate   float64 `json:"totalHashrate"`
	BlockChance     float64 `json:"blockChance"`
	TotalWatts      float64 `json:"totalWatts"`
	PowerResponded  int     `json:"powerResponded"`
	PowerQueried    int     `json:"powerQueried"`
	DailyCost       float64 `json:"dailyCost"`
	ElectricityCost float64 `json:"electricityCost"`
	Efficiency      float64 `json:"efficiency"` // J/TH
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{
		registry:  miner.NewRegistry(),
		stats:     miner.NewStatsAggregator(),
		discovery: miner.NewDiscovery(),
		stopStats: make(chan struct{}),
	}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Load config
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("WARNING: failed to load config: %v, using defaults\n", err)
		cfg = config.Defaults()
	}
	a.config = cfg

	// Initialize logger
	log, err := logger.New(cfg.LogDir(), cfg.App.LogLevel)
	if err != nil {
		fmt.Printf("WARNING: failed to init logger: %v\n", err)
	}
	a.log = log

	if a.log != nil {
		a.log.OnNewEntry = func(entry logger.LogEntry) {
			runtime.EventsEmit(a.ctx, "log:entry", entry)
		}
		a.log.Info("app", "GoVault starting up")
	}

	// Initialize database
	db, err := database.Open(cfg.DBPath())
	if err != nil {
		if a.log != nil {
			a.log.Errorf("app", "failed to open database: %v (stats will not persist)", err)
		}
	} else {
		a.db = db
		a.buffer = database.NewBuffer(db)
		a.loadStatsFromDB()
		if a.log != nil {
			a.log.Infof("app", "database opened at %s", cfg.DBPath())
		}
	}

	// Initialize node client
	a.nodeClient = node.NewClient(
		cfg.Node.Host,
		cfg.Node.Port,
		cfg.Node.Username,
		cfg.Node.Password,
		cfg.Node.UseSSL,
	)

	// Start stats ticker
	go a.statsLoop()

	// Auto-start stratum if configured
	canAutoStart := cfg.Mining.PayoutAddress != "" || cfg.MiningMode == "proxy"
	if cfg.Stratum.AutoStart && canAutoStart {
		go func() {
			time.Sleep(1 * time.Second) // Wait for frontend to be ready
			if err := a.StartStratum(); err != nil {
				a.log.Errorf("app", "auto-start stratum failed: %v", err)
			}
		}()
	}
}

// domReady is called after the frontend dom is ready.
func (a *App) domReady(ctx context.Context) {
	// Try connecting to node
	go a.refreshNodeInfo()
}

// shutdown is called when the app is closing.
func (a *App) shutdown(ctx context.Context) {
	close(a.stopStats)

	if a.upstream != nil {
		a.upstream.Stop()
	}
	if a.stratum != nil && a.stratum.IsRunning() {
		a.stratum.Stop()
	}
	if a.monitor != nil {
		a.monitor.Stop()
	}
	if a.buffer != nil {
		a.buffer.Stop()
	}
	a.saveCumulativeStats()
	if a.db != nil {
		a.db.Close()
	}
	if a.config != nil {
		a.config.Save()
	}
	if a.log != nil {
		a.log.Info("app", "GoVault shutting down")
		a.log.Close()
	}
}

// beforeClose is called before the app closes - return false to allow close.
func (a *App) beforeClose(ctx context.Context) bool {
	return false // Allow close
}

// === Stratum Control ===

func (a *App) StartStratum() error {
	if a.stratum != nil && a.stratum.IsRunning() {
		return fmt.Errorf("stratum server already running")
	}

	mode := a.config.MiningMode
	if mode == "" {
		mode = "solo"
	}

	if mode == "proxy" {
		return a.startProxy()
	}
	return a.startSolo()
}

func (a *App) startSolo() error {
	if a.config.Mining.PayoutAddress == "" {
		return fmt.Errorf("payout address not configured")
	}

	coinDef := coin.Get(a.config.Mining.Coin)
	a.log.Infof("app", "starting stratum (solo) for %s (%s)", coinDef.Name, coinDef.Symbol)

	a.stratum = stratum.NewServer(
		&a.config.Stratum,
		&a.config.Mining,
		&a.config.Vardiff,
		a.nodeClient,
		a.log,
		coinDef,
	)

	a.wireStratumCallbacks()

	// Pre-fetch the first block template BEFORE accepting miners so the
	// first reconnecting device gets work immediately.
	tmpl, err := a.nodeClient.GetBlockTemplate(coinDef.GBTRules)
	if err != nil {
		a.log.Errorf("app", "initial block template fetch failed: %v (miners will wait for next poll)", err)
	} else {
		a.stratum.NewBlockTemplate(tmpl)
		a.blockHeight = tmpl.Height
		a.log.Infof("app", "initial block template ready: height=%d", tmpl.Height)
	}

	if err := a.stratum.Start(); err != nil {
		return err
	}

	// Start chain monitor for ongoing block updates
	a.monitor = node.NewChainMonitor(a.nodeClient, 500*time.Millisecond, coinDef.GBTRules)
	a.monitor.SetRefreshInterval(10 * time.Second)
	a.monitor.OnNewBlock = func(tmpl *node.BlockTemplate) {
		a.log.Infof("app", "new block template: height=%d txns=%d", tmpl.Height, len(tmpl.Transactions))
		a.stratum.NewBlockTemplate(tmpl)
		a.blockHeight = tmpl.Height
		runtime.EventsEmit(a.ctx, "node:new-block", map[string]interface{}{
			"height": tmpl.Height,
		})
	}
	a.monitor.OnTemplateRefresh = func(tmpl *node.BlockTemplate) {
		a.stratum.RefreshBlockTemplate(tmpl)
	}
	a.monitor.SetOnError(func(err error) {
		a.log.Errorf("app", "chain monitor error: %v", err)
	})
	a.monitor.Start()

	a.log.Info("app", "stratum server started (solo mode)")
	return nil
}

func (a *App) startProxy() error {
	proxyCfg := a.config.Proxy
	if proxyCfg.URL == "" {
		return fmt.Errorf("proxy URL not configured")
	}
	if proxyCfg.WorkerName == "" {
		return fmt.Errorf("proxy worker name not configured")
	}

	password := proxyCfg.Password
	if password == "" {
		password = "x"
	}

	a.log.Infof("app", "starting stratum (proxy) → %s worker=%s", proxyCfg.URL, proxyCfg.WorkerName)

	// Connect to upstream pool
	uc := upstream.NewClient(proxyCfg.URL, proxyCfg.WorkerName, password, a.log)
	if err := uc.Connect(); err != nil {
		return fmt.Errorf("upstream connect: %w", err)
	}
	a.upstream = uc

	// Create stratum server with nil nodeClient (proxy mode)
	coinDef := coin.Get(a.config.Mining.Coin)
	a.stratum = stratum.NewServer(
		&a.config.Stratum,
		&a.config.Mining,
		&a.config.Vardiff,
		nil, // no local node
		a.log,
		coinDef,
	)

	// Configure proxy mode on stratum server
	// Parse upstream version-rolling mask so local miners are constrained to it.
	var vMask uint32
	if uc.VersionRolling() && uc.VersionMask() != "" {
		maskBytes, err := hex.DecodeString(uc.VersionMask())
		if err == nil && len(maskBytes) == 4 {
			vMask = binary.BigEndian.Uint32(maskBytes)
		}
	}
	a.stratum.SetProxyMode(uc.Extranonce1(), uc.LocalEN2Size(), uc.PrefixBytes(), vMask)
	a.stratum.SetUpstreamDifficulty(uc.UpstreamDifficulty())

	a.wireStratumCallbacks()

	// Wire upstream → stratum job relay
	uc.OnJob = func(params *upstream.JobParams) {
		a.stratum.BroadcastUpstreamJob(params)
		a.updateNetworkDiffFromNBits(params.NBits)
		if params.CleanJobs {
			a.blockHeight++
			runtime.EventsEmit(a.ctx, "node:new-block", map[string]interface{}{
				"height": a.blockHeight,
			})
		}
	}

	uc.OnDifficulty = func(diff float64) {
		a.stratum.SetUpstreamDifficulty(diff)
		// Log miner diffs for comparison with upstream
		sessions := a.stratum.GetSessions()
		for _, s := range sessions {
			a.log.Infof("proxy", "[DIFF-CMP] upstream=%.4f miner=%s localVardiff=%.4f", diff, s.WorkerName, s.CurrentDiff)
		}
	}

	uc.OnDisconnect = func(err error) {
		a.log.Errorf("app", "upstream disconnected: %v (reconnecting...)", err)
	}

	uc.OnReconnect = func() {
		// Upstream assigned a new EN1 — update stratum server and kick
		// all miners so they reconnect with new EN1-based sessions.
		var vMask uint32
		if uc.VersionRolling() && uc.VersionMask() != "" {
			maskBytes, _ := hex.DecodeString(uc.VersionMask())
			if len(maskBytes) == 4 {
				vMask = binary.BigEndian.Uint32(maskBytes)
			}
		}
		a.stratum.UpdateProxyState(uc.Extranonce1(), uc.LocalEN2Size(), uc.PrefixBytes(), vMask)
		a.stratum.SetUpstreamDifficulty(uc.UpstreamDifficulty())
	}

	// Wire share forwarding: stratum → upstream
	a.stratum.OnShareForward = func(workerName, jobID, fullEN2, ntime, nonce, versionBits string) (bool, string) {
		// Use upstream authorized worker name, not local miner name
		return uc.SubmitShare(uc.WorkerName(), jobID, fullEN2, ntime, nonce, versionBits)
	}

	if err := a.stratum.Start(); err != nil {
		uc.Stop()
		a.upstream = nil
		return err
	}

	// Replay any job notification received during the Connect() handshake
	// (before OnJob was wired). Without this, the first job is lost and
	// miners sit idle until the next upstream notification.
	if earlyJob := uc.DrainEarlyJob(); earlyJob != nil {
		a.log.Infof("app", "replaying early upstream job %s", earlyJob.JobID)
		a.stratum.BroadcastUpstreamJob(earlyJob)
		a.updateNetworkDiffFromNBits(earlyJob.NBits)
		if earlyJob.CleanJobs {
			a.blockHeight++
		}
	}

	// Seed initial network diff from nBits if we already have a job
	if nbits := uc.LastNBits(); nbits != "" {
		a.updateNetworkDiffFromNBits(nbits)
	}

	a.log.Info("app", "stratum server started (proxy mode)")
	return nil
}

// wireStratumCallbacks sets up callbacks shared by both solo and proxy modes.
func (a *App) wireStratumCallbacks() {
	a.stratum.OnMinerConnected = func(info stratum.MinerInfo) {
		a.registry.Register(miner.MinerInfo{
			ID:          info.ID,
			WorkerName:  info.WorkerName,
			UserAgent:   info.UserAgent,
			IPAddress:   info.IPAddress,
			ConnectedAt: info.ConnectedAt,
			CurrentDiff: info.CurrentDiff,
		})
		if a.db != nil {
			a.db.UpsertMinerSession(database.MinerSessionEntry{
				SessionID:   info.ID,
				Worker:      info.WorkerName,
				IPAddress:   info.IPAddress,
				ConnectedAt: info.ConnectedAt.Unix(),
			})
		}
		runtime.EventsEmit(a.ctx, "stratum:miner-connected", info)
	}

	a.stratum.OnMinerDisconnected = func(id string) {
		a.registry.Unregister(id)
		if a.db != nil {
			a.db.DisconnectMiner(id, time.Now().Unix())
		}
		runtime.EventsEmit(a.ctx, "stratum:miner-disconnected", map[string]string{"id": id})
	}

	a.stratum.OnShareAccepted = func(minerID string, sessionDiff, actualDiff float64) {
		a.registry.RecordShare(minerID, actualDiff, true)
		a.stats.RecordShare(minerID, sessionDiff, true)
		a.stats.RecordBestDifficulty(actualDiff)
		if a.buffer != nil {
			a.buffer.AddShare(database.ShareEntry{
				Timestamp:   time.Now().Unix(),
				MinerID:     minerID,
				Difficulty:  actualDiff,
				SessionDiff: sessionDiff,
				Accepted:    true,
			})
		}
		runtime.EventsEmit(a.ctx, "stratum:share-accepted", map[string]interface{}{
			"minerId":    minerID,
			"difficulty": actualDiff,
		})
	}

	a.stratum.OnShareRejected = func(minerID string, reason string) {
		a.registry.RecordShare(minerID, 0, false)
		a.stats.RecordShare(minerID, 0, false)
		if a.buffer != nil {
			a.buffer.AddShare(database.ShareEntry{
				Timestamp:    time.Now().Unix(),
				MinerID:      minerID,
				Accepted:     false,
				RejectReason: reason,
			})
		}
		runtime.EventsEmit(a.ctx, "stratum:share-rejected", map[string]interface{}{
			"minerId": minerID,
			"reason":  reason,
		})
	}

	a.stratum.OnBlockFound = func(hash string, height int64, accepted bool) {
		if accepted {
			a.stats.RecordBlock()
			if a.db != nil {
				a.db.InsertBlock(database.BlockEntry{
					Timestamp: time.Now().Unix(),
					Height:    height,
					Hash:      hash,
				})
			}
			runtime.EventsEmit(a.ctx, "stratum:block-found", map[string]interface{}{
				"hash":   hash,
				"height": height,
			})
			a.log.Infof("app", "BLOCK ACCEPTED! Hash: %s Height: %d", hash, height)
		} else {
			a.log.Warnf("app", "Block candidate rejected. Hash: %s Height: %d", hash, height)
		}
	}

	a.stratum.LookupWorkerDiff = func(workerName string) float64 {
		if a.db != nil {
			diff, _ := a.db.GetWorkerDiff(workerName)
			return diff
		}
		return 0
	}
	a.stratum.OnDiffChanged = func(workerName string, diff float64) {
		if a.db != nil && workerName != "" {
			a.db.SaveWorkerDiff(workerName, diff)
		}
	}
}

func (a *App) StopStratum() error {
	if a.upstream != nil {
		a.upstream.Stop()
		a.upstream = nil
	}
	if a.monitor != nil {
		a.monitor.Stop()
		a.monitor = nil
	}
	if a.stratum != nil {
		a.stratum.Stop()
	}

	// Clear stale state so restart doesn't misattribute hashrate.
	a.stats.ClearShareRecords()
	a.registry.Clear()

	a.log.Info("app", "stratum server stopped")
	return nil
}

func (a *App) IsStratumRunning() bool {
	return a.stratum != nil && a.stratum.IsRunning()
}

// === Dashboard ===

func (a *App) GetDashboardStats() miner.DashboardStats {
	activeMiners := 0
	if a.stratum != nil {
		activeMiners = a.stratum.SessionCount()
	}

	return a.stats.GetDashboardStats(
		activeMiners,
		a.networkDiff,
		a.networkHashrate,
		a.blockHeight,
		a.IsStratumRunning(),
	)
}

func (a *App) GetHashrateHistory(period string) []miner.HashratePoint {
	return a.stats.GetHashrateHistory(period)
}

// GetMinerHashrateHistory returns per-miner hashrate sparkline data (1h window, 2min buckets).
func (a *App) GetMinerHashrateHistory(minerID string) []miner.HashratePoint {
	if a.db == nil {
		return nil
	}
	since := time.Now().Add(-1 * time.Hour).Unix()
	entries, err := a.db.MinerHashrateHistory(minerID, since, 120) // 2-minute buckets
	if err != nil {
		if a.log != nil {
			a.log.Errorf("app", "miner hashrate history: %v", err)
		}
		return nil
	}
	points := make([]miner.HashratePoint, len(entries))
	for i, e := range entries {
		points[i] = miner.HashratePoint{
			Timestamp: e.Timestamp,
			Hashrate:  e.Hashrate,
		}
	}
	return points
}

// ClearRejectedShares removes all rejected share records from the database
// and resets the in-memory rejected counter.
func (a *App) ClearRejectedShares() (int64, error) {
	a.stats.ResetRejected()
	if a.db != nil {
		n, err := a.db.ClearRejectedShares()
		if err != nil {
			return 0, err
		}
		if a.log != nil {
			a.log.Infof("app", "cleared %d rejected shares from database", n)
		}
		return n, nil
	}
	return 0, nil
}

// === Miners ===

func (a *App) GetMiners() []miner.MinerInfo {
	miners := a.registry.GetAll()

	// Get live session data for current difficulty
	var liveSessions map[string]stratum.MinerInfo
	if a.stratum != nil && a.stratum.IsRunning() {
		sessions := a.stratum.GetSessions()
		liveSessions = make(map[string]stratum.MinerInfo, len(sessions))
		for _, s := range sessions {
			liveSessions[s.ID] = s
		}
	}

	for i := range miners {
		miners[i].Hashrate = a.stats.EstimateMinerHashrate(miners[i].ID)
		if live, ok := liveSessions[miners[i].ID]; ok {
			miners[i].CurrentDiff = live.CurrentDiff
		}
	}
	return miners
}

// GetFleetOverview returns aggregated stats for the Miners page fleet overview.
func (a *App) GetFleetOverview() FleetOverview {
	dash := a.GetDashboardStats()
	overview := FleetOverview{
		TotalHashrate:   dash.TotalHashrate,
		BlockChance:     dash.BlockChance,
		ElectricityCost: a.config.App.ElectricityCost,
	}

	// Collect unique miner IPs from active sessions
	var ips []string
	if a.stratum != nil && a.stratum.IsRunning() {
		seen := make(map[string]bool)
		for _, s := range a.stratum.GetSessions() {
			host, _, err := net.SplitHostPort(s.IPAddress)
			if err != nil {
				host = s.IPAddress
			}
			if !seen[host] {
				seen[host] = true
				ips = append(ips, host)
			}
		}
	}

	// Query fleet power with 30s cache
	a.fleetPowerMu.Lock()
	if time.Since(a.fleetPowerTime) > 30*time.Second {
		a.fleetPowerMu.Unlock()
		// Query outside mutex
		power := a.discovery.QueryFleetPower(ips)
		a.fleetPowerMu.Lock()
		// Double-check: only update if still stale
		if time.Since(a.fleetPowerTime) > 30*time.Second {
			a.fleetPowerCache = power
			a.fleetPowerTime = time.Now()
		}
	}
	power := a.fleetPowerCache
	a.fleetPowerMu.Unlock()

	overview.TotalWatts = power.TotalWatts
	overview.PowerResponded = power.Responded
	overview.PowerQueried = power.Queried

	// Daily cost: watts * 24h / 1000 * $/kWh
	if power.TotalWatts > 0 && overview.ElectricityCost > 0 {
		overview.DailyCost = (power.TotalWatts * 24 / 1000) * overview.ElectricityCost
	}

	// Efficiency: J/TH = watts / (hashrate in TH/s)
	if power.TotalWatts > 0 && dash.TotalHashrate > 0 {
		thPerSec := dash.TotalHashrate / 1e12
		if thPerSec > 0 {
			overview.Efficiency = power.TotalWatts / thPerSec
		}
	}

	return overview
}

// === Node ===

// DetectNode probes the local machine for a running node matching the given coin.
// Tries saved credentials, cookie auth, config file auth, and default credentials.
func (a *App) DetectNode(coinID string) map[string]interface{} {
	if a.log != nil {
		a.log.Infof("app", "detecting local node for coin: %s", coinID)
	}

	// Pass saved credentials so detection can verify the existing config first
	savedHost := a.config.Node.Host
	savedPort := a.config.Node.Port
	savedUser := a.config.Node.Username
	savedPass := a.config.Node.Password

	result := node.DetectLocalNode(coinID, savedHost, savedPort, savedUser, savedPass)
	if a.log != nil {
		if result.Found {
			a.log.Infof("app", "detected node: %s on %s:%d (auth: %s)", result.NodeVersion, result.Host, result.Port, result.AuthMethod)
		} else {
			for _, t := range result.Tried {
				a.log.Info("app", "detect: "+t)
			}
			a.log.Info("app", "no local node detected")
		}
	}
	return map[string]interface{}{
		"found":       result.Found,
		"host":        result.Host,
		"port":        result.Port,
		"username":    result.Username,
		"password":    result.Password,
		"authMethod":  result.AuthMethod,
		"nodeVersion": result.NodeVersion,
		"chain":       result.Chain,
		"blockHeight": result.BlockHeight,
		"syncPercent": result.SyncPercent,
		"tried":       result.Tried,
	}
}

func (a *App) TestNodeConnection(host string, port int, username, password string, useSSL bool) (map[string]interface{}, error) {
	client := node.NewQuickClient(host, port, username, password, useSSL)
	if err := client.Ping(); err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}

	info, err := client.GetBlockchainInfo()
	if err != nil {
		return nil, fmt.Errorf("get blockchain info: %w", err)
	}

	return map[string]interface{}{
		"connected":   true,
		"chain":       info.Chain,
		"blocks":      info.Blocks,
		"headers":     info.Headers,
		"difficulty":  info.Difficulty,
		"syncPercent": info.VerificationProgress * 100,
		"pruned":      info.Pruned,
	}, nil
}

func (a *App) GetNodeStatus() map[string]interface{} {
	connected := a.nodeClient.IsConnected()

	result := map[string]interface{}{
		"connected":       connected,
		"blockHeight":     a.blockHeight,
		"networkDifficulty": a.networkDiff,
		"networkHashrate": a.networkHashrate,
	}

	if connected {
		if info, err := a.nodeClient.GetBlockchainInfo(); err == nil {
			result["chain"] = info.Chain
			result["blocks"] = info.Blocks
			result["headers"] = info.Headers
			result["syncPercent"] = info.VerificationProgress * 100
			result["syncing"] = info.InitialBlockDownload
		}
		if netInfo, err := a.nodeClient.GetNetworkInfo(); err == nil {
			result["nodeVersion"] = netInfo.SubVersion
			result["connections"] = netInfo.Connections
		}
	}

	return result
}

// ConnectNode performs a quick connectivity check and returns node status.
// Uses a short timeout for fast interactive feedback.
func (a *App) ConnectNode() (map[string]interface{}, error) {
	quick := node.NewQuickClient(
		a.config.Node.Host,
		a.config.Node.Port,
		a.config.Node.Username,
		a.config.Node.Password,
		a.config.Node.UseSSL,
	)
	if err := quick.Ping(); err != nil {
		return nil, fmt.Errorf("connection failed: %w", err)
	}

	info, err := quick.GetBlockchainInfo()
	if err != nil {
		return nil, fmt.Errorf("get blockchain info: %w", err)
	}

	result := map[string]interface{}{
		"connected":         true,
		"chain":             info.Chain,
		"blocks":            info.Blocks,
		"headers":           info.Headers,
		"networkDifficulty": info.Difficulty,
		"syncPercent":       info.VerificationProgress * 100,
		"syncing":           info.InitialBlockDownload,
	}

	if netInfo, err := quick.GetNetworkInfo(); err == nil {
		result["nodeVersion"] = netInfo.SubVersion
		result["connections"] = netInfo.Connections
	}

	// Update main client state in background
	go func() {
		a.nodeClient.Ping()
		a.refreshNodeInfo()
	}()

	return result, nil
}

// === Config ===

func (a *App) GetConfig() *config.Config {
	return a.config
}

func (a *App) UpdateConfig(newCfg *config.Config) error {
	if err := newCfg.Validate(); err != nil {
		return err
	}

	// Check if coin changed — requires stratum restart
	coinChanged := a.config.Mining.Coin != newCfg.Mining.Coin

	// If node settings changed, recreate client
	oldNode := a.config.Node
	if err := a.config.Update(newCfg); err != nil {
		return err
	}

	if oldNode.Host != newCfg.Node.Host || oldNode.Port != newCfg.Node.Port ||
		oldNode.Username != newCfg.Node.Username || oldNode.Password != newCfg.Node.Password ||
		oldNode.UseSSL != newCfg.Node.UseSSL {
		a.nodeClient = node.NewClient(
			newCfg.Node.Host,
			newCfg.Node.Port,
			newCfg.Node.Username,
			newCfg.Node.Password,
			newCfg.Node.UseSSL,
		)
	}

	// If coin changed and stratum is running, stop it (requires restart with new coin params)
	if coinChanged && a.stratum != nil && a.stratum.IsRunning() {
		a.log.Infof("app", "coin changed to %s, stopping stratum server for restart", newCfg.Mining.Coin)
		a.StopStratum()
	}

	// Update payout address in stratum server
	if a.stratum != nil && a.stratum.IsRunning() {
		a.stratum.UpdatePayoutAddress(newCfg.Mining.PayoutAddress)
	}

	// Update log level
	if a.log != nil {
		a.log.SetLevel(newCfg.App.LogLevel)
	}

	return nil
}

func (a *App) ValidateAddress(addr string, coinID string) map[string]interface{} {
	result := map[string]interface{}{
		"address": addr,
		"valid":   false,
	}

	if len(addr) < 10 {
		return result
	}

	// Use provided coin ID, or fall back to config
	selectedCoin := a.config.Mining.Coin
	if coinID != "" {
		selectedCoin = coinID
	}

	coinDef := coin.Get(selectedCoin)
	valid, addrType := coin.ValidateAddress(coinDef, addr)
	if valid {
		result["valid"] = true
		result["type"] = addrType
		result["coin"] = coinDef.Symbol
	}

	return result
}

// GetCoinList returns metadata for all supported coins (for frontend dropdown).
func (a *App) GetCoinList() []map[string]interface{} {
	var list []map[string]interface{}
	for _, id := range coin.List() {
		c := coin.Get(id)
		list = append(list, map[string]interface{}{
			"id":             c.CoinID,
			"name":           c.Name,
			"symbol":         c.Symbol,
			"defaultRPCPort": c.DefaultRPCPort,
			"defaultRPCUser": c.DefaultRPCUsername,
			"segwit":         c.SegWit,
		})
	}
	return list
}

// === Discovery ===

func (a *App) ScanForMiners() []miner.DiscoveredMiner {
	a.log.Info("discovery", "starting network scan for miners")
	results := a.discovery.ScanSubnet()
	a.log.Infof("discovery", "found %d miners on network", len(results))
	return results
}

func (a *App) ConfigureMiner(ip string) error {
	localIP := miner.GetLocalIP()
	stratumURL := localIP
	stratumPort := a.config.Stratum.Port
	stratumUser := a.config.Mining.PayoutAddress

	return a.discovery.ConfigureMiner(ip, stratumURL, stratumPort, stratumUser)
}

// ReconnectMiners nudges disconnected AxeOS miners by PATCHing their
// stratum settings via HTTP, causing them to reconnect immediately.
func (a *App) ReconnectMiners() map[string]interface{} {
	if !a.IsStratumRunning() {
		return map[string]interface{}{
			"error": "stratum server is not running",
		}
	}

	if a.db == nil {
		return map[string]interface{}{
			"error": "database not available",
		}
	}

	// Get IPs that connected in the last 24h
	recentIPs, err := a.db.RecentMinerIPs()
	if err != nil {
		a.log.Errorf("app", "ReconnectMiners: failed to get recent IPs: %v", err)
		return map[string]interface{}{
			"error": fmt.Sprintf("failed to get recent miners: %v", err),
		}
	}

	// Build set of currently-connected IPs (strip port from session IP)
	connectedIPs := make(map[string]bool)
	if a.stratum != nil {
		for _, s := range a.stratum.GetSessions() {
			host, _, err := net.SplitHostPort(s.IPAddress)
			if err != nil {
				host = s.IPAddress // fallback if no port
			}
			connectedIPs[host] = true
		}
	}

	// Filter to only disconnected IPs
	var targets []string
	for _, ip := range recentIPs {
		if !connectedIPs[ip] {
			targets = append(targets, ip)
		}
	}

	if len(targets) == 0 {
		return map[string]interface{}{
			"attempted": 0,
			"success":   0,
			"message":   "all recent miners are already connected",
		}
	}

	// PATCH each disconnected miner concurrently
	localIP := miner.GetLocalIP()
	stratumPort := a.config.Stratum.Port
	stratumUser := a.config.Mining.PayoutAddress

	var mu sync.Mutex
	var wg sync.WaitGroup
	success := 0

	for _, ip := range targets {
		wg.Add(1)
		go func(ip string) {
			defer wg.Done()
			if err := a.discovery.ConfigureMiner(ip, localIP, stratumPort, stratumUser); err == nil {
				mu.Lock()
				success++
				mu.Unlock()
				a.log.Infof("app", "reconnect nudge sent to %s", ip)
			}
		}(ip)
	}
	wg.Wait()

	a.log.Infof("app", "reconnect miners: %d/%d succeeded", success, len(targets))

	return map[string]interface{}{
		"attempted": len(targets),
		"success":   success,
	}
}

func (a *App) GetStratumURL() string {
	localIP := miner.GetLocalIP()
	return fmt.Sprintf("stratum+tcp://%s:%d", localIP, a.config.Stratum.Port)
}

// GetMiningMode returns the current mining mode ("solo" or "proxy").
func (a *App) GetMiningMode() string {
	mode := a.config.MiningMode
	if mode == "" {
		return "solo"
	}
	return mode
}

// TestUpstreamConnection tests connectivity to an upstream pool.
func (a *App) TestUpstreamConnection(url, worker, password string) map[string]interface{} {
	if password == "" {
		password = "x"
	}

	uc := upstream.NewClient(url, worker, password, a.log)
	err := uc.Connect()
	if err != nil {
		return map[string]interface{}{
			"connected": false,
			"error":     err.Error(),
		}
	}
	defer uc.Stop()

	// Wait briefly for a job to arrive (gives us nBits for difficulty)
	time.Sleep(2 * time.Second)

	result := map[string]interface{}{
		"connected":       true,
		"extranonce1":     uc.Extranonce1(),
		"extranonce2Size": uc.Extranonce2Size(),
		"localEN2Size":    uc.LocalEN2Size(),
		"upstreamDiff":    uc.UpstreamDifficulty(),
	}

	if nbits := uc.LastNBits(); nbits != "" {
		result["lastNBits"] = nbits
		// Compute network difficulty from nBits
		target := stratum.CompactToBig(nbits)
		if target.Sign() > 0 {
			pdiff1 := stratum.Pdiff1Target()
			netDiff := new(big.Float).SetInt(pdiff1)
			netDiff.Quo(netDiff, new(big.Float).SetInt(target))
			nd, _ := netDiff.Float64()
			result["networkDiff"] = nd
		}
	}

	return result
}

// GetUpstreamStatus returns connection state of the upstream pool.
func (a *App) GetUpstreamStatus() map[string]interface{} {
	if a.upstream == nil {
		return map[string]interface{}{
			"connected": false,
			"mode":      a.GetMiningMode(),
		}
	}
	return map[string]interface{}{
		"connected":    a.upstream.IsConnected(),
		"authorized":   a.upstream.IsAuthorized(),
		"extranonce1":  a.upstream.Extranonce1(),
		"upstreamDiff": a.upstream.UpstreamDifficulty(),
		"mode":         "proxy",
	}
}

// GetProxyDiagnostics returns proxy share pipeline counters for debugging.
func (a *App) GetProxyDiagnostics() map[string]interface{} {
	if a.stratum == nil || !a.stratum.IsProxyMode() {
		return map[string]interface{}{"enabled": false}
	}
	d := a.stratum.GetProxyDiagnostics()
	return map[string]interface{}{
		"enabled":        true,
		"sharesIn":       d.SharesIn,
		"sharesFwd":      d.SharesFwd,
		"sharesAccepted": d.SharesAccepted,
		"sharesRejected": d.SharesRejected,
		"sharesBelow":    d.SharesBelow,
		"sharesDupe":     d.SharesDupe,
		"upstreamDiff":   d.UpstreamDiff,
		"minerDiffs":     d.MinerDiffs,
	}
}

// updateNetworkDiffFromNBits computes network difficulty from a compact target.
func (a *App) updateNetworkDiffFromNBits(nbitsHex string) {
	if nbitsHex == "" {
		return
	}
	nbitsBytes, err := hex.DecodeString(nbitsHex)
	if err != nil || len(nbitsBytes) != 4 {
		return
	}
	_ = binary.BigEndian.Uint32(nbitsBytes) // validate it parses

	target := stratum.CompactToBig(nbitsHex)
	if target.Sign() <= 0 {
		return
	}

	pdiff1 := stratum.Pdiff1Target()
	netDiff := new(big.Float).SetInt(pdiff1)
	netDiff.Quo(netDiff, new(big.Float).SetInt(target))
	nd, _ := netDiff.Float64()
	a.networkDiff = nd
}

// === Database ===

// GetDatabaseInfo returns the database file path and total disk usage in bytes.
func (a *App) GetDatabaseInfo() map[string]interface{} {
	if a.db == nil {
		return map[string]interface{}{"path": "", "size": 0}
	}
	return map[string]interface{}{
		"path": a.db.Path(),
		"size": a.db.Size(),
	}
}

// === Logs ===

func (a *App) GetRecentLogs(count int) []logger.LogEntry {
	if a.log == nil {
		return nil
	}
	return a.log.GetEntries(count)
}

func (a *App) SetLogLevel(level string) {
	if a.log != nil {
		a.log.SetLevel(level)
	}
	a.config.App.LogLevel = level
	a.config.Save()
}

// === Internal ===

func (a *App) statsLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	hashrateTicker := time.NewTicker(60 * time.Second)
	defer hashrateTicker.Stop()

	nodeRefreshTicker := time.NewTicker(30 * time.Second)
	defer nodeRefreshTicker.Stop()

	cumulativeTicker := time.NewTicker(5 * time.Minute)
	defer cumulativeTicker.Stop()

	pruneTicker := time.NewTicker(1 * time.Hour)
	defer pruneTicker.Stop()

	proxyStatsTicker := time.NewTicker(30 * time.Second)
	defer proxyStatsTicker.Stop()

	for {
		select {
		case <-a.stopStats:
			return
		case <-ticker.C:
			stats := a.GetDashboardStats()
			runtime.EventsEmit(a.ctx, "stats:updated", stats)
		case <-hashrateTicker.C:
			hashrate := a.stats.EstimateHashrate()
			a.stats.RecordHashrate(hashrate)
			if a.db != nil {
				a.db.InsertHashrate(time.Now().Unix(), hashrate)
			}
			// Update per-miner hashrates in registry
			for _, m := range a.registry.GetAll() {
				hr := a.stats.EstimateMinerHashrate(m.ID)
				a.registry.UpdateHashrate(m.ID, hr)
			}
		case <-cumulativeTicker.C:
			a.saveCumulativeStats()
		case <-pruneTicker.C:
			a.pruneOldData()
		case <-nodeRefreshTicker.C:
			a.refreshNodeInfo()
		case <-proxyStatsTicker.C:
			if a.stratum != nil && a.stratum.IsProxyMode() {
				d := a.stratum.GetProxyDiagnostics()
				fwdRate := float64(0)
				if d.SharesValid > 0 {
					fwdRate = float64(d.SharesFwd) / float64(d.SharesValid) * 100
				}
				rejectRate := float64(0)
				if d.SharesFwd > 0 {
					rejectRate = float64(d.SharesRejected) / float64(d.SharesFwd) * 100
				}
				dropped := d.SharesIn - d.SharesValid - d.SharesDupe - d.SharesStale
				a.log.Infof("proxy", "[STATS] in=%d valid=%d stale=%d dupe=%d other_reject=%d | fwd=%d(%.1f%%) accepted=%d rejected=%d(%.1f%%) below=%d upDiff=%.2f",
					d.SharesIn, d.SharesValid, d.SharesStale, d.SharesDupe, dropped,
					d.SharesFwd, fwdRate, d.SharesAccepted, d.SharesRejected, rejectRate,
					d.SharesBelow, d.UpstreamDiff)
				for name, diff := range d.MinerDiffs {
					a.log.Infof("proxy", "[STATS]   miner=%s vardiff=%.2f upDiff=%.2f ratio=%.2fx",
						name, diff, d.UpstreamDiff, diff/d.UpstreamDiff)
				}
			}
		}
	}
}

func (a *App) refreshNodeInfo() {
	// In proxy mode, network info comes from upstream job nBits
	if a.config.MiningMode == "proxy" {
		return
	}
	if info, err := a.nodeClient.GetMiningInfo(); err == nil {
		a.blockHeight = info.Blocks

		// For multi-algo coins (DGB), use the per-algorithm difficulty and
		// hashrate from the "difficulties"/"networkhashesps" maps. These are
		// always correct regardless of which algorithm's turn it is.
		miningAlgo := coin.Get(a.config.Mining.Coin).MiningAlgo
		if miningAlgo != "" && len(info.Difficulties) > 0 {
			if algoDiff, ok := info.Difficulties[miningAlgo]; ok {
				a.networkDiff = algoDiff
			}
			if algoHash, ok := info.NetworkHashesPSs[miningAlgo]; ok {
				a.networkHashrate = algoHash
			}
		} else {
			a.networkDiff = info.Difficulty
			a.networkHashrate = info.NetworkHashPS
		}
	}
}

func (a *App) loadStatsFromDB() {
	if a.db == nil {
		return
	}

	cumulative, err := a.db.LoadCumulativeStats()
	if err != nil {
		if a.log != nil {
			a.log.Errorf("app", "failed to load cumulative stats: %v", err)
		}
		return
	}

	// Load 7 days of hashrate history
	since := time.Now().Add(-7 * 24 * time.Hour).Unix()
	history, err := a.db.LoadHashrateHistory(since)
	if err != nil {
		if a.log != nil {
			a.log.Errorf("app", "failed to load hashrate history: %v", err)
		}
	}

	var points []miner.HashratePoint
	for _, h := range history {
		points = append(points, miner.HashratePoint{
			Timestamp: h.Timestamp,
			Hashrate:  h.Hashrate,
		})
	}

	a.stats.LoadFromDB(
		cumulative.TotalAccepted,
		cumulative.TotalRejected,
		cumulative.BlocksFound,
		cumulative.BestDifficulty,
		points,
	)

	if a.log != nil {
		a.log.Infof("app", "restored stats: %d accepted, %d rejected, %d blocks, %d hashrate points",
			cumulative.TotalAccepted, cumulative.TotalRejected, cumulative.BlocksFound, len(points))
	}
}

func (a *App) saveCumulativeStats() {
	if a.db == nil {
		return
	}

	accepted, rejected, blocks, bestDiff := a.stats.GetCumulativeStats()
	err := a.db.SaveCumulativeStats(database.CumulativeStats{
		TotalAccepted:  accepted,
		TotalRejected:  rejected,
		BestDifficulty: bestDiff,
		BlocksFound:    blocks,
	})
	if err != nil && a.log != nil {
		a.log.Errorf("app", "failed to save cumulative stats: %v", err)
	}
}

func (a *App) pruneOldData() {
	if a.db == nil {
		return
	}

	maxAge := 30 * 24 * time.Hour // 30 days
	if n, err := a.db.PruneShares(maxAge); err != nil {
		if a.log != nil {
			a.log.Errorf("app", "failed to prune shares: %v", err)
		}
	} else if n > 0 && a.log != nil {
		a.log.Infof("app", "pruned %d old shares", n)
	}

	if n, err := a.db.PruneHashrate(maxAge); err != nil {
		if a.log != nil {
			a.log.Errorf("app", "failed to prune hashrate: %v", err)
		}
	} else if n > 0 && a.log != nil {
		a.log.Infof("app", "pruned %d old hashrate entries", n)
	}
}
