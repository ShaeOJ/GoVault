package stratum

import (
	"bufio"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"strconv"
	"sync"
	"time"
)

// Session represents a single miner connection.
type Session struct {
	ID          string
	conn        net.Conn
	server      *Server
	extranonce1 string
	subscribed  bool
	authorized  bool
	workerName  string
	userAgent   string
	currentDiff float64
	connectedAt  time.Time
	lastActivity time.Time
	reader      *bufio.Reader
	writeMu     sync.Mutex

	vardiffState *VardiffState

	versionRolling bool
	versionMask    uint32

	sharesAccepted uint64
	sharesRejected uint64
	sharesDuped    uint64
	bestDifficulty float64

	suggestedDiff float64 // from mining.suggest_difficulty (miner's threshold)

	// Difficulty transition grace period (matches ckpool diff_change_job_id).
	// Shares for jobs issued before diffChangeJobID are validated against oldDiff.
	oldDiff          float64
	diffChangeJobID  string
}

func newSession(id string, conn net.Conn, server *Server, extranonce1 string) *Session {
	now := time.Now()
	return &Session{
		ID:           id,
		conn:         conn,
		server:       server,
		extranonce1:  extranonce1,
		currentDiff:  server.vardiffMgr.StartDiff(),
		connectedAt:  now,
		lastActivity: now,
		reader:       bufio.NewReaderSize(conn, 4096),
	}
}

// Handle is the main loop for processing messages from a miner.
func (s *Session) Handle() {
	defer func() {
		if r := recover(); r != nil {
			s.server.log.Errorf("stratum", "session %s panic: %v", s.ID, r)
		}
		s.conn.Close()
		s.server.removeSession(s)
	}()

	// Initialize vardiff state
	s.vardiffState = s.server.vardiffMgr.NewState()

	for {
		// Use retarget interval as read deadline so idle sessions get
		// periodic vardiff checks (halving difficulty when no shares arrive).
		retargetInterval := s.server.vardiffMgr.RetargetInterval()
		s.conn.SetReadDeadline(time.Now().Add(retargetInterval))

		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			// Timeout → idle vardiff check (don't disconnect yet)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// True inactivity (no data at all for 5 min) → disconnect
				if time.Since(s.lastActivity) > 5*time.Minute {
					return
				}
				// Idle vardiff: halve difficulty if no qualifying shares arrived
				if s.authorized && s.vardiffState != nil {
					if newDiff, changed := s.server.vardiffMgr.CheckRetarget(s.vardiffState, s.currentDiff, s.suggestedDiff); changed {
						s.oldDiff = s.currentDiff
						if curJob := s.server.currentJob(); curJob != nil {
							s.diffChangeJobID = curJob.ID
						}
						s.currentDiff = newDiff
						s.sendSetDifficulty(newDiff)
						s.server.log.Infof("stratum", "idle vardiff: %s difficulty -> %.6f", s.workerName, newDiff)
						if s.server.OnDiffChanged != nil && s.workerName != "" {
							s.server.OnDiffChanged(s.workerName, newDiff)
						}
					}
				}
				continue
			}
			return // real error → disconnect
		}

		s.lastActivity = time.Now()

		// Trim trailing whitespace
		for len(line) > 0 && (line[len(line)-1] == '\n' || line[len(line)-1] == '\r') {
			line = line[:len(line)-1]
		}
		if len(line) == 0 {
			continue
		}

		req, err := ParseRequest(line)
		if err != nil {
			s.server.log.Debugf("stratum", "session %s bad request: %v", s.ID, err)
			continue
		}

		s.handleRequest(req)
	}
}

func (s *Session) handleRequest(req *Request) {
	switch req.Method {
	case "mining.configure":
		s.handleConfigure(req)
	case "mining.subscribe":
		s.handleSubscribe(req)
	case "mining.authorize":
		s.handleAuthorize(req)
	case "mining.submit":
		s.handleSubmit(req)
	case "mining.suggest_difficulty":
		s.handleSuggestDifficulty(req)
	case "mining.extranonce.subscribe":
		s.sendResponse(req.ID, true, nil)
	default:
		s.server.log.Debugf("stratum", "session %s unknown method: %s", s.ID, req.Method)
		s.sendResponse(req.ID, nil, NewError(ErrOther, "unknown method"))
	}
}

func (s *Session) handleConfigure(req *Request) {
	// mining.configure params: [["version-rolling", ...], {"version-rolling.mask": "ffffffff", ...}]
	result := make(map[string]interface{})

	var extensions []string
	if len(req.Params) > 0 {
		json.Unmarshal(req.Params[0], &extensions)
	}

	var extParams map[string]json.RawMessage
	if len(req.Params) > 1 {
		json.Unmarshal(req.Params[1], &extParams)
	}

	for _, ext := range extensions {
		switch ext {
		case "version-rolling":
			// In proxy mode, constrain to the upstream pool's mask so
			// forwarded shares don't trigger "mask violation" rejections.
			// In solo mode, use the standard safe mask.
			poolMask := uint32(0x1fffe000)
			if s.server.proxyMode && s.server.proxyVersionMask != 0 {
				poolMask = s.server.proxyVersionMask
			}

			// Intersect with miner's requested mask
			mask := poolMask
			if raw, ok := extParams["version-rolling.mask"]; ok {
				var maskHex string
				if json.Unmarshal(raw, &maskHex) == nil {
					maskBytes, err := hex.DecodeString(maskHex)
					if err == nil && len(maskBytes) == 4 {
						minerMask := binary.BigEndian.Uint32(maskBytes)
						mask = poolMask & minerMask
					}
				}
			}

			if s.server.proxyMode && s.server.proxyVersionMask == 0 {
				// Upstream doesn't support version-rolling — reject
				result["version-rolling"] = false
				s.server.log.Infof("stratum", "session %s version-rolling denied (upstream doesn't support it)", s.ID)
			} else {
				s.versionRolling = true
				s.versionMask = mask
				result["version-rolling"] = true
				result["version-rolling.mask"] = fmt.Sprintf("%08x", mask)
				s.server.log.Infof("stratum", "session %s version-rolling enabled (mask=%08x)", s.ID, mask)
			}
		case "minimum-difficulty":
			// Accept minimum difficulty from the miner
			if raw, ok := extParams["minimum-difficulty.value"]; ok {
				var minDiffVal float64
				if json.Unmarshal(raw, &minDiffVal) == nil && minDiffVal > 0 {
					// Clamp to our bounds
					poolMin := s.server.vardiffMgr.config.MinDiff
					if minDiffVal < poolMin {
						minDiffVal = poolMin
					}
					poolMax := s.server.vardiffMgr.config.MaxDiff
					if poolMax > 0 && minDiffVal > poolMax {
						minDiffVal = poolMax
					}
					s.currentDiff = minDiffVal
					result["minimum-difficulty"] = true
					s.server.log.Infof("stratum", "session %s minimum-difficulty set to %.6f", s.ID, minDiffVal)
				} else {
					result["minimum-difficulty"] = false
				}
			} else {
				result["minimum-difficulty"] = false
			}
		default:
			// Unknown extension — report as unsupported
			result[ext] = false
		}
	}

	// Send difficulty update if changed via minimum-difficulty
	s.sendResponse(req.ID, result, nil)
	if s.currentDiff != s.server.vardiffMgr.StartDiff() {
		s.sendSetDifficulty(s.currentDiff)
	}
}

func (s *Session) handleSubscribe(req *Request) {
	s.subscribed = true

	// Parse user-agent from first param (e.g. "cgminer/4.12.1", "ESP-Miner")
	if len(req.Params) > 0 {
		var ua string
		if json.Unmarshal(req.Params[0], &ua) == nil && ua != "" {
			s.userAgent = ua
		}
	}

	// Auto-detect start difficulty from miner type (only if no explicit
	// mining.suggest_difficulty was received, which takes priority)
	if s.userAgent != "" && s.suggestedDiff == 0 {
		uaDiff := s.server.vardiffMgr.StartDiffForUA(s.userAgent)
		if uaDiff != s.currentDiff {
			s.currentDiff = uaDiff
			s.server.log.Infof("stratum", "UA auto-detect: %s start difficulty -> %.6f", s.userAgent, uaDiff)
		}
	}

	// Response: [[["mining.set_difficulty", sub_id], ["mining.notify", sub_id]], extranonce1, extranonce2_size]
	subscriptions := [][]string{
		{"mining.set_difficulty", s.ID},
		{"mining.notify", s.ID},
	}

	result := []interface{}{
		subscriptions,
		s.extranonce1,
		s.server.extranonce2Size,
	}

	s.sendResponse(req.ID, result, nil)

	// Send initial difficulty after subscribe response
	s.sendSetDifficulty(s.currentDiff)

	s.server.log.Infof("stratum", "miner %s subscribed (extranonce1=%s ua=%s)", s.conn.RemoteAddr(), s.extranonce1, s.userAgent)
}

func (s *Session) handleAuthorize(req *Request) {
	if !s.subscribed {
		s.sendResponse(req.ID, false, NewError(ErrNotSubscribed, "not subscribed"))
		return
	}

	workerName, _ := ParamString(req.Params, 0)
	if workerName == "" {
		s.sendResponse(req.ID, false, NewError(ErrUnauthorized, "empty worker name"))
		return
	}

	s.workerName = workerName
	s.authorized = true

	s.sendResponse(req.ID, true, nil)
	s.server.log.Infof("stratum", "miner %s authorized as %s", s.conn.RemoteAddr(), workerName)

	// In proxy mode, set difficulty to upstream diff immediately.
	// In solo mode, restore last known difficulty for this worker.
	if s.server.proxyMode {
		if upDiff := s.server.UpstreamDifficulty(); upDiff > 0 {
			s.currentDiff = upDiff
			s.sendSetDifficulty(upDiff)
		}
	} else if s.server.LookupWorkerDiff != nil && s.currentDiff == s.server.vardiffMgr.StartDiff() {
		if stored := s.server.LookupWorkerDiff(workerName); stored > 0 {
			// Clamp to pool bounds
			minDiff := s.server.vardiffMgr.config.MinDiff
			maxDiff := s.server.vardiffMgr.config.MaxDiff
			if stored < minDiff {
				stored = minDiff
			}
			if maxDiff > 0 && stored > maxDiff {
				stored = maxDiff
			}
			s.currentDiff = stored
			s.sendSetDifficulty(stored)
			s.server.log.Infof("stratum", "restored difficulty %.6f for %s", stored, workerName)
		}
	}

	// Notify callbacks
	if s.server.OnMinerConnected != nil {
		s.server.OnMinerConnected(s.toMinerInfo())
	}

	// Send current job if available
	s.server.sendCurrentJob(s)
}

func (s *Session) handleSubmit(req *Request) {
	if !s.authorized {
		s.sendResponse(req.ID, false, NewError(ErrUnauthorized, "not authorized"))
		return
	}

	worker, _ := ParamString(req.Params, 0)
	jobID, _ := ParamJobID(req.Params, 1)
	en2, _ := ParamString(req.Params, 2)
	ntime, _ := ParamString(req.Params, 3)
	nonce, _ := ParamString(req.Params, 4)

	// Optional 6th param: version bits (from version-rolling miners)
	versionBits, _ := ParamString(req.Params, 5)

	// Fix extranonce2 length: silently pad or truncate broken clients (matches ckpool behavior).
	expectedEN2Len := s.server.extranonce2Size * 2
	if len(en2) != expectedEN2Len {
		if len(en2) > expectedEN2Len {
			// Truncate to expected length
			s.server.log.Debugf("stratum", "truncated en2 from %d to %d chars for %s", len(en2), expectedEN2Len, s.workerName)
			en2 = en2[:expectedEN2Len]
		} else if len(en2) > 0 {
			// Pad with leading zeros
			for len(en2) < expectedEN2Len {
				en2 = "0" + en2
			}
			s.server.log.Debugf("stratum", "padded en2 to %s for %s", en2, s.workerName)
		}
	}

	sub := ShareSubmission{
		WorkerName:  worker,
		JobID:       jobID,
		Extranonce2: en2,
		NTime:       ntime,
		Nonce:       nonce,
		VersionBits: versionBits,
		VersionMask: s.versionMask,
	}

	s.server.log.Debugf("stratum", "share submit from %s: job=%q en1=%s en2=%s ntime=%s nonce=%s vbits=%s en2size=%d",
		s.workerName, jobID, s.extranonce1, en2, ntime, nonce, versionBits, s.server.extranonce2Size)

	shareReceived := time.Now()

	// Count ALL shares at entry point (before validation) for proxy accounting
	if s.server.proxyMode {
		s.server.proxySharesIn.Add(1)
	}

	result, stratumErr := s.server.shareValidator.ValidateShare(s.extranonce1, sub)
	if stratumErr != nil {
		s.sendResponse(req.ID, false, stratumErr)

		// Duplicate shares are normal ASIC behavior (BM1366 result buffer
		// re-reads) — don't count them as rejections or fire callbacks.
		// Matches ckpool which silently drops duplicates.
		if stratumErr.Code == ErrDuplicate {
			s.sharesDuped++
			if s.server.proxyMode {
				s.server.proxySharesDupe.Add(1)
			}
			s.server.log.Debugf("stratum", "duplicate share from %s (job=%q en2=%s nonce=%s vbits=%s)",
				s.workerName, jobID, en2, nonce, versionBits)
			return
		}

		// Track stale jobs in proxy mode — these are shares we'll never forward
		if s.server.proxyMode && stratumErr.Code == ErrStaleJob {
			s.server.proxySharesStale.Add(1)
			s.server.log.Infof("proxy", "[SHARE-STALE] miner=%s job=%q — share lost (not forwarded)",
				s.workerName, jobID)
		}

		s.sharesRejected++
		if s.server.OnShareRejected != nil {
			s.server.OnShareRejected(s.ID, stratumErr.Message)
		}
		s.server.log.Infof("stratum", "share REJECTED from %s: %s (job=%q en1=%s en2=%s ntime=%s nonce=%s vbits=%s)",
			s.workerName, stratumErr.Message, jobID, s.extranonce1, en2, ntime, nonce, versionBits)
		return
	}

	s.sharesAccepted++
	if result.Difficulty > s.bestDifficulty {
		s.bestDifficulty = result.Difficulty
	}

	s.sendResponse(req.ID, true, nil)

	// Determine effective difficulty for qualifying shares.
	// In proxy mode, use upstream difficulty — it's the stable threshold
	// that matters. Local vardiff oscillates wildly for ASIC miners whose
	// hardware filter (suggest_difficulty) ignores pool difficulty changes.
	// In solo mode, use session difficulty with grace period for in-flight shares.
	effectiveDiff := s.currentDiff
	if s.server.proxyMode {
		if upDiff := s.server.UpstreamDifficulty(); upDiff > 0 {
			effectiveDiff = upDiff
		}
	} else if s.oldDiff > 0 && s.diffChangeJobID != "" {
		submitJobNum, _ := strconv.ParseUint(jobID, 16, 64)
		changeJobNum, _ := strconv.ParseUint(s.diffChangeJobID, 16, 64)
		if submitJobNum > 0 && submitJobNum <= changeJobNum {
			effectiveDiff = s.oldDiff
		}
	}
	meetsTarget := result.Difficulty >= effectiveDiff
	if meetsTarget {
		s.server.vardiffMgr.RecordQualifyingShare(s.vardiffState)
	}

	// In proxy mode, skip vardiff — upstream diff is relayed proactively
	// by SetUpstreamDifficulty() when the pool changes it.
	// In solo mode, vardiff runs normally.
	if !s.server.proxyMode {
		if newDiff, changed := s.server.vardiffMgr.CheckRetarget(s.vardiffState, s.currentDiff, s.suggestedDiff); changed {
			// Record grace period: shares for jobs before the next one use the old diff
			s.oldDiff = s.currentDiff
			if curJob := s.server.currentJob(); curJob != nil {
				s.diffChangeJobID = curJob.ID
			}
			s.currentDiff = newDiff
			s.sendSetDifficulty(newDiff)
			s.server.log.Infof("stratum", "vardiff: %s difficulty -> %.6f", s.workerName, newDiff)
			if s.server.OnDiffChanged != nil && s.workerName != "" {
				s.server.OnDiffChanged(s.workerName, newDiff)
			}
		}
	}

	// Hashrate: record qualifying shares for estimation.
	// Use min(actualDiff, sessionDiff). In proxy mode, sessionDiff is locked
	// to upstream diff so the hashrate estimate matches the pool's estimate.
	// In solo mode, sessionDiff is the vardiff level.
	var hashrateDiff float64
	if meetsTarget {
		hashrateDiff = s.currentDiff
		if result.Difficulty < hashrateDiff {
			hashrateDiff = result.Difficulty
		}
	}

	if s.server.OnShareAccepted != nil {
		s.server.OnShareAccepted(s.ID, hashrateDiff, result.Difficulty)
	}

	// Proxy mode: instrument and forward qualifying shares upstream
	if s.server.proxyMode {
		upDiff := s.server.UpstreamDifficulty()
		s.server.proxySharesValid.Add(1)

		// Per-share diagnostic: shows every share with all difficulty levels
		s.server.log.Infof("proxy", "[SHARE-IN] miner=%s actualDiff=%.2f sessionDiff=%.2f upstreamDiff=%.2f meetsSession=%v meetsUpstream=%v",
			s.workerName, result.Difficulty, effectiveDiff, upDiff, meetsTarget, result.Difficulty >= upDiff)

		if s.server.OnShareForward != nil && upDiff > 0 && result.Difficulty >= upDiff {
			s.server.proxySharesFwd.Add(1)
			minerPrefix := s.extranonce1[len(s.server.upstreamEN1):]
			fullEN2 := minerPrefix + en2
			accepted, reason := s.server.OnShareForward(s.workerName, jobID, fullEN2, ntime, nonce, versionBits)
			latency := time.Since(shareReceived)

			if accepted {
				s.server.proxySharesUpAccept.Add(1)
				s.server.log.Infof("proxy", "[SHARE-FWD] miner=%s ACCEPTED latency=%v job=%s diff=%.2f upDiff=%.2f",
					s.workerName, latency, jobID, result.Difficulty, upDiff)
			} else {
				s.server.proxySharesUpReject.Add(1)
				s.server.log.Infof("proxy", "[SHARE-FWD] miner=%s REJECTED reason=%q latency=%v job=%s diff=%.2f upDiff=%.2f en2=%s",
					s.workerName, reason, latency, jobID, result.Difficulty, upDiff, fullEN2)
			}
		} else if upDiff > 0 && result.Difficulty < upDiff {
			s.server.proxySharesBelow.Add(1)
		}
	}

	// Block found
	if result.BlockFound {
		if s.server.proxyMode {
			// In proxy mode, the share was already forwarded upstream
			s.server.log.Infof("stratum", "BLOCK CANDIDATE by %s! Hash: %s (forwarded upstream)", s.workerName, result.BlockHash)
			if s.server.OnBlockFound != nil {
				s.server.OnBlockFound(result.BlockHash, 0, true)
			}
		} else {
			// Solo mode: submit to node
			height := s.server.currentJob().Template.Height
			s.server.log.Infof("stratum", "BLOCK CANDIDATE by %s! Hash: %s — submitting to node...", s.workerName, result.BlockHash)

			accepted := false
			if result.BlockHex != "" && s.server.nodeClient != nil {
				if err := s.server.nodeClient.SubmitBlock(result.BlockHex); err != nil {
					s.server.log.Errorf("stratum", "block REJECTED by node: %v", err)
				} else {
					s.server.log.Infof("stratum", "BLOCK ACCEPTED by node! Hash: %s Height: %d", result.BlockHash, height)
					accepted = true
				}
			} else {
				s.server.log.Errorf("stratum", "block candidate but no node client or block hex available")
			}

			if s.server.OnBlockFound != nil {
				s.server.OnBlockFound(result.BlockHash, height, accepted)
			}
		}
	}
}

func (s *Session) handleSuggestDifficulty(req *Request) {
	diff, err := ParamFloat(req.Params, 0)
	if err != nil {
		s.sendResponse(req.ID, false, NewError(ErrOther, "invalid difficulty"))
		return
	}

	// Clamp to our bounds
	minDiff := s.server.vardiffMgr.config.MinDiff
	if diff < minDiff {
		diff = minDiff
	}
	maxDiff := s.server.vardiffMgr.config.MaxDiff
	if maxDiff > 0 && diff > maxDiff {
		diff = maxDiff
	}

	s.suggestedDiff = diff
	// Record grace period for in-flight shares
	s.oldDiff = s.currentDiff
	if curJob := s.server.currentJob(); curJob != nil {
		s.diffChangeJobID = curJob.ID
	}
	s.currentDiff = diff
	s.sendSetDifficulty(diff)
	s.sendResponse(req.ID, true, nil)
	s.server.log.Infof("stratum", "miner %s suggested difficulty: %.6f", s.workerName, diff)
}


func (s *Session) sendNotify(job *Job, cleanJobs bool) {
	params := []interface{}{
		job.ID,
		job.PrevHash,
		job.Coinbase1,
		job.Coinbase2,
		job.MerkleBranches,
		job.Version,
		job.NBits,
		job.NTime,
		cleanJobs,
	}
	s.send(EncodeNotification("mining.notify", params))
}

func (s *Session) sendSetDifficulty(diff float64) {
	params := []interface{}{diff}
	s.send(EncodeNotification("mining.set_difficulty", params))
}

// setProxyDiff updates session difficulty from upstream and notifies the miner.
func (s *Session) setProxyDiff(diff float64) {
	if s.currentDiff == diff {
		return
	}
	s.oldDiff = s.currentDiff
	if curJob := s.server.currentJob(); curJob != nil {
		s.diffChangeJobID = curJob.ID
	}
	s.currentDiff = diff
	s.sendSetDifficulty(diff)
}

// sendReconnect tells the miner to disconnect and reconnect after waitSec.
// Supports cgminer, BFGminer, and many firmware variants.
func (s *Session) sendReconnect(waitSec int) {
	params := []interface{}{"", 0, waitSec}
	s.send(EncodeNotification("client.reconnect", params))
}

func (s *Session) sendResponse(id interface{}, result interface{}, stratumErr *StratumError) {
	s.send(EncodeResponse(id, result, stratumErr))
}

func (s *Session) send(data []byte) {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	s.conn.Write(data)
}

func (s *Session) toMinerInfo() MinerInfo {
	return MinerInfo{
		ID:             s.ID,
		WorkerName:     s.workerName,
		UserAgent:      s.userAgent,
		IPAddress:      s.conn.RemoteAddr().String(),
		ConnectedAt:    s.connectedAt,
		CurrentDiff:    s.currentDiff,
		SharesAccepted: s.sharesAccepted,
		SharesRejected: s.sharesRejected,
		BestDifficulty: s.bestDifficulty,
	}
}

// MinerInfo is the public info about a connected miner.
type MinerInfo struct {
	ID             string    `json:"id"`
	WorkerName     string    `json:"workerName"`
	UserAgent      string    `json:"userAgent"`
	IPAddress      string    `json:"ipAddress"`
	ConnectedAt    time.Time `json:"connectedAt"`
	CurrentDiff    float64   `json:"currentDiff"`
	Hashrate       float64   `json:"hashrate"`
	SharesAccepted uint64    `json:"sharesAccepted"`
	SharesRejected uint64    `json:"sharesRejected"`
	BestDifficulty float64   `json:"bestDifficulty"`
	LastShareTime  time.Time `json:"lastShareTime"`
}

// Ensure MinerInfo implements json.Marshaler if needed
var _ json.Marshaler = (*MinerInfo)(nil)

func (m *MinerInfo) MarshalJSON() ([]byte, error) {
	type Alias MinerInfo
	return json.Marshal(&struct {
		ConnectedAt string `json:"connectedAt"`
		LastShareTime string `json:"lastShareTime"`
		*Alias
	}{
		ConnectedAt:   m.ConnectedAt.Format(time.RFC3339),
		LastShareTime: m.LastShareTime.Format(time.RFC3339),
		Alias:         (*Alias)(m),
	})
}
