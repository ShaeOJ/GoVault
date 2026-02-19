package miner

import (
	"math"
	"sync"
	"time"
)

// HashratePoint is a single data point in a hashrate time series.
type HashratePoint struct {
	Timestamp int64   `json:"t"`
	Hashrate  float64 `json:"h"`
}

// DashboardStats holds the aggregated stats for the frontend dashboard.
type DashboardStats struct {
	TotalHashrate       float64 `json:"totalHashrate"`
	ActiveMiners        int     `json:"activeMiners"`
	SharesAccepted      uint64  `json:"sharesAccepted"`
	SharesRejected      uint64  `json:"sharesRejected"`
	PoolShares          uint64  `json:"poolShares"`
	BestDifficulty      float64 `json:"bestDifficulty"`
	BlocksFound         uint64  `json:"blocksFound"`
	NetworkDifficulty   float64 `json:"networkDifficulty"`
	NetworkHashrate     float64 `json:"networkHashrate"`
	EstTimeToBlock      float64 `json:"estTimeToBlock"`
	BlockChance         float64 `json:"blockChance"`
	StratumRunning      bool    `json:"stratumRunning"`
	BlockHeight         int64   `json:"blockHeight"`

	// Proxy mode fields
	MiningMode          string  `json:"miningMode"`
	UpstreamDiff        float64 `json:"upstreamDiff"`
	ProxySharesFwd      uint64  `json:"proxySharesFwd"`
	ProxySharesAccepted uint64  `json:"proxySharesAccepted"`
	ProxySharesRejected uint64  `json:"proxySharesRejected"`
}

// StatsAggregator collects and aggregates mining statistics.
type StatsAggregator struct {
	hashrateHistory []HashratePoint
	maxHistory      int // max points to keep

	totalAccepted  uint64
	totalRejected  uint64
	poolShares     uint64 // qualifying shares (met session difficulty)
	bestDifficulty float64
	blocksFound    uint64

	// Share tracking for hashrate estimation
	shareRecords []shareRecord
	maxRecords   int

	mu sync.RWMutex
}

type shareRecord struct {
	timestamp  time.Time
	minerID    string
	difficulty float64
}

func NewStatsAggregator() *StatsAggregator {
	return &StatsAggregator{
		hashrateHistory: make([]HashratePoint, 0, 10080), // 7 days at 1-min intervals
		maxHistory:      10080,
		shareRecords:    make([]shareRecord, 0, 10000),
		maxRecords:      10000,
	}
}

// LoadFromDB restores cumulative stats and hashrate history from persisted data.
func (s *StatsAggregator) LoadFromDB(accepted, rejected, blocks uint64, bestDiff float64, history []HashratePoint) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.totalAccepted = accepted
	s.totalRejected = rejected
	s.blocksFound = blocks
	s.bestDifficulty = bestDiff

	if len(history) > 0 {
		if len(history) > s.maxHistory {
			history = history[len(history)-s.maxHistory:]
		}
		s.hashrateHistory = history
	}
}

// GetCumulativeStats returns the current cumulative counters for persistence.
func (s *StatsAggregator) GetCumulativeStats() (accepted, rejected, blocks uint64, bestDiff float64) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.totalAccepted, s.totalRejected, s.blocksFound, s.bestDifficulty
}

// ResetRejected zeros the in-memory rejected share counter.
func (s *StatsAggregator) ResetRejected() {
	s.mu.Lock()
	s.totalRejected = 0
	s.mu.Unlock()
}

// ClearShareRecords wipes the windowed share records used for hashrate
// estimation. Call this when the stratum server stops so that stale records
// (which reference now-dead session IDs) don't pollute hashrate estimates
// after restart.
func (s *StatsAggregator) ClearShareRecords() {
	s.mu.Lock()
	s.shareRecords = s.shareRecords[:0]
	s.mu.Unlock()
}

// RecordShare records a share for statistics.
// difficulty is the session difficulty for qualifying shares (>= pool diff),
// or 0 for sub-target shares. Only qualifying shares contribute to hashrate.
func (s *StatsAggregator) RecordShare(minerID string, difficulty float64, accepted bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if accepted {
		s.totalAccepted++
		if difficulty > 0 {
			s.poolShares++
			s.shareRecords = append(s.shareRecords, shareRecord{
				timestamp:  time.Now(),
				minerID:    minerID,
				difficulty: difficulty,
			})
			if len(s.shareRecords) > s.maxRecords {
				s.shareRecords = s.shareRecords[1:]
			}
		}
	} else {
		s.totalRejected++
	}
}

// RecordBestDifficulty updates the best share difficulty using the actual hash difficulty.
func (s *StatsAggregator) RecordBestDifficulty(actualDiff float64) {
	s.mu.Lock()
	if actualDiff > s.bestDifficulty {
		s.bestDifficulty = actualDiff
	}
	s.mu.Unlock()
}

// RecordBlock records a block found event.
func (s *StatsAggregator) RecordBlock() {
	s.mu.Lock()
	s.blocksFound++
	s.mu.Unlock()
}

// RecordHashrate records a hashrate data point for the time series.
func (s *StatsAggregator) RecordHashrate(hashrate float64) {
	s.mu.Lock()
	defer s.mu.Unlock()

	point := HashratePoint{
		Timestamp: time.Now().Unix(),
		Hashrate:  hashrate,
	}

	s.hashrateHistory = append(s.hashrateHistory, point)
	if len(s.hashrateHistory) > s.maxHistory {
		s.hashrateHistory = s.hashrateHistory[1:]
	}
}

const hashrateWindow = 10 * time.Minute // matches miningcore default

// EstimateHashrate estimates the total hashrate from recent shares.
func (s *StatsAggregator) EstimateHashrate() float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.estimateHashrateAdaptive(hashrateWindow, "")
}

// EstimateMinerHashrate estimates hashrate for a specific miner.
func (s *StatsAggregator) EstimateMinerHashrate(minerID string) float64 {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.estimateHashrateAdaptive(hashrateWindow, minerID)
}

// estimateHashrateAdaptive uses an adaptive window: during ramp-up (when the
// miner has been active less than the full window), it uses the actual time
// since the first share rather than the full window duration. This prevents
// wild readings when the window is mostly empty. If minerID is empty, all
// miners are included.
func (s *StatsAggregator) estimateHashrateAdaptive(maxWindow time.Duration, minerID string) float64 {
	now := time.Now()
	cutoff := now.Add(-maxWindow)

	var totalDiff float64
	var earliest time.Time

	for i := len(s.shareRecords) - 1; i >= 0; i-- {
		r := s.shareRecords[i]
		if r.timestamp.Before(cutoff) {
			break
		}
		if minerID != "" && r.minerID != minerID {
			continue
		}
		totalDiff += r.difficulty
		earliest = r.timestamp
	}

	if totalDiff == 0 {
		return 0
	}

	// Adaptive window: use the time span from first share in window to now,
	// but never less than 30 seconds (avoids huge spikes from a few early shares)
	// and never more than the full window.
	windowSec := now.Sub(earliest).Seconds()
	maxSec := maxWindow.Seconds()
	if windowSec > maxSec {
		windowSec = maxSec
	}
	if windowSec < 30 {
		windowSec = 30
	}

	return totalDiff * math.Pow(2, 32) / windowSec
}

// GetDashboardStats returns aggregate stats for the dashboard.
func (s *StatsAggregator) GetDashboardStats(activeMiners int, networkDiff, networkHashrate float64, blockHeight int64, stratumRunning bool) DashboardStats {
	s.mu.RLock()
	defer s.mu.RUnlock()

	totalHashrate := s.estimateHashrateAdaptive(hashrateWindow, "")
	estTimeToBlock := EstimateTimeToBlock(totalHashrate, networkDiff)

	// P(24h) = (1 - e^(-86400 / estTimeToBlock)) * 100
	var blockChance float64
	if estTimeToBlock > 0 {
		blockChance = (1 - math.Exp(-86400/estTimeToBlock)) * 100
	}

	return DashboardStats{
		TotalHashrate:     totalHashrate,
		ActiveMiners:      activeMiners,
		SharesAccepted:    s.totalAccepted,
		SharesRejected:    s.totalRejected,
		PoolShares:        s.poolShares,
		BestDifficulty:    s.bestDifficulty,
		BlocksFound:       s.blocksFound,
		NetworkDifficulty: networkDiff,
		NetworkHashrate:   networkHashrate,
		EstTimeToBlock:    estTimeToBlock,
		BlockChance:       blockChance,
		StratumRunning:    stratumRunning,
		BlockHeight:       blockHeight,
	}
}

// GetHashrateHistory returns hashrate time series filtered by period.
func (s *StatsAggregator) GetHashrateHistory(period string) []HashratePoint {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var cutoff time.Time
	switch period {
	case "1h":
		cutoff = time.Now().Add(-1 * time.Hour)
	case "6h":
		cutoff = time.Now().Add(-6 * time.Hour)
	case "24h":
		cutoff = time.Now().Add(-24 * time.Hour)
	case "7d":
		cutoff = time.Now().Add(-7 * 24 * time.Hour)
	default:
		cutoff = time.Now().Add(-24 * time.Hour)
	}

	cutoffUnix := cutoff.Unix()
	var result []HashratePoint
	for _, p := range s.hashrateHistory {
		if p.Timestamp >= cutoffUnix {
			result = append(result, p)
		}
	}

	return result
}

// EstimateTimeToBlock calculates expected seconds to find a block.
func EstimateTimeToBlock(hashrate, networkDiff float64) float64 {
	if hashrate <= 0 || networkDiff <= 0 {
		return 0
	}
	// expected_seconds = networkDiff * 2^32 / hashrate
	return networkDiff * math.Pow(2, 32) / hashrate
}
