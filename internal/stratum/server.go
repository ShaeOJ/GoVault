package stratum

import (
	"fmt"
	"govault/internal/coin"
	"govault/internal/config"
	"govault/internal/logger"
	"govault/internal/node"
	"govault/internal/upstream"
	"math/rand"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Server is the Stratum V1 TCP server.
type Server struct {
	listener  net.Listener
	sessions  map[string]*Session
	sessionMu sync.RWMutex

	// Per-IP connection tracking for rate limiting.
	ipConns   map[string]int
	ipConnsMu sync.Mutex

	banList *BanList

	jobManager     *JobManager
	shareValidator *ShareValidator
	vardiffMgr     *VardiffManager
	nodeClient     *node.Client

	extranonce2Size int
	nextEN1         atomic.Uint32
	nextSessionID   atomic.Uint32

	running  atomic.Bool
	startSec atomic.Int64 // unix seconds when Start() last succeeded
	stopCh   chan struct{}
	wg       sync.WaitGroup

	log    *logger.Logger
	config *config.StratumConfig

	currentJobMu sync.RWMutex
	currentJobVal *Job

	// Proxy mode fields
	proxyMode        bool
	upstreamEN1      string
	proxyPrefixBytes int    // bytes of EN2 used for miner prefix (0-2)
	upstreamDiff     float64
	upstreamDiffMu   sync.RWMutex
	proxyVersionMask uint32 // version-rolling mask from upstream (0 = no rolling)

	// Dual-pool hashrate-split mode fields. Independent of upstreamEN1/
	// proxyPrefixBytes above (those stay untouched for single-pool proxy
	// mode) — splitMode sessions get a purely LOCAL, stable extranonce1
	// (never containing any real pool's EN1), because the real pool's EN1 is
	// folded into that job's Coinbase1 by the proxysplit router instead. This
	// is what lets the active pool change without ever reconnecting a miner.
	// See internal/proxysplit/router.go for the fold/pad construction.
	splitMode        bool
	splitPrefixBytes int // bytes of the (virtual) EN2 space used as miner prefix
	activePoolLabel  string
	activePoolMu     sync.RWMutex

	upLatMs float64 // EMA of upstream share round-trip latency (ms)
	upLatMu sync.Mutex

	// Proxy diagnostic counters
	proxySharesIn       atomic.Uint64 // ALL shares received from miners (proxy mode)
	proxySharesValid    atomic.Uint64 // passed validation
	proxySharesFwd      atomic.Uint64 // forwarded to upstream
	proxySharesUpAccept atomic.Uint64 // accepted by upstream pool
	proxySharesUpReject atomic.Uint64 // rejected by upstream pool
	proxySharesBelow    atomic.Uint64 // below upstream diff, not forwarded
	proxySharesDupe     atomic.Uint64 // duplicate shares (not counted as rejections)
	proxySharesStale    atomic.Uint64 // stale job (job not found)

	// Event callbacks
	OnMinerConnected    func(MinerInfo)
	OnMinerDisconnected func(string)
	OnShareAccepted     func(minerID string, sessionDiff, actualDiff float64, nonce, header, hash string)
	OnShareRejected     func(string, string)
	OnBlockFound        func(hash string, height int64, accepted bool)
	LookupWorkerDiff    func(workerName string) float64
	OnDiffChanged       func(workerName string, diff float64)
	OnShareForward      func(workerName, jobID, fullEN2, ntime, nonce, versionBits string) (bool, string)

	// OnMinerAuthorize is called in proxy/edge mode when a miner sends
	// mining.authorize. If set, the callback forwards the credentials to the
	// upstream pool and returns (true, "") on success or (false, reason) on
	// rejection. When nil, all miners are accepted locally (original behaviour).
	OnMinerAuthorize func(worker, password string) (bool, string)

	// OnMinerConnect is set in per-miner pass-through mode. When non-nil it
	// takes priority over OnMinerAuthorize. It creates and connects a dedicated
	// upstream client for this miner using their own credentials, making them
	// appear as an independent connection to the upstream pool.
	OnMinerConnect func(session *Session, worker, password string) (*upstream.Client, error)

	// OnMinerSubscribe (per-miner pass-through, preferred over OnMinerConnect) is
	// called at mining.subscribe. It opens+subscribes a dedicated upstream (no
	// authorize yet) and returns it, so the pool's extranonce1/size can be sent in
	// the miner's subscribe response — working even for firmware that ignores
	// mining.set_extranonce. Authorization is deferred to OnMinerAuthorizeUpstream.
	OnMinerSubscribe func(session *Session) (*upstream.Client, error)

	// OnMinerAuthorizeUpstream authorizes the already-connected dedicated upstream
	// (from OnMinerSubscribe) with the miner's worker name. Returns (accepted, err).
	OnMinerAuthorizeUpstream func(session *Session, worker, password string) (bool, error)

	// OnUpstreamJobInfo, when set, is called for each upstream job in per-miner
	// pass-through mode with the job's nBits and cleanJobs flag, so the engine can
	// track network difficulty and block height (there's no single shared upstream
	// to hang an OnJob callback on).
	OnUpstreamJobInfo func(nbits, coinbase1 string, cleanJobs bool)
}

func NewServer(
	cfg *config.StratumConfig,
	miningCfg *config.MiningConfig,
	vardiffCfg *config.VardiffConfig,
	nodeClient *node.Client,
	log *logger.Logger,
	coinDef *coin.CoinDef,
) *Server {
	extranonce2Size := 4
	jm := NewJobManager(miningCfg.PayoutAddress, miningCfg.CoinbaseTag, extranonce2Size, coinDef)
	sv := NewShareValidator(jm)
	vm := NewVardiffManager(vardiffCfg)

	s := &Server{
		sessions:        make(map[string]*Session),
		ipConns:         make(map[string]int),
		banList:         newBanList(),
		jobManager:      jm,
		shareValidator:  sv,
		vardiffMgr:      vm,
		nodeClient:      nodeClient,
		extranonce2Size: extranonce2Size,
		stopCh:          make(chan struct{}),
		log:             log,
		config:          cfg,
	}

	// Seed EN1 counter with random upper 16 bits so session IDs don't
	// recycle across stop/start cycles. Lower 16 bits = counter space
	// for 65536 connections per server instance (more than enough).
	s.nextEN1.Store(rand.Uint32() & 0xFFFF0000)
	s.nextSessionID.Store(rand.Uint32() & 0xFFFF0000)

	return s
}

// Start begins listening for miner connections.
func (s *Server) Start() error {
	addr := fmt.Sprintf("0.0.0.0:%d", s.config.Port)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", addr, err)
	}

	s.listener = listener
	s.running.Store(true)
	s.startSec.Store(time.Now().Unix())
	s.log.Infof("stratum", "server started on %s", addr)

	s.wg.Add(1)
	go s.acceptLoop()

	return nil
}

// Stop gracefully shuts down the server.
func (s *Server) Stop() {
	if !s.running.CompareAndSwap(true, false) {
		return
	}
	close(s.stopCh)

	if s.listener != nil {
		s.listener.Close()
	}

	// Tell miners to reconnect before we close their connections.
	// cgminer/S9 and other firmware use this to reconnect quickly
	// instead of entering a long exponential backoff.
	s.sessionMu.RLock()
	for _, session := range s.sessions {
		if session.authorized {
			session.sendReconnect(3)
		}
	}
	s.sessionMu.RUnlock()

	// Brief pause so miners receive the reconnect notification
	// before we tear down their TCP connections.
	time.Sleep(200 * time.Millisecond)

	// Close all sessions
	s.sessionMu.Lock()
	for _, session := range s.sessions {
		session.conn.Close()
	}
	s.sessionMu.Unlock()

	s.wg.Wait()
	s.log.Info("stratum", "server stopped")
}

func (s *Server) IsRunning() bool {
	return s.running.Load()
}

// UptimeSeconds returns how long the server has been running since its last
// Start(), or 0 if it isn't running.
func (s *Server) UptimeSeconds() int64 {
	if !s.running.Load() {
		return 0
	}
	start := s.startSec.Load()
	if start == 0 {
		return 0
	}
	return time.Now().Unix() - start
}

func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for s.running.Load() {
		conn, err := s.listener.Accept()
		if err != nil {
			if !s.running.Load() {
				return // server shutting down — expected
			}
			// Transient error (e.g. too many open files) — log and
			// retry after a short backoff instead of exiting permanently.
			s.log.Errorf("stratum", "accept error (retrying): %v", err)
			select {
			case <-s.stopCh:
				return
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}

		// Enable TCP keepalives for fast dead-connection detection.
		// Matches ckpool: idle=45s, interval=30s (Go combines into period).
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetKeepAlive(true)
			tc.SetKeepAlivePeriod(45 * time.Second)
			tc.SetNoDelay(true)
		}

		remoteIP, _, _ := net.SplitHostPort(conn.RemoteAddr().String())

		// Reject banned IPs immediately.
		if s.config.BanDurationMinutes > 0 {
			if banned, expiry := s.banList.IsBanned(remoteIP); banned {
				s.log.Warnf("stratum", "rejected banned IP %s (ban expires %s)", remoteIP, expiry.Format("15:04:05"))
				conn.Close()
				continue
			}
		}

		// Enforce global connection cap.
		s.sessionMu.RLock()
		totalConns := len(s.sessions)
		s.sessionMu.RUnlock()
		if s.config.MaxConn > 0 && totalConns >= s.config.MaxConn {
			s.log.Warnf("stratum", "max connections (%d) reached, rejecting %s", s.config.MaxConn, remoteIP)
			conn.Close()
			continue
		}

		// Enforce per-IP connection cap.
		if s.config.MaxConnPerIP > 0 {
			s.ipConnsMu.Lock()
			ipCount := s.ipConns[remoteIP]
			if ipCount >= s.config.MaxConnPerIP {
				s.ipConnsMu.Unlock()
				s.log.Warnf("stratum", "per-IP limit (%d) reached for %s, rejecting", s.config.MaxConnPerIP, remoteIP)
				conn.Close()
				continue
			}
			s.ipConns[remoteIP] = ipCount + 1
			s.ipConnsMu.Unlock()
		}

		en1 := s.generateExtranonce1()
		sessionID := fmt.Sprintf("s_%08x", s.nextSessionID.Add(1))

		session := newSession(sessionID, conn, s, en1)
		session.remoteIP = remoteIP

		s.sessionMu.Lock()
		s.sessions[sessionID] = session
		s.sessionMu.Unlock()

		s.log.Infof("stratum", "new connection from %s (session %s)", conn.RemoteAddr(), sessionID)

		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			session.Handle()
		}()
	}
}

func (s *Server) removeSession(session *Session) {
	s.sessionMu.Lock()
	delete(s.sessions, session.ID)
	s.sessionMu.Unlock()

	if session.remoteIP != "" {
		s.ipConnsMu.Lock()
		if s.ipConns[session.remoteIP] > 1 {
			s.ipConns[session.remoteIP]--
		} else {
			delete(s.ipConns, session.remoteIP)
		}
		s.ipConnsMu.Unlock()
	}

	s.log.Infof("stratum", "session %s disconnected (%s)", session.ID, session.workerName)

	if s.OnDiffChanged != nil && session.authorized && session.workerName != "" {
		session.diffMu.Lock()
		diff := session.currentDiff
		session.diffMu.Unlock()
		s.OnDiffChanged(session.workerName, diff)
	}
	if s.OnMinerDisconnected != nil && session.authorized {
		s.OnMinerDisconnected(session.ID)
	}
}

func (s *Server) generateExtranonce1() string {
	if s.splitMode {
		if s.splitPrefixBytes == 0 {
			// Degraded fallback (joint EN2 budget too small for isolation) —
			// same risk profile as the single-pool proxyPrefixBytes==0 case
			// below, just with no upstream EN1 to even partially disambiguate.
			return ""
		}
		mask := uint32((1 << (8 * s.splitPrefixBytes)) - 1)
		counter := s.nextEN1.Add(1) & mask
		format := fmt.Sprintf("%%0%dx", s.splitPrefixBytes*2)
		return fmt.Sprintf(format, counter)
	}
	if s.proxyMode {
		if s.OnMinerConnect != nil {
			// Per-miner mode: assign a unique placeholder; real EN1 is sent via
			// mining.set_extranonce once the miner's dedicated upstream connects.
			return fmt.Sprintf("%08x", s.nextEN1.Add(1))
		}
		if s.proxyPrefixBytes == 0 {
			// Shared upstream, no prefix — all miners share the upstream EN2 space.
			return s.upstreamEN1
		}
		mask := uint32((1 << (8 * s.proxyPrefixBytes)) - 1)
		counter := s.nextEN1.Add(1) & mask
		format := fmt.Sprintf("%%0%dx", s.proxyPrefixBytes*2)
		return s.upstreamEN1 + fmt.Sprintf(format, counter)
	}
	val := s.nextEN1.Add(1)
	return fmt.Sprintf("%08x", val)
}

// SetProxyMode configures the server for proxy operation.
// versionMask is the upstream pool's version-rolling mask (0 = no rolling).
func (s *Server) SetProxyMode(upstreamEN1 string, localEN2Size, prefixBytes int, versionMask uint32) {
	s.proxyMode = true
	s.upstreamEN1 = upstreamEN1
	s.extranonce2Size = localEN2Size
	s.proxyPrefixBytes = prefixBytes
	s.proxyVersionMask = versionMask
	s.shareValidator.skipDupeCheck = true // let upstream pool handle duplicates
}

// SetProxySplitMode configures the server for dual-pool hashrate-split
// operation. Unlike SetProxyMode, there's no single upstreamEN1 — sessions
// get a stable local EN1 (see generateExtranonce1), and virtualEN2Size is the
// FIXED extranonce2 size advertised to every miner for the life of their
// connection (computed by the router as the smaller of both pools' real EN2
// sizes minus prefixBytes, so it always fits regardless of which pool is
// active — see proxysplit.Router).
func (s *Server) SetProxySplitMode(prefixBytes, virtualEN2Size int, versionMask uint32) {
	s.proxyMode = true // reuse the general proxy-mode flag (skipDupeCheck etc.
	// below already keys off it the same way single-pool proxy mode does)
	s.splitMode = true
	s.splitPrefixBytes = prefixBytes
	s.extranonce2Size = virtualEN2Size
	s.proxyVersionMask = versionMask
	s.shareValidator.skipDupeCheck = true // let upstream pools handle duplicates
}

// IsSplitMode returns true if the server is running dual-pool hashrate split.
func (s *Server) IsSplitMode() bool {
	return s.splitMode
}

// SetPerMinerMode configures the server for 1:1 miner→upstream pass-through.
// Each miner gets their own dedicated upstream connection using their own credentials,
// so they appear as independent connections to the upstream pool.
// versionMask: use 0 to default to standard BTC mask (1fffe000).
func (s *Server) SetPerMinerMode(versionMask uint32) {
	s.proxyMode = true
	s.proxyPrefixBytes = 0
	if versionMask == 0 {
		versionMask = 0x1fffe000
	}
	s.proxyVersionMask = versionMask
	s.shareValidator.skipDupeCheck = true
}

// IsProxyMode returns true if the server is in proxy mode.
func (s *Server) IsProxyMode() bool {
	return s.proxyMode
}

// UpdateProxyState updates upstream EN1 and version mask after a reconnect,
// then kicks all miners so they reconnect and get new EN1-based sessions.
func (s *Server) UpdateProxyState(upstreamEN1 string, localEN2Size, prefixBytes int, versionMask uint32) {
	s.upstreamEN1 = upstreamEN1
	s.extranonce2Size = localEN2Size
	s.proxyPrefixBytes = prefixBytes
	s.proxyVersionMask = versionMask

	s.log.Infof("stratum", "upstream reconnected — new EN1=%s, kicking %d miners to reconnect", upstreamEN1, s.SessionCount())

	// Send client.reconnect to all authorized miners, then close their connections.
	// They'll reconnect immediately and get new sessions with the updated EN1.
	s.sessionMu.RLock()
	for _, session := range s.sessions {
		if session.authorized {
			session.sendReconnect(3)
		}
	}
	s.sessionMu.RUnlock()

	time.Sleep(200 * time.Millisecond)

	s.sessionMu.Lock()
	for _, session := range s.sessions {
		session.conn.Close()
	}
	s.sessionMu.Unlock()
}

// SetUpstreamDifficulty sets the current upstream pool difficulty and
// immediately relays it to all connected miners (true passthrough).
func (s *Server) SetUpstreamDifficulty(diff float64) {
	s.upstreamDiffMu.Lock()
	old := s.upstreamDiff
	s.upstreamDiff = diff
	s.upstreamDiffMu.Unlock()

	if old != diff {
		s.log.Infof("proxy", "[DIFF-RELAY] upstream pool diff: %.4f → %.4f — broadcasting to miners", old, diff)

		// Immediately relay new difficulty to all authorized miners.
		s.sessionMu.RLock()
		for _, session := range s.sessions {
			if session.authorized {
				session.setProxyDiff(diff)
			}
		}
		s.sessionMu.RUnlock()
	}
}

// UpstreamDifficulty returns the current upstream pool difficulty.
func (s *Server) UpstreamDifficulty() float64 {
	s.upstreamDiffMu.RLock()
	defer s.upstreamDiffMu.RUnlock()
	return s.upstreamDiff
}

// recordUpstreamLatency feeds an EMA of the upstream share round-trip time,
// smoothed over roughly the last 10 shares (alpha = 1/10).
func (s *Server) recordUpstreamLatency(d time.Duration) {
	ms := float64(d.Microseconds()) / 1000.0
	s.upLatMu.Lock()
	if s.upLatMs == 0 {
		s.upLatMs = ms
	} else {
		s.upLatMs = s.upLatMs*0.9 + ms*0.1
	}
	s.upLatMu.Unlock()
}

// AvgUpstreamLatencyMs returns the smoothed upstream share latency (pool ping).
func (s *Server) AvgUpstreamLatencyMs() float64 {
	s.upLatMu.Lock()
	defer s.upLatMu.Unlock()
	return s.upLatMs
}

// ProxyDiagnostics holds proxy share pipeline counters.
type ProxyDiagnostics struct {
	SharesIn       uint64  `json:"sharesIn"`       // ALL received from miners
	SharesValid    uint64  `json:"sharesValid"`    // passed validation
	SharesFwd      uint64  `json:"sharesFwd"`      // forwarded to upstream
	SharesAccepted uint64  `json:"sharesAccepted"` // accepted by upstream
	SharesRejected uint64  `json:"sharesRejected"` // rejected by upstream
	SharesBelow    uint64  `json:"sharesBelow"`    // below upstream diff
	SharesDupe     uint64  `json:"sharesDupe"`     // duplicate (ASIC re-reads)
	SharesStale    uint64  `json:"sharesStale"`    // stale job (job not found)
	UpstreamDiff   float64 `json:"upstreamDiff"`   // current upstream pool diff
	MinerDiffs     map[string]float64 `json:"minerDiffs"` // worker → session diff
	MinerDupes     map[string]uint64  `json:"minerDupes"` // worker → duplicate count
	MinerAccepted  map[string]uint64  `json:"minerAccepted"` // worker → accepted count
	ActivePool     string             `json:"activePool,omitempty"` // split mode only: "A" or "B"
}

// GetProxyDiagnostics returns current proxy pipeline counters.
func (s *Server) GetProxyDiagnostics() ProxyDiagnostics {
	d := ProxyDiagnostics{
		SharesIn:       s.proxySharesIn.Load(),
		SharesValid:    s.proxySharesValid.Load(),
		SharesFwd:      s.proxySharesFwd.Load(),
		SharesAccepted: s.proxySharesUpAccept.Load(),
		SharesRejected: s.proxySharesUpReject.Load(),
		SharesBelow:    s.proxySharesBelow.Load(),
		SharesDupe:     s.proxySharesDupe.Load(),
		SharesStale:    s.proxySharesStale.Load(),
		UpstreamDiff:   s.UpstreamDifficulty(),
		MinerDiffs:     make(map[string]float64),
		MinerDupes:     make(map[string]uint64),
		MinerAccepted:  make(map[string]uint64),
		ActivePool:     s.ActivePoolLabel(),
	}

	s.sessionMu.RLock()
	for _, session := range s.sessions {
		if session.authorized && session.workerName != "" {
			session.diffMu.Lock()
			d.MinerDiffs[session.workerName] = session.currentDiff
			d.MinerDupes[session.workerName] = session.sharesDuped
			d.MinerAccepted[session.workerName] = session.sharesAccepted
			session.diffMu.Unlock()
		}
		// In per-miner pass-through there's no server-wide upstream diff (each
		// miner has its own upstream), so derive a representative value from the
		// dedicated connections for the dashboard's "Pool Difficulty".
		if d.UpstreamDiff == 0 && session.dedicatedUpstream != nil {
			if ud := session.dedicatedUpstream.UpstreamDifficulty(); ud > 0 {
				d.UpstreamDiff = ud
			}
		}
	}
	s.sessionMu.RUnlock()

	return d
}

// GetJob returns the job registered under id, or nil if unknown/evicted. Used
// by the proxysplit router to resolve a submitted job's source pool.
func (s *Server) GetJob(id string) *Job {
	return s.jobManager.GetJob(id)
}

// RegisterAndMaybeBroadcastSplitJob registers a dual-pool-split job (folded/
// padded coinbase already computed by the caller — see proxysplit.Router) and
// broadcasts it to miners only when broadcast is true (i.e. sourcePool is the
// currently-active pool). When false, the job is still registered so shares
// already in flight against it (e.g. right at a pool-switch boundary) still
// resolve correctly — the inactive pool stays a "hot" registered source.
func (s *Server) RegisterAndMaybeBroadcastSplitJob(
	mapID, realJobID, prevHash, coinbase1, coinbase2 string,
	merkleBranches []string,
	version, nbits, ntime string,
	cleanJobs bool,
	sourcePool string,
	realEN2Size int,
	broadcast bool,
) *Job {
	job := s.jobManager.RegisterUpstreamJob(
		mapID, realJobID, prevHash, coinbase1, coinbase2,
		merkleBranches, version, nbits, ntime, cleanJobs,
		sourcePool, realEN2Size,
	)

	activeIDs := s.jobManager.ActiveJobIDs()
	s.shareValidator.CleanDuplicates(activeIDs)

	if broadcast {
		s.log.Debugf("stratum", "[split] registered+broadcasting job %q from pool %s (active jobs: %d)", job.ID, sourcePool, len(activeIDs))
		s.BroadcastJob(job, cleanJobs)
	} else {
		s.log.Debugf("stratum", "[split] registered (standby) job %q from pool %s (active jobs: %d)", job.ID, sourcePool, len(activeIDs))
	}

	return job
}

// SetActivePoolLabel records which pool ("A"/"B") is currently live, surfaced
// via GetProxyDiagnostics for the dashboard. No-op outside split mode.
func (s *Server) SetActivePoolLabel(label string) {
	s.activePoolMu.Lock()
	s.activePoolLabel = label
	s.activePoolMu.Unlock()
}

func (s *Server) ActivePoolLabel() string {
	s.activePoolMu.RLock()
	defer s.activePoolMu.RUnlock()
	return s.activePoolLabel
}

// BroadcastUpstreamJob registers a job from upstream and broadcasts to all miners.
func (s *Server) BroadcastUpstreamJob(params *upstream.JobParams) {
	job := s.jobManager.RegisterUpstreamJob(
		params.JobID,
		params.JobID,
		params.PrevHash,
		params.Coinbase1,
		params.Coinbase2,
		params.MerkleBranches,
		params.Version,
		params.NBits,
		params.NTime,
		params.CleanJobs,
		"",
		0,
	)

	activeIDs := s.jobManager.ActiveJobIDs()
	s.log.Debugf("stratum", "registered upstream job %q (active jobs: %d)", job.ID, len(activeIDs))

	// Always clean duplicate tracking for trimmed-out jobs (matches solo mode)
	s.shareValidator.CleanDuplicates(activeIDs)

	s.BroadcastJob(job, params.CleanJobs)
}

// BroadcastJob sends a new job to all connected and authorized miners.
func (s *Server) BroadcastJob(job *Job, cleanJobs bool) {
	s.setCurrentJob(job)

	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()

	for _, session := range s.sessions {
		if session.authorized {
			session.sendNotify(job, cleanJobs)
		}
	}

	s.log.Infof("stratum", "broadcast job %s to %d miners (clean=%v)", job.ID, len(s.sessions), cleanJobs)
}

// NewBlockTemplate processes a new block template from the node.
func (s *Server) NewBlockTemplate(tmpl *node.BlockTemplate) {
	job, err := s.jobManager.CreateJob(tmpl, 4) // extranonce1 is 4 bytes
	if err != nil {
		s.log.Errorf("stratum", "create job failed: %v", err)
		return
	}

	// Clean up stale duplicate tracking
	s.shareValidator.CleanDuplicates(s.jobManager.ActiveJobIDs())

	s.BroadcastJob(job, true)
}

// RefreshBlockTemplate sends an updated job with fresh ntime (same block).
// This gives miners a new search space without discarding in-flight work.
func (s *Server) RefreshBlockTemplate(tmpl *node.BlockTemplate) {
	job, err := s.jobManager.CreateJob(tmpl, 4)
	if err != nil {
		s.log.Errorf("stratum", "refresh job failed: %v", err)
		return
	}

	s.shareValidator.CleanDuplicates(s.jobManager.ActiveJobIDs())

	s.BroadcastJob(job, false) // cleanJobs=false — miners keep old work
}

func (s *Server) sendCurrentJob(session *Session) {
	job := s.currentJob()
	if job != nil {
		session.sendNotify(job, true)
		s.log.Infof("stratum", "sent job %s to miner %s", job.ID, session.workerName)
	} else {
		s.log.Infof("stratum", "no current job available for miner %s (waiting for block template)", session.workerName)
	}
}

func (s *Server) setCurrentJob(job *Job) {
	s.currentJobMu.Lock()
	s.currentJobVal = job
	s.currentJobMu.Unlock()
}

func (s *Server) currentJob() *Job {
	s.currentJobMu.RLock()
	defer s.currentJobMu.RUnlock()
	return s.currentJobVal
}

// GetSessions returns info about all connected miners.
func (s *Server) GetSessions() []MinerInfo {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()

	var miners []MinerInfo
	for _, session := range s.sessions {
		if session.authorized {
			miners = append(miners, session.toMinerInfo())
		}
	}
	return miners
}

// SessionCount returns the number of active sessions.
func (s *Server) SessionCount() int {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	return len(s.sessions)
}

// UpdatePayoutAddress updates the payout address for new jobs.
func (s *Server) UpdatePayoutAddress(addr string) {
	s.jobManager.SetPayoutAddress(addr)
}

// BanIP temporarily bans ip for the configured duration.
// No-op when BanDurationMinutes is zero.
func (s *Server) BanIP(ip, reason string) {
	if s.config.BanDurationMinutes <= 0 {
		return
	}
	dur := time.Duration(s.config.BanDurationMinutes) * time.Minute
	s.banList.Ban(ip, reason, dur, s.log)
}
