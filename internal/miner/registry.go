package miner

import (
	"sync"
	"time"
)

// MinerInfo tracks a connected miner's state.
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
	LastShareTime  time.Time `json:"lastShareTime"`
	BestDifficulty float64   `json:"bestDifficulty"`
}

// Registry manages connected miners.
type Registry struct {
	miners map[string]*MinerInfo
	mu     sync.RWMutex
}

func NewRegistry() *Registry {
	return &Registry{
		miners: make(map[string]*MinerInfo),
	}
}

func (r *Registry) Register(info MinerInfo) {
	r.mu.Lock()
	r.miners[info.ID] = &info
	r.mu.Unlock()
}

func (r *Registry) Unregister(id string) {
	r.mu.Lock()
	delete(r.miners, id)
	r.mu.Unlock()
}

func (r *Registry) Get(id string) *MinerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if m, ok := r.miners[id]; ok {
		copy := *m
		return &copy
	}
	return nil
}

func (r *Registry) GetAll() []MinerInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]MinerInfo, 0, len(r.miners))
	for _, m := range r.miners {
		result = append(result, *m)
	}
	return result
}

func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.miners)
}

func (r *Registry) RecordShare(id string, difficulty float64, accepted bool) {
	r.mu.Lock()
	defer r.mu.Unlock()

	m, ok := r.miners[id]
	if !ok {
		return
	}

	if accepted {
		m.SharesAccepted++
		if difficulty > m.BestDifficulty {
			m.BestDifficulty = difficulty
		}
	} else {
		m.SharesRejected++
	}
	m.LastShareTime = time.Now()
}

func (r *Registry) UpdateDifficulty(id string, diff float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if m, ok := r.miners[id]; ok {
		m.CurrentDiff = diff
	}
}

func (r *Registry) UpdateHashrate(id string, hashrate float64) {
	r.mu.Lock()
	defer r.mu.Unlock()

	if m, ok := r.miners[id]; ok {
		m.Hashrate = hashrate
	}
}
