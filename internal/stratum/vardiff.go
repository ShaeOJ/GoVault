package stratum

import (
	"math"
	"strings"
	"time"

	"govault/internal/config"
)

// VardiffState tracks per-session variable difficulty state.
type VardiffState struct {
	LastRetargetTime time.Time
	SharesInWindow   int
	RetargetCount    int // how many retargets have occurred (for warmup gating)
}

// VardiffManager adjusts difficulty for each miner session.
type VardiffManager struct {
	config *config.VardiffConfig
}

func NewVardiffManager(cfg *config.VardiffConfig) *VardiffManager {
	return &VardiffManager{config: cfg}
}

// RetargetInterval returns the retarget period as a time.Duration.
func (v *VardiffManager) RetargetInterval() time.Duration {
	return time.Duration(v.config.RetargetTimeSec) * time.Second
}

// StartDiffForUA returns an appropriate start difficulty based on the miner's
// user-agent string. Known low-hashrate miners get a lower start difficulty
// so they don't sit idle waiting for vardiff to ramp down.
func (v *VardiffManager) StartDiffForUA(userAgent string) float64 {
	ua := strings.ToLower(userAgent)
	switch {
	case strings.Contains(ua, "nerdminer"):
		return v.config.MinDiff // ~500 H/s, needs absolute minimum
	default:
		return v.StartDiff()
	}
}

// StartDiff returns the initial difficulty for new sessions.
// Falls back to MinDiff if StartDiff is not configured.
func (v *VardiffManager) StartDiff() float64 {
	if v.config.StartDiff > 0 {
		return v.config.StartDiff
	}
	return v.config.MinDiff
}

// NewState creates a new VardiffState for a session.
func (v *VardiffManager) NewState() *VardiffState {
	return &VardiffState{
		LastRetargetTime: time.Now(),
		SharesInWindow:   0,
	}
}

// RecordQualifyingShare increments the count of shares that meet session
// difficulty. Only shares with actualDiff >= sessionDiff should be counted,
// so that miners submitting at ASIC difficulty don't inflate the share rate.
func (v *VardiffManager) RecordQualifyingShare(state *VardiffState) {
	state.SharesInWindow++
}

// CheckRetarget evaluates whether difficulty should be adjusted.
// Call on every accepted share (including sub-target) so that difficulty
// can decrease when no qualifying shares arrive within a retarget window.
// floorDiff is the minimum difficulty (e.g., from mining.suggest_difficulty);
// vardiff will never go below max(MinDiff, floorDiff).
// Returns (newDifficulty, shouldChange).
func (v *VardiffManager) CheckRetarget(state *VardiffState, currentDiff, floorDiff float64) (float64, bool) {
	elapsed := time.Since(state.LastRetargetTime).Seconds()
	if elapsed < 0.001 {
		elapsed = 0.001 // avoid division by zero
	}

	retargetInterval := float64(v.config.RetargetTimeSec)

	// Effective floor: never go below the miner's suggested difficulty
	// (pointless since the miner won't submit more shares at lower diff)
	floor := v.config.MinDiff
	if floorDiff > floor {
		floor = floorDiff
	}

	// Fast ramp-up: if qualifying shares are flooding in way too fast,
	// retarget early instead of waiting for the full window to expire.
	sharesPerSec := float64(state.SharesInWindow) / elapsed
	targetSharesPerSec := 1.0 / float64(v.config.TargetTimeSec)
	floodRatio := sharesPerSec / targetSharesPerSec

	isFlooding := floodRatio > 3 && elapsed >= 5 // at least 5 seconds of data
	normalRetarget := elapsed >= retargetInterval

	if !isFlooding && !normalRetarget {
		return 0, false
	}

	if state.SharesInWindow == 0 {
		// No qualifying shares in window - decrease difficulty
		newDiff := currentDiff / 2
		newDiff = math.Max(newDiff, floor)
		state.LastRetargetTime = time.Now()
		state.RetargetCount++
		return newDiff, newDiff != currentDiff
	}

	// Calculate actual time per qualifying share
	actualTimePerShare := elapsed / float64(state.SharesInWindow)
	targetTime := float64(v.config.TargetTimeSec)

	// Check if within acceptable variance (only for normal retargets)
	if normalRetarget && !isFlooding {
		lowerBound := targetTime * (1 - v.config.VariancePct/100)
		upperBound := targetTime * (1 + v.config.VariancePct/100)

		if actualTimePerShare >= lowerBound && actualTimePerShare <= upperBound {
			// Within acceptable range
			state.LastRetargetTime = time.Now()
			state.SharesInWindow = 0
			state.RetargetCount++
			return 0, false
		}
	}

	// Calculate new difficulty.
	// During warmup (first 3 retargets), allow uncapped ratio and aggressive
	// weighting so high-hashrate miners converge in 1-2 retargets instead of 10+.
	// After warmup, cap ratio to 2x with 50/50 damping to prevent oscillation.
	ratio := targetTime / actualTimePerShare
	warmup := state.RetargetCount < 3
	if warmup {
		// Uncapped ratio — let it jump straight to where it needs to be
		if ratio < 0.25 {
			ratio = 0.25
		}
	} else {
		if ratio > 2 {
			ratio = 2
		}
		if ratio < 0.5 {
			ratio = 0.5
		}
	}
	idealDiff := currentDiff * ratio

	// Damping: warmup uses 25/75 (aggressive), steady-state uses 50/50 (smooth)
	var newDiff float64
	if warmup {
		newDiff = 0.25*currentDiff + 0.75*idealDiff
	} else {
		newDiff = 0.5*currentDiff + 0.5*idealDiff
	}

	// Clamp to bounds
	newDiff = math.Max(newDiff, floor)
	if v.config.MaxDiff > 0 {
		newDiff = math.Min(newDiff, v.config.MaxDiff)
	}

	// Reset window
	state.LastRetargetTime = time.Now()
	state.SharesInWindow = 0
	state.RetargetCount++

	// Only retarget if the change is meaningful (>5%)
	if math.Abs(newDiff-currentDiff)/currentDiff < 0.05 {
		return 0, false
	}

	return newDiff, true
}
