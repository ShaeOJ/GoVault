// Package proxysplit implements dual-pool hashrate splitting for GOStratum's
// shared-broadcast proxy mode: it connects to two upstream pools simultaneously
// and automatically decides, job by job, which pool's work is handed to
// connected miners — converging statistically on a configured percentage
// split — with instant failover to the surviving pool if either disconnects.
//
// # Why this needs a virtual extranonce layer
//
// A mined share is a solution to one specific pool's coinbase/job template;
// it cannot be resubmitted to a different pool. What IS achievable is
// job-level time-slicing: at any instant all miners work whichever pool's
// job is "active", and that choice is re-rolled (weighted by the configured
// percentage) on every new job from either pool. Naively, switching the
// active pool would mean switching the miner-facing extranonce1 (since it's
// normally derived from the real pool's own EN1), forcing every connected
// miner to reconnect — and with natural job cadence (~20-60s) that would mean
// near-continuous reconnects.
//
// This package avoids that entirely by folding the real pool's EN1 into the
// job's Coinbase1 at broadcast time instead of handing it to miners as
// extranonce1. That frees up the miner-visible "extranonce region" (the real
// pool's own EN2 budget) to be subdivided into a stable, per-session virtual
// EN1 (assigned once at connect, by stratum.Server.generateExtranonce1, and
// never touched again) plus a fixed virtual EN2 size shown to every miner —
// both computed once, up front, from the SMALLER of the two pools' advertised
// EN2 sizes, so they always fit regardless of which pool is active.
//
// Byte-layout proof, for a job backed by a pool whose real EN1/EN2 are
// realEN1 (realEN1Size bytes) and realEN2Size bytes total:
//
//	virtualCoinbase1 = Coinbase1_real + realEN1
//	virtualCoinbase2 = zeroPad(padLen) + Coinbase2_real
//	padLen           = realEN2Size - prefixSize - virtualEN2Size  (bytes, >= 0 by construction)
//
// What a miner actually hashes (session.extranonce1 is the stable P-byte
// prefix, minerEN2 is whatever they fill in as their V=virtualEN2Size-byte
// "extranonce2"):
//
//	virtualCoinbase1 + sessionEN1(P) + minerEN2(V) + virtualCoinbase2
//	= Coinbase1_real + realEN1 + P-bytes + V-bytes + zeroPad(padLen) + Coinbase2_real
//
// The bracketed span between Coinbase1_real and Coinbase2_real is exactly
// realEN1Size + (P+V+padLen) = realEN1Size + realEN2Size bytes — precisely
// the real pool's reserved extranonce region — so this is a byte-for-byte
// valid coinbase for that job. stratum.ShareValidator's existing splice
// (job.Coinbase1 + extranonce1 + submittedEN2 + job.Coinbase2) needs no
// changes to validate it locally.
//
// At submit time, what gets sent to the real pool as its "extranonce2" field
// is reconstructed (in stratum/session.go) as:
//
//	realEN2 = sessionEN1(P) + minerEN2(V) + zeroPad(padLen)   // length == realEN2Size
//
// which the real pool concatenates with ITS OWN EN1 (assigned on its own
// subscribe, supplied implicitly — Stratum submit never includes EN1) to get
// Coinbase1_real + realEN1 + realEN2 + Coinbase2_real — byte-identical to what
// was actually hashed above. The share validates upstream exactly as it did
// locally, with zero protocol extension and zero miner-visible disruption.
package proxysplit

import (
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"govault/internal/logger"
	"govault/internal/stratum"
	"govault/internal/upstream"
)

const (
	LabelA = "A"
	LabelB = "B"
)

// poolLink pairs a pool label with its upstream connection.
type poolLink struct {
	label  string
	client *upstream.Client
}

// Router owns both upstream.Client connections for dual-pool hashrate
// splitting and drives a stratum.Server in split mode. See the package doc
// for the virtual-extranonce mechanism that makes pool switches invisible to
// connected miners.
type Router struct {
	log    *logger.Logger
	server *stratum.Server

	splitPercentA int // Pool A's target share, 1-99; Pool B gets the rest

	mu      sync.Mutex
	pools   map[string]*poolLink
	active  string
	healthy map[string]bool
	lastJob map[string]*upstream.JobParams

	prefixSize     int
	virtualEN2Size int

	sharesFwdA, sharesFwdB atomic.Uint64
	switchCount            atomic.Uint64
	lastSwitchAt           atomic.Int64 // UnixNano; 0 = never switched

	// OnBroadcastJob, if set, is called whenever a job is actually broadcast
	// to miners (i.e. it came from the currently-active pool) — NOT for every
	// registered-but-standby job from the inactive pool. Lets the caller
	// (appcore) do the same network-diff/block-height bookkeeping the
	// single-pool proxy path does off its upstream.Client's OnJob.
	OnBroadcastJob func(label string, params *upstream.JobParams)
}

// NewRouter constructs a Router. clientA/clientB must already be constructed
// (via upstream.NewClient) but do not need to be connected yet — Start()
// connects/verifies both. splitPercentA must be 1-99 (validated by
// config.Validate() before this is ever called).
func NewRouter(log *logger.Logger, server *stratum.Server, splitPercentA int, clientA, clientB *upstream.Client) *Router {
	return &Router{
		log:           log,
		server:        server,
		splitPercentA: splitPercentA,
		pools: map[string]*poolLink{
			LabelA: {label: LabelA, client: clientA},
			LabelB: {label: LabelB, client: clientB},
		},
		healthy: map[string]bool{LabelA: true, LabelB: true},
		lastJob: make(map[string]*upstream.JobParams),
	}
}

// Start computes the shared virtual-EN1/EN2 constants from both pools
// (which must already be connected — their Extranonce2Size()/VersionMask()
// must reflect a real subscribe response), configures the stratum server for
// split mode, wires job/difficulty/disconnect/reconnect callbacks on both
// clients, and replays any job buffered during each client's Connect() call
// before this wiring happened. Call once, after both clients' Connect()
// succeeded and before srv.Start() begins accepting miner connections.
func (r *Router) Start(defaultActive string) error {
	clientA := r.pools[LabelA].client
	clientB := r.pools[LabelB].client
	if !clientA.IsConnected() || !clientB.IsConnected() {
		return fmt.Errorf("both pools must be connected before starting split mode")
	}

	enA := clientA.Extranonce2Size()
	enB := clientB.Extranonce2Size()
	minEN2 := enA
	if enB < minEN2 {
		minEN2 = enB
	}

	if minEN2 >= 5 {
		r.prefixSize = minEN2 - 3
		if r.prefixSize > 2 {
			r.prefixSize = 2
		}
	} else {
		r.prefixSize = 0
		r.log.Infof("proxysplit", "joint EN2 size %d (min of %d, %d) is too small for per-session EN1 isolation — miners may collide in EN2 space", minEN2, enA, enB)
	}
	r.virtualEN2Size = minEN2 - r.prefixSize
	if r.virtualEN2Size < 1 {
		r.virtualEN2Size = 1
	}

	combinedMask := parseVersionMask(clientA.VersionMask()) & parseVersionMask(clientB.VersionMask())

	r.server.SetProxySplitMode(r.prefixSize, r.virtualEN2Size, combinedMask)

	if defaultActive != LabelA && defaultActive != LabelB {
		defaultActive = LabelA
	}
	r.mu.Lock()
	r.active = defaultActive
	r.mu.Unlock()
	r.server.SetActivePoolLabel(defaultActive)
	r.server.SetUpstreamDifficulty(r.pools[defaultActive].client.UpstreamDifficulty())

	r.log.Infof("proxysplit", "split mode started: prefixSize=%d virtualEN2Size=%d splitA=%d%% defaultActive=%s",
		r.prefixSize, r.virtualEN2Size, r.splitPercentA, defaultActive)

	for _, label := range [2]string{LabelA, LabelB} {
		label := label // capture
		pl := r.pools[label]
		pl.client.OnJob = func(params *upstream.JobParams) { r.handleJob(label, params, false) }
		pl.client.OnDifficulty = func(diff float64) { r.onDifficulty(label, diff) }
		pl.client.OnDisconnect = func(err error) { r.onDisconnect(label, err) }
		pl.client.OnReconnect = func() { r.onReconnect(label) }
	}

	// Replay jobs buffered during Connect() before OnJob was wired above —
	// same race the single-pool startProxy() guards against.
	for _, label := range [2]string{LabelA, LabelB} {
		if early := r.pools[label].client.DrainEarlyJob(); early != nil {
			r.handleJob(label, early, false)
		}
	}

	return nil
}

// Stop disconnects both upstream pools.
func (r *Router) Stop() {
	for _, pl := range r.pools {
		pl.client.Stop()
	}
}

// ActivePool returns the currently-active pool label ("A" or "B").
func (r *Router) ActivePool() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.active
}

// Client returns the upstream.Client for the given label, or nil.
func (r *Router) Client(label string) *upstream.Client {
	r.mu.Lock()
	defer r.mu.Unlock()
	if pl, ok := r.pools[label]; ok {
		return pl.client
	}
	return nil
}

// Switch manually flips the active pool and instantly replays its last known
// job + difficulty — no reconnect, same mechanism as an automatic re-pick.
// Exposed for a dashboard "Switch now" control; the automatic weighted split
// (handleJob) is the primary mechanism, this is an operator override.
func (r *Router) Switch(label string) error {
	if label != LabelA && label != LabelB {
		return fmt.Errorf("unknown pool %q", label)
	}
	r.mu.Lock()
	if r.active == label {
		r.mu.Unlock()
		return nil
	}
	r.active = label
	r.switchCount.Add(1)
	r.lastSwitchAt.Store(time.Now().UnixNano())
	last := r.lastJob[label]
	pl := r.pools[label]
	r.mu.Unlock()

	r.server.SetActivePoolLabel(label)
	if pl.client.IsConnected() {
		r.server.SetUpstreamDifficulty(pl.client.UpstreamDifficulty())
	}
	r.log.Infof("proxysplit", "manual switch -> %s", label)
	if last != nil {
		r.handleJob(label, last, true)
	}
	return nil
}

// handleJob is the core per-job decision point: update health/last-job state,
// decide (for a job from the currently-inactive pool) whether to switch active
// pools, then fold+register the job and broadcast it only if it's from the
// (possibly newly) active pool. forceClean forces cleanJobs=true regardless of
// the upstream flag — used when replaying a job as part of an explicit switch
// (manual or failover) where the caller already changed r.active itself.
func (r *Router) handleJob(label string, params *upstream.JobParams, forceClean bool) {
	r.mu.Lock()
	pl := r.pools[label]
	realEN2Size := pl.client.Extranonce2Size()
	realEN1 := pl.client.Extranonce1()
	r.lastJob[label] = params

	prevActive := r.active
	other := otherLabel(label)

	switch {
	case label == r.active:
		// Normal job refresh from the already-active pool — no re-pick.
	case !r.healthy[other]:
		// The active pool is down and this (healthy) pool just produced a
		// job — become active. (onDisconnect already does this proactively;
		// this covers the case where a job arrives before/without a clean
		// disconnect signal.)
		r.active = label
	case r.healthy[label]:
		// Both pools healthy: weighted random re-pick, triggered by a new
		// job from the currently-inactive pool. Natural job cadence from
		// either pool drives convergence toward splitPercentA over time.
		if label == LabelA && rand.Intn(100) < r.splitPercentA {
			r.active = label
		} else if label == LabelB && rand.Intn(100) >= r.splitPercentA {
			r.active = label
		}
	}

	active := r.active
	switched := active != prevActive
	if switched {
		r.switchCount.Add(1)
		r.lastSwitchAt.Store(time.Now().UnixNano())
	}
	r.mu.Unlock()

	if switched {
		r.server.SetActivePoolLabel(active)
		if activePl := r.pools[active]; activePl.client.IsConnected() {
			r.server.SetUpstreamDifficulty(activePl.client.UpstreamDifficulty())
		}
		r.log.Infof("proxysplit", "active pool switched %s -> %s (triggered by job %s from %s)", prevActive, active, params.JobID, label)
	}

	virtualCB1, virtualCB2 := foldJob(params, realEN1, r.prefixSize, r.virtualEN2Size, realEN2Size)
	mapID := label + ":" + params.JobID
	broadcast := label == active

	r.server.RegisterAndMaybeBroadcastSplitJob(
		mapID, params.JobID, params.PrevHash, virtualCB1, virtualCB2,
		params.MerkleBranches, params.Version, params.NBits, params.NTime,
		params.CleanJobs || switched || forceClean,
		label, realEN2Size, broadcast,
	)

	if broadcast && r.OnBroadcastJob != nil {
		r.OnBroadcastJob(label, params)
	}
}

// onDifficulty relays a pool's difficulty change to miners only if that pool
// is currently active — matches single-pool proxy mode's existing behavior,
// just gated by which of the two pools is live right now.
func (r *Router) onDifficulty(label string, diff float64) {
	r.mu.Lock()
	active := r.active
	r.mu.Unlock()
	if label == active {
		r.server.SetUpstreamDifficulty(diff)
	}
}

// onDisconnect marks a pool unhealthy and, if it was the active pool, forces
// an immediate failover to the surviving pool — replaying its last known job
// so miners don't sit idle until its next natural job tick. No miner is ever
// reconnected by this path.
func (r *Router) onDisconnect(label string, err error) {
	r.log.Errorf("proxysplit", "pool %s disconnected: %v", label, err)

	r.mu.Lock()
	r.healthy[label] = false
	wasActive := r.active == label
	other := otherLabel(label)
	if wasActive {
		r.active = other
		r.switchCount.Add(1)
		r.lastSwitchAt.Store(time.Now().UnixNano())
	}
	lastOtherJob := r.lastJob[other]
	otherPl := r.pools[other]
	r.mu.Unlock()

	if !wasActive {
		return
	}

	r.server.SetActivePoolLabel(other)
	r.log.Infof("proxysplit", "failover: %s down, switching to %s", label, other)
	if otherPl.client.IsConnected() {
		r.server.SetUpstreamDifficulty(otherPl.client.UpstreamDifficulty())
	}
	if lastOtherJob != nil {
		r.handleJob(other, lastOtherJob, true)
	}
}

// onReconnect marks a pool healthy again. It deliberately does NOT force an
// immediate switch back — handleJob's weighted re-pick resumes naturally on
// the next job event from either pool, so recovery is smooth rather than an
// abrupt mid-job snap back.
func (r *Router) onReconnect(label string) {
	r.log.Infof("proxysplit", "pool %s reconnected", label)
	r.mu.Lock()
	r.healthy[label] = true
	r.mu.Unlock()
}

// SubmitShare resolves the job's source pool (which may be the currently
// inactive one — its connection stays live as a hot standby specifically so
// this still works) and forwards the share using that pool's OWN job ID
// (job.RealJobID), not the router's namespaced map-key ID. Wired as
// stratum.Server.OnShareForward.
func (r *Router) SubmitShare(worker, jobID, realEN2, ntime, nonce, versionBits string) (bool, string) {
	job := r.server.GetJob(jobID)
	if job == nil {
		return false, "stale job"
	}
	r.mu.Lock()
	pl, ok := r.pools[job.SourcePool]
	r.mu.Unlock()
	if !ok {
		return false, "unknown source pool for job"
	}

	accepted, reason := pl.client.SubmitShare(worker, job.RealJobID, realEN2, ntime, nonce, versionBits)
	if job.SourcePool == LabelA {
		r.sharesFwdA.Add(1)
	} else {
		r.sharesFwdB.Add(1)
	}
	return accepted, reason
}

// Status is a snapshot of split-mode diagnostics for the dashboard.
type Status struct {
	ActivePool    string
	SplitPercentA int
	HealthyA      bool
	HealthyB      bool
	SharesFwdA    uint64
	SharesFwdB    uint64
	SwitchCount   uint64
	LastSwitchAt  time.Time // zero value if never switched
}

// Status returns a snapshot of current split-mode diagnostics.
func (r *Router) Status() Status {
	r.mu.Lock()
	active := r.active
	healthyA := r.healthy[LabelA]
	healthyB := r.healthy[LabelB]
	r.mu.Unlock()

	var lastSwitch time.Time
	if ns := r.lastSwitchAt.Load(); ns != 0 {
		lastSwitch = time.Unix(0, ns)
	}

	return Status{
		ActivePool:    active,
		SplitPercentA: r.splitPercentA,
		HealthyA:      healthyA,
		HealthyB:      healthyB,
		SharesFwdA:    r.sharesFwdA.Load(),
		SharesFwdB:    r.sharesFwdB.Load(),
		SwitchCount:   r.switchCount.Load(),
		LastSwitchAt:  lastSwitch,
	}
}

// foldJob computes the virtual (folded/padded) coinbase1/coinbase2 for a job
// from a pool whose real EN1 is realEN1 and whose real EN2 budget for this
// job is realEN2Size bytes. See the package doc for the byte-layout proof.
func foldJob(params *upstream.JobParams, realEN1 string, prefixSize, virtualEN2Size, realEN2Size int) (coinbase1, coinbase2 string) {
	padLen := realEN2Size - prefixSize - virtualEN2Size
	if padLen < 0 {
		padLen = 0 // shouldn't happen — Start() sizes virtualEN2Size from the joint minimum
	}
	coinbase1 = params.Coinbase1 + realEN1
	coinbase2 = strings.Repeat("00", padLen) + params.Coinbase2
	return coinbase1, coinbase2
}

// otherLabel returns the label of the pool that isn't the given one.
func otherLabel(label string) string {
	if label == LabelA {
		return LabelB
	}
	return LabelA
}

// parseVersionMask parses a pool's hex version-rolling mask (e.g.
// "1fffe000") into a uint32, returning 0 (no rolling) on any parse failure —
// mirrors the pattern already used in appcore.go for single-pool proxy mode.
func parseVersionMask(hexMask string) uint32 {
	if hexMask == "" {
		return 0
	}
	b, err := hex.DecodeString(hexMask)
	if err != nil || len(b) != 4 {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}
