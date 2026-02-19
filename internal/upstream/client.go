package upstream

import (
	"bufio"
	"encoding/json"
	"fmt"
	"math/rand"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"govault/internal/logger"
)

// pendingJob stores an early job notification received before the OnJob
// callback is wired. This avoids losing the first job from the upstream.
type pendingJob struct {
	mu  sync.Mutex
	job *JobParams
}

// JobParams holds the fields from a mining.notify message.
type JobParams struct {
	JobID          string
	PrevHash       string
	Coinbase1      string
	Coinbase2      string
	MerkleBranches []string
	Version        string
	NBits          string
	NTime          string
	CleanJobs      bool
}

// Client is a Stratum V1 TCP client that connects to an upstream pool.
type Client struct {
	url        string
	workerName string
	password   string

	conn    net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex

	extranonce1     string
	extranonce2Size int
	localEN2Size    int // upstream_en2_size - prefixBytes
	prefixBytes     int // bytes stolen from EN2 for miner prefix (0-2)

	nextMinerPrefix atomic.Uint32
	upstreamDiff    float64
	upstreamDiffMu  sync.RWMutex

	lastNBits      string
	lastNBitsMu    sync.RWMutex
	versionRolling bool   // true if upstream accepted version-rolling
	versionMask    string // hex mask from upstream (e.g. "1fffe000")

	connected  atomic.Bool
	authorized atomic.Bool
	running    atomic.Bool
	stopCh     chan struct{}
	wg         sync.WaitGroup

	nextID  atomic.Int64
	pending map[int64]chan json.RawMessage
	pendMu  sync.Mutex

	log *logger.Logger

	// Callbacks
	OnJob        func(*JobParams)
	OnDifficulty func(float64)
	OnDisconnect func(error)
	OnReconnect  func() // called after successful reconnect (new EN1 assigned)

	// Buffer for early job notifications received before OnJob is wired.
	earlyJob pendingJob
}

type rpcRequest struct {
	ID     int64         `json:"id"`
	Method string        `json:"method"`
	Params []interface{} `json:"params"`
}

type rpcResponse struct {
	ID     *int64          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  json.RawMessage `json:"error"`
	Method string          `json:"method"`
	Params json.RawMessage `json:"params"`
}

// NewClient creates a new upstream stratum client.
func NewClient(url, workerName, password string, log *logger.Logger) *Client {
	return &Client{
		url:        normalizeURL(url),
		workerName: workerName,
		password:   password,
		pending:    make(map[int64]chan json.RawMessage),
		stopCh:     make(chan struct{}),
		log:        log,
	}
}

// Connect dials the upstream pool, subscribes, and authorizes.
func (c *Client) Connect() error {
	addr := c.url

	conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
	if err != nil {
		return fmt.Errorf("dial %s: %w", addr, err)
	}

	if tc, ok := conn.(*net.TCPConn); ok {
		tc.SetKeepAlive(true)
		tc.SetKeepAlivePeriod(45 * time.Second)
		tc.SetNoDelay(true)
	}

	c.conn = conn
	c.reader = bufio.NewReaderSize(conn, 8192)
	c.connected.Store(true)
	c.running.Store(true)

	c.wg.Add(1)
	go c.readLoop()

	// Negotiate version-rolling with upstream so forwarded shares
	// from version-rolling miners (Bitaxe, NerdAxe) validate correctly.
	c.configure()

	// Subscribe
	if err := c.subscribe(); err != nil {
		c.closeConn()
		return fmt.Errorf("subscribe: %w", err)
	}

	// Authorize
	if err := c.authorize(); err != nil {
		c.closeConn()
		return fmt.Errorf("authorize: %w", err)
	}

	c.log.Infof("upstream", "connected to %s (en1=%s en2_size=%d local_en2=%d vroll=%v)",
		addr, c.extranonce1, c.extranonce2Size, c.localEN2Size, c.versionRolling)

	// Start reconnect watcher
	c.wg.Add(1)
	go c.reconnectLoop()

	return nil
}

// Stop gracefully disconnects from the upstream pool.
func (c *Client) Stop() {
	if !c.running.CompareAndSwap(true, false) {
		return
	}
	close(c.stopCh)
	c.closeConn()
	c.wg.Wait()
	c.log.Info("upstream", "client stopped")
}

func (c *Client) IsConnected() bool  { return c.connected.Load() }
func (c *Client) IsAuthorized() bool { return c.authorized.Load() }

func (c *Client) Extranonce1() string    { return c.extranonce1 }
func (c *Client) Extranonce2Size() int   { return c.extranonce2Size }
func (c *Client) LocalEN2Size() int      { return c.localEN2Size }
func (c *Client) PrefixBytes() int       { return c.prefixBytes }
func (c *Client) WorkerName() string     { return c.workerName }
func (c *Client) VersionRolling() bool   { return c.versionRolling }
func (c *Client) VersionMask() string    { return c.versionMask }

func (c *Client) UpstreamDifficulty() float64 {
	c.upstreamDiffMu.RLock()
	defer c.upstreamDiffMu.RUnlock()
	return c.upstreamDiff
}

func (c *Client) LastNBits() string {
	c.lastNBitsMu.RLock()
	defer c.lastNBitsMu.RUnlock()
	return c.lastNBits
}

// DrainEarlyJob returns (and clears) any job notification that arrived
// before the OnJob callback was wired.
func (c *Client) DrainEarlyJob() *JobParams {
	c.earlyJob.mu.Lock()
	defer c.earlyJob.mu.Unlock()
	j := c.earlyJob.job
	c.earlyJob.job = nil
	return j
}

// AssignMinerPrefix allocates a hex prefix for a local miner's EN2 space.
// Prefix size varies (0-2 bytes) to ensure miners always get at least 4-byte EN2.
func (c *Client) AssignMinerPrefix() (prefix string, en2Size int) {
	if c.prefixBytes == 0 {
		return "", c.localEN2Size
	}
	mask := uint32((1 << (8 * c.prefixBytes)) - 1)
	val := c.nextMinerPrefix.Add(1) & mask
	format := fmt.Sprintf("%%0%dx", c.prefixBytes*2) // e.g. "%02x" for 1 byte, "%04x" for 2
	return fmt.Sprintf(format, val), c.localEN2Size
}

// SubmitShare forwards a share to the upstream pool.
func (c *Client) SubmitShare(worker, jobID, fullEN2, ntime, nonce, versionBits string) (bool, string) {
	if !c.connected.Load() {
		return false, "upstream disconnected"
	}

	params := []interface{}{worker, jobID, fullEN2, ntime, nonce}
	if versionBits != "" {
		params = append(params, versionBits)
	}

	c.log.Infof("proxy", "[SUBMIT-OUT] worker=%s job=%s en2=%s ntime=%s nonce=%s vbits=%s",
		worker, jobID, fullEN2, ntime, nonce, versionBits)

	resp, err := c.call("mining.submit", params, 10*time.Second)
	if err != nil {
		c.log.Infof("proxy", "[SUBMIT-RESP] ERROR: %v", err)
		return false, fmt.Sprintf("submit error: %v", err)
	}

	c.log.Infof("proxy", "[SUBMIT-RESP] raw=%s", string(resp))

	if resp == nil {
		return false, "upstream disconnected"
	}
	var result bool
	if json.Unmarshal(resp, &result) == nil && result {
		return true, ""
	}
	return false, string(resp)
}

// --- internal ---

// configure sends mining.configure to negotiate version-rolling with
// the upstream pool. Without this, forwarded shares from version-rolling
// miners (Bitaxe, NerdAxe) produce wrong hashes on the upstream side.
// Non-fatal: if the pool doesn't support it, we proceed without.
func (c *Client) configure() {
	// Reset before (re-)negotiation so stale state from a previous
	// connection doesn't persist if the new upstream doesn't support it.
	c.versionRolling = false
	c.versionMask = ""

	extensions := []string{"version-rolling"}
	extParams := map[string]interface{}{
		"version-rolling.mask":    "1fffe000",
		"version-rolling.min-bit-count": 2,
	}

	resp, err := c.call("mining.configure", []interface{}{extensions, extParams}, 10*time.Second)
	if err != nil {
		c.log.Infof("upstream", "mining.configure not supported: %v (version rolling disabled)", err)
		return
	}

	var result map[string]json.RawMessage
	if err := json.Unmarshal(resp, &result); err != nil {
		c.log.Infof("upstream", "mining.configure parse error: %v (version rolling disabled)", err)
		return
	}

	// Check if version-rolling was accepted
	if raw, ok := result["version-rolling"]; ok {
		var accepted bool
		if json.Unmarshal(raw, &accepted) == nil && accepted {
			c.versionRolling = true
			if maskRaw, ok := result["version-rolling.mask"]; ok {
				var mask string
				json.Unmarshal(maskRaw, &mask)
				c.versionMask = mask
			}
			c.log.Infof("upstream", "version-rolling enabled (mask=%s)", c.versionMask)
		}
	}

	if !c.versionRolling {
		c.log.Infof("upstream", "upstream did not accept version-rolling")
	}
}

func (c *Client) subscribe() error {
	resp, err := c.call("mining.subscribe", []interface{}{"GoVault/0.2.0"}, 10*time.Second)
	if err != nil {
		return err
	}

	// Parse: [[["mining.set_difficulty","id"],["mining.notify","id"]], extranonce1, extranonce2_size]
	var result []json.RawMessage
	if err := json.Unmarshal(resp, &result); err != nil {
		return fmt.Errorf("parse subscribe result: %w", err)
	}
	if len(result) < 3 {
		return fmt.Errorf("subscribe result too short: %d elements", len(result))
	}

	var en1 string
	if err := json.Unmarshal(result[1], &en1); err != nil {
		return fmt.Errorf("parse extranonce1: %w", err)
	}

	var en2Size int
	if err := json.Unmarshal(result[2], &en2Size); err != nil {
		return fmt.Errorf("parse extranonce2_size: %w", err)
	}

	c.extranonce1 = en1
	c.extranonce2Size = en2Size

	// Reserve prefix bytes from EN2 to give each miner a unique search space.
	// Each miner gets a unique EN1 (upstream_en1 + prefix), preventing nonce
	// overlap on the upstream pool. Requires at least 4-byte local EN2 for
	// firmware compatibility (many ASIC miners hardcode 4-byte EN2).
	c.prefixBytes = 2
	if en2Size-c.prefixBytes < 4 {
		c.prefixBytes = en2Size - 4
		if c.prefixBytes < 0 {
			c.prefixBytes = 0
		}
	}
	c.localEN2Size = en2Size - c.prefixBytes
	if c.localEN2Size < 1 {
		c.localEN2Size = 1
	}

	return nil
}

func (c *Client) authorize() error {
	resp, err := c.call("mining.authorize", []interface{}{c.workerName, c.password}, 10*time.Second)
	if err != nil {
		return err
	}

	var result bool
	if err := json.Unmarshal(resp, &result); err != nil || !result {
		return fmt.Errorf("authorization rejected: %s", string(resp))
	}

	c.authorized.Store(true)
	return nil
}

func (c *Client) readLoop() {
	defer c.wg.Done()
	defer func() {
		c.connected.Store(false)
		c.authorized.Store(false)
	}()

	for c.running.Load() {
		c.conn.SetReadDeadline(time.Now().Add(5 * time.Minute))
		line, err := c.reader.ReadBytes('\n')
		if err != nil {
			if c.running.Load() {
				c.log.Errorf("upstream", "read error: %v", err)
				if c.OnDisconnect != nil {
					c.OnDisconnect(err)
				}
			}
			return
		}

		c.log.Debugf("upstream", "recv: %s", strings.TrimSpace(string(line)))

		var msg rpcResponse
		if err := json.Unmarshal(line, &msg); err != nil {
			c.log.Debugf("upstream", "unparseable message: %v", err)
			continue
		}

		// Is this a response to a call we made?
		if msg.ID != nil {
			c.pendMu.Lock()
			ch, ok := c.pending[*msg.ID]
			if ok {
				delete(c.pending, *msg.ID)
			}
			c.pendMu.Unlock()
			if ok {
				// Check for error
				if len(msg.Error) > 0 && string(msg.Error) != "null" {
					ch <- msg.Error
				} else {
					ch <- msg.Result
				}
			}
			continue
		}

		// It's a notification
		c.handleNotification(msg.Method, msg.Params)
	}
}

func (c *Client) handleNotification(method string, params json.RawMessage) {
	switch method {
	case "mining.notify":
		c.handleJobNotify(params)
	case "mining.set_difficulty":
		c.handleSetDifficulty(params)
	default:
		c.log.Debugf("upstream", "unknown notification: %s", method)
	}
}

func (c *Client) handleJobNotify(params json.RawMessage) {
	var raw []json.RawMessage
	if err := json.Unmarshal(params, &raw); err != nil || len(raw) < 9 {
		c.log.Errorf("upstream", "invalid mining.notify params: %s", string(params))
		return
	}

	// Parse job ID — handle both string ("6b8d") and numeric (1234) formats.
	// Some pools send job IDs as JSON numbers instead of strings.
	// Also handle null → "null" so we always have a usable key.
	var jobID string
	if err := json.Unmarshal(raw[0], &jobID); err != nil || jobID == "" {
		// Not a JSON string, or parsed as empty/null; use raw representation.
		jobID = strings.Trim(string(raw[0]), " \t\n\r\"")
		if jobID == "" || jobID == "null" {
			jobID = "0" // fallback to a usable key
		}
		c.log.Debugf("upstream", "job ID parsed from raw: %q (raw=%s)", jobID, string(raw[0]))
	}

	var prevHash, cb1, cb2, version, nbits, ntime string
	var branches []string
	var cleanJobs bool

	if err := json.Unmarshal(raw[1], &prevHash); err != nil {
		c.log.Errorf("upstream", "failed to parse prevHash: %v (raw=%s)", err, string(raw[1]))
		return
	}
	if err := json.Unmarshal(raw[2], &cb1); err != nil {
		c.log.Errorf("upstream", "failed to parse coinbase1: %v (raw=%s)", err, string(raw[2]))
		return
	}
	if err := json.Unmarshal(raw[3], &cb2); err != nil {
		c.log.Errorf("upstream", "failed to parse coinbase2: %v (raw=%s)", err, string(raw[3]))
		return
	}
	json.Unmarshal(raw[4], &branches) // branches can be [] or null — both OK
	if err := json.Unmarshal(raw[5], &version); err != nil {
		c.log.Errorf("upstream", "failed to parse version: %v (raw=%s)", err, string(raw[5]))
		return
	}
	if err := json.Unmarshal(raw[6], &nbits); err != nil {
		c.log.Errorf("upstream", "failed to parse nbits: %v (raw=%s)", err, string(raw[6]))
		return
	}
	if err := json.Unmarshal(raw[7], &ntime); err != nil {
		c.log.Errorf("upstream", "failed to parse ntime: %v (raw=%s)", err, string(raw[7]))
		return
	}
	json.Unmarshal(raw[8], &cleanJobs) // false on error is fine

	// Validate critical fields
	if len(prevHash) != 64 {
		c.log.Errorf("upstream", "invalid prevHash length %d (expected 64): %s", len(prevHash), prevHash)
		return
	}
	if len(version) != 8 {
		c.log.Errorf("upstream", "invalid version length %d (expected 8): %s", len(version), version)
		return
	}
	if len(nbits) != 8 {
		c.log.Errorf("upstream", "invalid nbits length %d (expected 8): %s", len(nbits), nbits)
		return
	}
	if len(ntime) != 8 {
		c.log.Errorf("upstream", "invalid ntime length %d (expected 8): %s", len(ntime), ntime)
		return
	}

	// Ensure branches is non-nil for JSON serialization
	if branches == nil {
		branches = []string{}
	}

	c.lastNBitsMu.Lock()
	c.lastNBits = nbits
	c.lastNBitsMu.Unlock()

	job := &JobParams{
		JobID:          jobID,
		PrevHash:       prevHash,
		Coinbase1:      cb1,
		Coinbase2:      cb2,
		MerkleBranches: branches,
		Version:        version,
		NBits:          nbits,
		NTime:          ntime,
		CleanJobs:      cleanJobs,
	}

	c.log.Infof("upstream", "job %s prevhash=%s..%s version=%s nbits=%s clean=%v cb1=%d cb2=%d branches=%d",
		jobID, prevHash[:8], prevHash[len(prevHash)-8:], version, nbits, cleanJobs,
		len(cb1), len(cb2), len(branches))

	if c.OnJob != nil {
		c.OnJob(job)
	} else {
		// Buffer early job before OnJob is wired (race with Connect)
		c.earlyJob.mu.Lock()
		c.earlyJob.job = job
		c.earlyJob.mu.Unlock()
		c.log.Debugf("upstream", "buffered early job %s (OnJob not wired yet)", jobID)
	}
}

func (c *Client) handleSetDifficulty(params json.RawMessage) {
	var raw []json.RawMessage
	if err := json.Unmarshal(params, &raw); err != nil || len(raw) < 1 {
		c.log.Errorf("proxy", "[DIFF-RECV] failed to parse mining.set_difficulty: %s", string(params))
		return
	}

	var diff float64
	if err := json.Unmarshal(raw[0], &diff); err != nil {
		c.log.Errorf("proxy", "[DIFF-RECV] failed to parse difficulty value: %s", string(raw[0]))
		return
	}

	c.upstreamDiffMu.Lock()
	oldDiff := c.upstreamDiff
	c.upstreamDiff = diff
	c.upstreamDiffMu.Unlock()

	c.log.Infof("proxy", "[DIFF-RECV] mining.set_difficulty from pool: %.4f → %.4f", oldDiff, diff)

	if c.OnDifficulty != nil {
		c.OnDifficulty(diff)
	}
}

func (c *Client) call(method string, params []interface{}, timeout time.Duration) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	ch := make(chan json.RawMessage, 1)
	c.pendMu.Lock()
	c.pending[id] = ch
	c.pendMu.Unlock()

	req := rpcRequest{ID: id, Method: method, Params: params}
	if err := c.send(req); err != nil {
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return nil, err
	}

	select {
	case resp := <-ch:
		return resp, nil
	case <-time.After(timeout):
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return nil, fmt.Errorf("timeout waiting for %s response", method)
	case <-c.stopCh:
		c.pendMu.Lock()
		delete(c.pending, id)
		c.pendMu.Unlock()
		return nil, fmt.Errorf("client stopped")
	}
}

func (c *Client) send(req rpcRequest) error {
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	data = append(data, '\n')

	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("not connected")
	}
	c.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	_, err = c.conn.Write(data)
	return err
}

func (c *Client) closeConn() {
	c.connected.Store(false)
	c.authorized.Store(false)
	if c.conn != nil {
		c.conn.Close()
	}

	// Drain all pending calls
	c.pendMu.Lock()
	for id, ch := range c.pending {
		close(ch)
		delete(c.pending, id)
	}
	c.pendMu.Unlock()
}

func (c *Client) reconnectLoop() {
	defer c.wg.Done()
	backoff := time.Second
	maxBackoff := 30 * time.Second

	for {
		select {
		case <-c.stopCh:
			return
		default:
		}

		// Wait until disconnected
		for c.connected.Load() {
			select {
			case <-c.stopCh:
				return
			case <-time.After(time.Second):
			}
		}

		if !c.running.Load() {
			return
		}

		c.log.Infof("upstream", "reconnecting in %v...", backoff)
		select {
		case <-c.stopCh:
			return
		case <-time.After(backoff):
		}

		if !c.running.Load() {
			return
		}

		// Attempt reconnect
		addr := c.url
		conn, err := net.DialTimeout("tcp", addr, 15*time.Second)
		if err != nil {
			c.log.Errorf("upstream", "reconnect dial failed: %v", err)
			backoff = backoff * 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			// Add jitter
			backoff += time.Duration(rand.Intn(1000)) * time.Millisecond
			continue
		}

		if tc, ok := conn.(*net.TCPConn); ok {
			tc.SetKeepAlive(true)
			tc.SetKeepAlivePeriod(45 * time.Second)
			tc.SetNoDelay(true)
		}

		c.conn = conn
		c.reader = bufio.NewReaderSize(conn, 8192)
		c.connected.Store(true)
		c.pendMu.Lock()
		c.pending = make(map[int64]chan json.RawMessage)
		c.pendMu.Unlock()

		c.wg.Add(1)
		go c.readLoop()

		// Re-negotiate version-rolling before subscribe
		c.configure()

		if err := c.subscribe(); err != nil {
			c.log.Errorf("upstream", "reconnect subscribe failed: %v", err)
			c.closeConn()
			continue
		}

		if err := c.authorize(); err != nil {
			c.log.Errorf("upstream", "reconnect authorize failed: %v", err)
			c.closeConn()
			continue
		}

		c.log.Infof("upstream", "reconnected to %s (en1=%s en2_size=%d local_en2=%d vroll=%v)",
			addr, c.extranonce1, c.extranonce2Size, c.localEN2Size, c.versionRolling)
		backoff = time.Second

		if c.OnReconnect != nil {
			c.OnReconnect()
		}
	}
}

// normalizeURL strips "stratum+tcp://" prefix if present.
func normalizeURL(url string) string {
	url = strings.TrimPrefix(url, "stratum+tcp://")
	url = strings.TrimPrefix(url, "stratum://")
	url = strings.TrimSuffix(url, "/")
	return url
}
