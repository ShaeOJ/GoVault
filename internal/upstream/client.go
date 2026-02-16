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
	localEN2Size    int // upstream_en2_size - 2 (space for miner prefix)

	nextMinerPrefix atomic.Uint32
	upstreamDiff    float64
	upstreamDiffMu  sync.RWMutex

	lastNBits   string
	lastNBitsMu sync.RWMutex

	connected  atomic.Bool
	authorized atomic.Bool
	running    atomic.Bool
	stopCh     chan struct{}

	nextID  atomic.Int64
	pending map[int64]chan json.RawMessage
	pendMu  sync.Mutex

	log *logger.Logger

	// Callbacks
	OnJob        func(*JobParams)
	OnDifficulty func(float64)
	OnDisconnect func(error)
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

	go c.readLoop()

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

	c.log.Infof("upstream", "connected to %s (en1=%s en2_size=%d local_en2=%d)",
		addr, c.extranonce1, c.extranonce2Size, c.localEN2Size)

	// Start reconnect watcher
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
	c.log.Info("upstream", "client stopped")
}

func (c *Client) IsConnected() bool  { return c.connected.Load() }
func (c *Client) IsAuthorized() bool { return c.authorized.Load() }

func (c *Client) Extranonce1() string  { return c.extranonce1 }
func (c *Client) Extranonce2Size() int { return c.extranonce2Size }
func (c *Client) LocalEN2Size() int    { return c.localEN2Size }

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

// AssignMinerPrefix allocates a 2-byte hex prefix for a local miner's EN2 space.
func (c *Client) AssignMinerPrefix() (prefix string, en2Size int) {
	val := c.nextMinerPrefix.Add(1) & 0xFFFF
	return fmt.Sprintf("%04x", val), c.localEN2Size
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

	resp, err := c.call("mining.submit", params, 10*time.Second)
	if err != nil {
		return false, fmt.Sprintf("submit error: %v", err)
	}

	var result bool
	if json.Unmarshal(resp, &result) == nil && result {
		return true, ""
	}
	return false, string(resp)
}

// --- internal ---

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

	// Reserve 2 bytes of EN2 space for miner prefixes
	c.localEN2Size = en2Size - 2
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

		var msg rpcResponse
		if err := json.Unmarshal(line, &msg); err != nil {
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
		c.log.Errorf("upstream", "invalid mining.notify params")
		return
	}

	var jobID, prevHash, cb1, cb2, version, nbits, ntime string
	var branches []string
	var cleanJobs bool

	json.Unmarshal(raw[0], &jobID)
	json.Unmarshal(raw[1], &prevHash)
	json.Unmarshal(raw[2], &cb1)
	json.Unmarshal(raw[3], &cb2)
	json.Unmarshal(raw[4], &branches)
	json.Unmarshal(raw[5], &version)
	json.Unmarshal(raw[6], &nbits)
	json.Unmarshal(raw[7], &ntime)
	json.Unmarshal(raw[8], &cleanJobs)

	// Ensure branches is non-nil
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

	c.log.Infof("upstream", "job %s prevhash=%s..%s clean=%v", jobID, prevHash[:8], prevHash[len(prevHash)-8:], cleanJobs)

	if c.OnJob != nil {
		c.OnJob(job)
	}
}

func (c *Client) handleSetDifficulty(params json.RawMessage) {
	var raw []json.RawMessage
	if err := json.Unmarshal(params, &raw); err != nil || len(raw) < 1 {
		return
	}

	var diff float64
	if err := json.Unmarshal(raw[0], &diff); err != nil {
		return
	}

	c.upstreamDiffMu.Lock()
	c.upstreamDiff = diff
	c.upstreamDiffMu.Unlock()

	c.log.Infof("upstream", "difficulty set to %f", diff)

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
		c.pending = make(map[int64]chan json.RawMessage)

		go c.readLoop()

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

		c.log.Infof("upstream", "reconnected to %s", addr)
		backoff = time.Second
	}
}

// normalizeURL strips "stratum+tcp://" prefix if present.
func normalizeURL(url string) string {
	url = strings.TrimPrefix(url, "stratum+tcp://")
	url = strings.TrimPrefix(url, "stratum://")
	url = strings.TrimSuffix(url, "/")
	return url
}
