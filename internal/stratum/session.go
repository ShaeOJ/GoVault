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

	"govault/internal/upstream"
)

// Session represents a single miner connection.
type Session struct {
	ID          string
	conn        net.Conn
	server      *Server
	extranonce1 string
	remoteIP    string
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
	sharesStale    uint64
	bestDifficulty float64

	suggestedDiff float64 // from mining.suggest_difficulty (miner's threshold)

	// Per-miner pass-through: dedicated upstream connection for this miner.
	// When set, shares are submitted directly through this connection and the
	// miner appears as an independent connection to the upstream pool.
	dedicatedUpstream *upstream.Client
	localEN2Size      int // EN2 size from dedicated upstream (overrides server.extranonce2Size)

	// Difficulty transition grace period (matches ckpool diff_change_job_id).
	// Shares for jobs issued before diffChangeJobID are validated against oldDiff.
	oldDiff          float64
	diffChangeJobID  string

	// diffMu protects currentDiff, oldDiff, diffChangeJobID, and the share
	// counters above. These are written by Handle() and read/written by
	// setProxyDiff() (from the server goroutine) and GetProxyDiagnostics().
	diffMu sync.Mutex

	// Security guards — single-threaded, owned by Handle().
	msgLimiter *msgLimiter
	spamGuard  *spamGuard
}

func newSession(id string, conn net.Conn, server *Server, extranonce1 string) *Session {
	now := time.Now()
	cfg := server.config

	// Build security guards from config; use safe fallbacks for unconfigured installs.
	msgRate := cfg.MaxMsgPerSec
	msgBurst := float64(cfg.MaxMsgBurst)
	if msgRate <= 0 {
		msgRate = 0 // disabled
	}
	if msgBurst <= 0 && msgRate > 0 {
		msgBurst = msgRate * 2.5
	}

	invalidThreshold := cfg.InvalidShareThreshold
	invalidWindow := cfg.InvalidShareWindow
	if invalidWindow <= 0 {
		invalidWindow = 10
	}

	return &Session{
		ID:           id,
		conn:         conn,
		server:       server,
		extranonce1:  extranonce1,
		currentDiff:  server.vardiffMgr.StartDiff(),
		connectedAt:  now,
		lastActivity: now,
		reader:       bufio.NewReaderSize(conn, 4096),
		msgLimiter:   newMsgLimiter(msgRate, msgBurst),
		spamGuard:    newSpamGuard(invalidWindow, invalidThreshold),
	}
}

// Handle is the main loop for processing messages from a miner.
func (s *Session) Handle() {
	defer func() {
		if r := recover(); r != nil {
			s.server.log.Errorf("stratum", "session %s panic: %v", s.ID, r)
		}
		// Stop dedicated upstream before closing so its goroutines exit cleanly.
		if s.dedicatedUpstream != nil {
			s.dedicatedUpstream.Stop()
		}
		s.conn.Close()
		s.server.removeSession(s)
	}()

	// Initialize vardiff state
	s.vardiffState = s.server.vardiffMgr.NewState()

	// Auth timeout: disconnect if the miner hasn't authorized within the configured window.
	authTimeoutSec := s.server.config.AuthTimeoutSec
	if authTimeoutSec <= 0 {
		authTimeoutSec = 30
	}
	authDeadline := s.connectedAt.Add(time.Duration(authTimeoutSec) * time.Second)

	for {
		// Use retarget interval as read deadline so idle sessions get
		// periodic vardiff checks (halving difficulty when no shares arrive).
		retargetInterval := s.server.vardiffMgr.RetargetInterval()
		s.conn.SetReadDeadline(time.Now().Add(retargetInterval))

		line, err := s.reader.ReadBytes('\n')
		if err != nil {
			// Timeout → idle vardiff check (don't disconnect yet)
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				// Auth timeout: drop connection if not yet authorized.
				if !s.authorized && time.Now().After(authDeadline) {
					s.server.log.Warnf("stratum", "auth timeout for %s, disconnecting", s.conn.RemoteAddr())
					return
				}
				// True inactivity (no data at all for 5 min) → disconnect
				if time.Since(s.lastActivity) > 5*time.Minute {
					return
				}
				// Idle vardiff: halve difficulty if no qualifying shares arrived.
				// Skip in proxy mode — upstream pool controls difficulty entirely.
				if s.authorized && s.vardiffState != nil && !s.server.proxyMode {
					s.diffMu.Lock()
					curDiff := s.currentDiff
					s.diffMu.Unlock()
					if newDiff, changed := s.server.vardiffMgr.CheckRetarget(s.vardiffState, curDiff, s.suggestedDiff); changed {
						s.diffMu.Lock()
						s.oldDiff = s.currentDiff
						if curJob := s.server.currentJob(); curJob != nil {
							s.diffChangeJobID = curJob.ID
						}
						s.currentDiff = newDiff
						s.diffMu.Unlock()
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

		// Enforce max line size — a legitimate miner never sends kilobytes of JSON.
		// An oversized line means something is very wrong; disconnect and ban.
		if maxKB := s.server.config.MaxLineSizeKB; maxKB > 0 && len(line) > maxKB*1024 {
			s.server.log.Warnf("stratum", "session %s line too large (%d bytes, max %d KB) — banning %s",
				s.ID, len(line), maxKB, s.remoteIP)
			s.server.BanIP(s.remoteIP, "oversized stratum message")
			return
		}

		// Enforce per-session message rate limit. Legitimate ASIC miners submit at
		// most a few shares per second; flooding is a clear sign of abuse.
		if s.server.config.MaxMsgPerSec > 0 && !s.msgLimiter.allow() {
			s.server.log.Warnf("stratum", "session %s message rate exceeded — banning %s", s.ID, s.remoteIP)
			s.server.BanIP(s.remoteIP, "message rate limit exceeded")
			return
		}

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
					s.diffMu.Lock()
					s.currentDiff = minDiffVal
					s.diffMu.Unlock()
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
	s.diffMu.Lock()
	curDiff := s.currentDiff
	s.diffMu.Unlock()
	if curDiff != s.server.vardiffMgr.StartDiff() {
		s.sendSetDifficulty(curDiff)
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

	password, _ := ParamString(req.Params, 1)

	// Per-miner pass-through mode: create a dedicated upstream connection for
	// this miner using their own credentials. They appear on the upstream pool
	// as an independent connection rather than sharing one with other miners.
	if s.server.OnMinerConnect != nil {
		uc, err := s.server.OnMinerConnect(s, workerName, password)
		if err != nil {
			s.sendResponse(req.ID, false, NewError(ErrUnauthorized, "upstream connect failed: "+err.Error()))
			s.server.log.Errorf("stratum", "miner %s per-miner upstream failed: %v", workerName, err)
			return
		}

		// Disable prefix carving — this miner owns the full EN2 space.
		uc.SetPerMinerMode()
		s.dedicatedUpstream = uc
		s.extranonce1 = uc.Extranonce1()
		s.localEN2Size = uc.LocalEN2Size()

		// Tighten the session's version-rolling mask to match the upstream's.
		if uc.VersionRolling() && uc.VersionMask() != "" {
			if b, err2 := hex.DecodeString(uc.VersionMask()); err2 == nil && len(b) == 4 {
				upMask := binary.BigEndian.Uint32(b)
				s.versionMask &= upMask
			}
		} else {
			s.versionRolling = false
			s.versionMask = 0
		}

		// Wire per-miner upstream callbacks (all captured on this session).
		uc.OnJob = func(params *upstream.JobParams) {
			if !s.server.IsRunning() {
				return
			}
			job := s.server.jobManager.RegisterUpstreamJob(
				params.JobID, params.PrevHash, params.Coinbase1, params.Coinbase2,
				params.MerkleBranches, params.Version, params.NBits, params.NTime, params.CleanJobs,
			)
			s.server.shareValidator.CleanDuplicates(s.server.jobManager.ActiveJobIDs())
			s.sendNotify(job, params.CleanJobs)
			s.server.log.Debugf("stratum", "[per-miner] job %s → %s", params.JobID, s.workerName)
		}
		uc.OnDifficulty = func(diff float64) {
			s.setProxyDiff(diff)
		}
		uc.OnDisconnect = func(connErr error) {
			// Pool dropped this miner's connection — close so they reconnect.
			s.server.log.Errorf("stratum", "[per-miner] upstream disconnected for %s: %v — kicking miner", s.workerName, connErr)
			s.conn.Close()
		}
		uc.OnReconnect = func() {
			// New EN1 assigned — tell miner to reconnect and get fresh EN1.
			s.server.log.Infof("stratum", "[per-miner] upstream reconnected for %s — sending reconnect", s.workerName)
			s.sendReconnect(3)
			s.conn.Close()
		}

		// Notify miner of real EN1 (replaces the placeholder sent at subscribe).
		s.sendSetExtranonce(s.extranonce1, s.localEN2Size)

		// Apply upstream difficulty.
		if upDiff := uc.UpstreamDifficulty(); upDiff > 0 {
			s.diffMu.Lock()
			s.currentDiff = upDiff
			s.diffMu.Unlock()
			s.sendSetDifficulty(upDiff)
		}

		s.workerName = workerName
		s.authorized = true
		s.sendResponse(req.ID, true, nil)
		s.server.log.Infof("stratum", "miner %s authorized as %s (per-miner upstream, en1=%s en2=%d)",
			s.conn.RemoteAddr(), workerName, s.extranonce1, s.localEN2Size)

		if s.server.OnMinerConnected != nil {
			s.server.OnMinerConnected(s.toMinerInfo())
		}

		// Send earliest available job from this miner's upstream.
		if earlyJob := uc.DrainEarlyJob(); earlyJob != nil {
			job := s.server.jobManager.RegisterUpstreamJob(
				earlyJob.JobID, earlyJob.PrevHash, earlyJob.Coinbase1, earlyJob.Coinbase2,
				earlyJob.MerkleBranches, earlyJob.Version, earlyJob.NBits, earlyJob.NTime, earlyJob.CleanJobs,
			)
			s.sendNotify(job, earlyJob.CleanJobs)
		} else {
			s.server.sendCurrentJob(s)
		}
		return
	}

	// Shared upstream mode: forward the miner's credentials to the shared upstream pool
	// connection so Firepool sees wallet.workername and attributes shares correctly.
	// OnMinerAuthorize is nil in solo mode — miners are accepted locally.
	if s.server.OnMinerAuthorize != nil {
		ok, reason := s.server.OnMinerAuthorize(workerName, password)
		if !ok {
			if reason == "" {
				reason = "authorization rejected by upstream pool"
			}
			s.sendResponse(req.ID, false, NewError(ErrUnauthorized, reason))
			s.server.log.Infof("stratum", "miner %s rejected (%s): %s", s.conn.RemoteAddr(), workerName, reason)
			return
		}
	}

	s.workerName = workerName
	s.authorized = true

	s.sendResponse(req.ID, true, nil)
	s.server.log.Infof("stratum", "miner %s authorized as %s", s.conn.RemoteAddr(), workerName)

	// In proxy mode, set difficulty to upstream diff immediately.
	// In solo mode, restore last known difficulty for this worker.
	if s.server.proxyMode {
		if upDiff := s.server.UpstreamDifficulty(); upDiff > 0 {
			s.diffMu.Lock()
			s.currentDiff = upDiff
			s.diffMu.Unlock()
			s.sendSetDifficulty(upDiff)
		}
	} else if s.server.LookupWorkerDiff != nil {
		s.diffMu.Lock()
		isDefault := s.currentDiff == s.server.vardiffMgr.StartDiff()
		s.diffMu.Unlock()
		if isDefault {
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
				s.diffMu.Lock()
				s.currentDiff = stored
				s.diffMu.Unlock()
				s.sendSetDifficulty(stored)
				s.server.log.Infof("stratum", "restored difficulty %.6f for %s", stored, workerName)
			}
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
	// Use per-session EN2 size when set (per-miner upstream mode).
	en2SizeToUse := s.server.extranonce2Size
	if s.localEN2Size > 0 {
		en2SizeToUse = s.localEN2Size
	}
	expectedEN2Len := en2SizeToUse * 2
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

	// Validate ntime is within the allowed drift window. Shares with wildly
	// wrong timestamps are always invalid and indicate a misconfigured or
	// malicious device. Accept gracefully during the window fill period.
	if maxDrift := s.server.config.NTimeMaxDriftSec; maxDrift > 0 && len(ntime) == 8 {
		if ntimeVal, ntErr := strconv.ParseUint(ntime, 16, 32); ntErr == nil {
			now := time.Now().Unix()
			delta := int64(ntimeVal) - now
			if delta < -int64(maxDrift) || delta > int64(maxDrift) {
				s.server.log.Warnf("stratum", "share from %s rejected: ntime %s is %ds from now (max ±%ds)",
					s.workerName, ntime, delta, maxDrift)
				s.sendResponse(req.ID, false, NewError(ErrOther, "ntime out of range"))
				if s.spamGuard.record(false) {
					s.server.log.Warnf("stratum", "invalid share threshold exceeded for %s — banning %s",
						s.workerName, s.remoteIP)
					s.server.BanIP(s.remoteIP, "invalid share spam")
					return
				}
				return
			}
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
			s.diffMu.Lock()
			s.sharesDuped++
			s.diffMu.Unlock()
			if s.server.proxyMode {
				s.server.proxySharesDupe.Add(1)
			}
			s.server.log.Debugf("stratum", "duplicate share from %s (job=%q en2=%s nonce=%s vbits=%s)",
				s.workerName, jobID, en2, nonce, versionBits)
			return
		}

		// Stale shares are normal — they arrive when an in-flight share lands
		// after the job has been evicted (server restart, reconnect, rapid job
		// cycling). Matches ckpool which counts these as "discarded", not
		// "rejected". Don't fire OnShareRejected so the dashboard rejected
		// counter stays clean.
		if stratumErr.Code == ErrStaleJob {
			s.diffMu.Lock()
			s.sharesStale++
			s.diffMu.Unlock()
			if s.server.proxyMode {
				s.server.proxySharesStale.Add(1)
				s.server.log.Infof("proxy", "[SHARE-STALE] miner=%s job=%q — share lost (not forwarded)",
					s.workerName, jobID)
			} else {
				s.server.log.Debugf("stratum", "stale share from %s (job=%q)", s.workerName, jobID)
			}
			return
		}

		s.diffMu.Lock()
		s.sharesRejected++
		s.diffMu.Unlock()
		if s.server.OnShareRejected != nil {
			s.server.OnShareRejected(s.ID, stratumErr.Message)
		}
		s.server.log.Infof("stratum", "share REJECTED from %s: %s (job=%q en1=%s en2=%s ntime=%s nonce=%s vbits=%s)",
			s.workerName, stratumErr.Message, jobID, s.extranonce1, en2, ntime, nonce, versionBits)

		// Feed the spam guard with this rejection. If the sliding-window rejection
		// rate exceeds the threshold, disconnect and ban the IP.
		if s.server.config.InvalidShareThreshold > 0 && s.spamGuard.record(false) {
			s.server.log.Warnf("stratum", "invalid share threshold exceeded for %s — banning %s",
				s.workerName, s.remoteIP)
			s.server.BanIP(s.remoteIP, "invalid share spam")
			return // closes connection via deferred cleanup in Handle()
		}
		return
	}

	// Valid share — record as accepted in spam guard.
	if s.server.config.InvalidShareThreshold > 0 {
		s.spamGuard.record(true)
	}

	s.sendResponse(req.ID, true, nil)

	// Lock diff fields and counters for the entire accounting section.
	// setProxyDiff() writes these from the server goroutine concurrently.
	s.diffMu.Lock()
	s.sharesAccepted++
	if result.Difficulty > s.bestDifficulty {
		s.bestDifficulty = result.Difficulty
	}

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
	// In proxy mode, use effectiveDiff (upstream diff) — NOT currentDiff.
	// currentDiff can be overridden by mining.suggest_difficulty to the miner's
	// hardware filter threshold (e.g. 512 for BM1366), which may be lower than
	// the upstream pool's actual difficulty (e.g. 1024+). Using currentDiff in
	// that case causes severe hashrate underestimation.
	// In solo mode, use currentDiff (the vardiff level).
	var hashrateDiff float64
	if meetsTarget {
		base := s.currentDiff
		if s.server.proxyMode {
			base = effectiveDiff // already holds upstream diff
		}
		hashrateDiff = base
		if result.Difficulty < hashrateDiff {
			hashrateDiff = result.Difficulty
		}
	}
	s.diffMu.Unlock()

	if s.server.OnShareAccepted != nil {
		s.server.OnShareAccepted(s.ID, hashrateDiff, result.Difficulty)
	}

	// Proxy mode: instrument and forward qualifying shares upstream
	if s.server.proxyMode {
		var upDiff float64
		if s.dedicatedUpstream != nil {
			upDiff = s.dedicatedUpstream.UpstreamDifficulty()
		} else {
			upDiff = s.server.UpstreamDifficulty()
		}
		s.server.proxySharesValid.Add(1)

		// Per-share diagnostic: shows every share with all difficulty levels
		s.server.log.Infof("proxy", "[SHARE-IN] miner=%s actualDiff=%.2f sessionDiff=%.2f upstreamDiff=%.2f meetsSession=%v meetsUpstream=%v",
			s.workerName, result.Difficulty, effectiveDiff, upDiff, meetsTarget, result.Difficulty >= upDiff)

		if (s.dedicatedUpstream != nil || s.server.OnShareForward != nil) && upDiff > 0 && result.Difficulty >= upDiff {
			s.server.proxySharesFwd.Add(1)
			var accepted bool
			var reason string
			if s.dedicatedUpstream != nil {
				// Per-miner pass-through: submit directly through miner's own pool
				// connection. The pool knows this connection's EN1 from subscribe,
				// so we send only the miner's EN2 (no prefix reconstruction needed).
				accepted, reason = s.dedicatedUpstream.SubmitShare(s.workerName, jobID, en2, ntime, nonce, versionBits)
			} else {
				// Shared upstream: reconstruct full EN2 with the miner's EN1 prefix.
				minerPrefix := s.extranonce1[len(s.server.upstreamEN1):]
				fullEN2 := minerPrefix + en2
				accepted, reason = s.server.OnShareForward(s.workerName, jobID, fullEN2, ntime, nonce, versionBits)
			}
			latency := time.Since(shareReceived)
			s.server.recordUpstreamLatency(latency)

			if accepted {
				s.server.proxySharesUpAccept.Add(1)
				s.server.log.Infof("proxy", "[SHARE-FWD] miner=%s ACCEPTED latency=%v job=%s diff=%.2f upDiff=%.2f",
					s.workerName, latency, jobID, result.Difficulty, upDiff)
			} else {
				s.server.proxySharesUpReject.Add(1)
				s.server.log.Infof("proxy", "[SHARE-FWD] miner=%s REJECTED reason=%q latency=%v job=%s diff=%.2f upDiff=%.2f en2=%s",
					s.workerName, reason, latency, jobID, result.Difficulty, upDiff, en2)
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

	// Treat suggest_difficulty as a vardiff floor hint only — do NOT override
	// currentDiff. AxeOS/ESP-Miner sends the ASIC hardware minimum (e.g. 512
	// for BM1366), not the desired pool difficulty. Immediately applying it
	// drops currentDiff from the DB-restored steady-state value (e.g. 15 000)
	// back to 512 on every reconnect, causing vardiff to re-ramp from scratch
	// and flooding the hashrate window with low-diff shares → ~40% underestimate.
	//
	// suggestedDiff is already used as a vardiff floor via CheckRetarget's
	// floorDiff parameter. The current difficulty (set at subscribe/authorize
	// time from UA auto-detect or DB restore) remains in effect.
	//
	// In proxy mode the upstream pool is sole authority on difficulty — same ACK.
	s.sendResponse(req.ID, true, nil)
	s.server.log.Infof("stratum", "miner %s suggested difficulty: %.6f (stored as vardiff floor, currentDiff unchanged at %.6f)",
		s.workerName, diff, s.currentDiff)
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
// Called from Server.SetUpstreamDifficulty on a different goroutine than Handle().
func (s *Session) setProxyDiff(diff float64) {
	s.diffMu.Lock()
	if s.currentDiff == diff {
		s.diffMu.Unlock()
		return
	}
	s.oldDiff = s.currentDiff
	if curJob := s.server.currentJob(); curJob != nil {
		s.diffChangeJobID = curJob.ID
	}
	s.currentDiff = diff
	s.diffMu.Unlock()
	s.sendSetDifficulty(diff)
}

// sendSetExtranonce notifies the miner of a new extranonce1 and extranonce2_size.
// Used in per-miner pass-through mode to push the real upstream EN1 after
// the dedicated upstream connection is established (replacing the placeholder
// sent in the subscribe response).
func (s *Session) sendSetExtranonce(en1 string, en2Size int) {
	params := []interface{}{en1, en2Size}
	s.send(EncodeNotification("mining.set_extranonce", params))
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
	s.diffMu.Lock()
	curDiff := s.currentDiff
	accepted := s.sharesAccepted
	rejected := s.sharesRejected
	stale := s.sharesStale
	bestDiff := s.bestDifficulty
	s.diffMu.Unlock()

	return MinerInfo{
		ID:             s.ID,
		WorkerName:     s.workerName,
		UserAgent:      s.userAgent,
		IPAddress:      s.conn.RemoteAddr().String(),
		ConnectedAt:    s.connectedAt,
		CurrentDiff:    curDiff,
		SharesAccepted: accepted,
		SharesRejected: rejected,
		SharesStale:    stale,
		BestDifficulty: bestDiff,
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
	SharesStale    uint64    `json:"sharesStale"`
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
