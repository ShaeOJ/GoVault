package stratum

import (
	"fmt"
	"govault/internal/coin"
	"govault/internal/config"
	"govault/internal/logger"
	"govault/internal/node"
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

	jobManager     *JobManager
	shareValidator *ShareValidator
	vardiffMgr     *VardiffManager
	nodeClient     *node.Client

	extranonce2Size int
	nextEN1         atomic.Uint32

	running atomic.Bool
	stopCh  chan struct{}
	wg      sync.WaitGroup

	log    *logger.Logger
	config *config.StratumConfig

	currentJobMu sync.RWMutex
	currentJobVal *Job

	// Event callbacks
	OnMinerConnected    func(MinerInfo)
	OnMinerDisconnected func(string)
	OnShareAccepted     func(string, float64, float64) // minerID, sessionDiff, actualDiff
	OnShareRejected     func(string, string)
	OnBlockFound        func(string, int64)
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

	return &Server{
		sessions:        make(map[string]*Session),
		jobManager:      jm,
		shareValidator:  sv,
		vardiffMgr:      vm,
		nodeClient:      nodeClient,
		extranonce2Size: extranonce2Size,
		stopCh:          make(chan struct{}),
		log:             log,
		config:          cfg,
	}
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

func (s *Server) acceptLoop() {
	defer s.wg.Done()

	for s.running.Load() {
		conn, err := s.listener.Accept()
		if err != nil {
			if s.running.Load() {
				s.log.Errorf("stratum", "accept error: %v", err)
			}
			return
		}

		// Enable TCP keepalives for fast dead-connection detection.
		// Matches ckpool: idle=45s, interval=30s (Go combines into period).
		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetKeepAlive(true)
			tc.SetKeepAlivePeriod(45 * time.Second)
			tc.SetNoDelay(true)
		}

		en1 := s.generateExtranonce1()
		sessionID := fmt.Sprintf("s_%s", en1)

		session := newSession(sessionID, conn, s, en1)

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

	s.log.Infof("stratum", "session %s disconnected (%s)", session.ID, session.workerName)

	if s.OnMinerDisconnected != nil && session.authorized {
		s.OnMinerDisconnected(session.ID)
	}
}

func (s *Server) generateExtranonce1() string {
	val := s.nextEN1.Add(1)
	return fmt.Sprintf("%08x", val)
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
