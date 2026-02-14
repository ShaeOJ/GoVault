package main

import (
	"context"
	"fmt"
	"time"

	"govault/internal/coin"
	"govault/internal/config"
	"govault/internal/database"
	"govault/internal/logger"
	"govault/internal/miner"
	"govault/internal/node"
	"govault/internal/stratum"

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

	// Database persistence
	db     *database.DB
	buffer *database.Buffer

	// Cached node info
	networkDiff    float64
	networkHashrate float64
	blockHeight    int64

	stopStats chan struct{}
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
	if cfg.Stratum.AutoStart && cfg.Mining.PayoutAddress != "" {
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

	if a.config.Mining.PayoutAddress == "" {
		return fmt.Errorf("payout address not configured")
	}

	coinDef := coin.Get(a.config.Mining.Coin)
	a.log.Infof("app", "starting stratum for %s (%s)", coinDef.Name, coinDef.Symbol)

	a.stratum = stratum.NewServer(
		&a.config.Stratum,
		&a.config.Mining,
		&a.config.Vardiff,
		a.nodeClient,
		a.log,
		coinDef,
	)

	// Wire up callbacks
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
		a.registry.RecordShare(minerID, actualDiff, true)       // actual diff for per-miner bestDiff
		a.stats.RecordShare(minerID, sessionDiff, true)         // session diff for hashrate estimation
		a.stats.RecordBestDifficulty(actualDiff)                // actual diff for global bestDiff
		if a.buffer != nil {
			a.buffer.AddShare(database.ShareEntry{
				Timestamp:  time.Now().Unix(),
				MinerID:    minerID,
				Difficulty: actualDiff,
				Accepted:   true,
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

	a.stratum.OnBlockFound = func(hash string, height int64) {
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
		a.log.Infof("app", "BLOCK FOUND! Hash: %s Height: %d", hash, height)
	}

	if err := a.stratum.Start(); err != nil {
		return err
	}

	// Start chain monitor
	a.monitor = node.NewChainMonitor(a.nodeClient, 500*time.Millisecond, coinDef.GBTRules)
	a.monitor.SetRefreshInterval(10 * time.Second) // Refresh template for miners that don't roll ntime/en2 (e.g. BG01 cycles ~7s)
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

	a.log.Info("app", "stratum server started")
	return nil
}

func (a *App) StopStratum() error {
	if a.monitor != nil {
		a.monitor.Stop()
		a.monitor = nil
	}
	if a.stratum != nil {
		a.stratum.Stop()
	}
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

// === Node ===

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

func (a *App) GetStratumURL() string {
	localIP := miner.GetLocalIP()
	return fmt.Sprintf("stratum+tcp://%s:%d", localIP, a.config.Stratum.Port)
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
		}
	}
}

func (a *App) refreshNodeInfo() {
	if info, err := a.nodeClient.GetMiningInfo(); err == nil {
		a.networkDiff = info.Difficulty
		a.networkHashrate = info.NetworkHashPS
		a.blockHeight = info.Blocks
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
