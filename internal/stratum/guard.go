package stratum

import (
	"sync"
	"time"

	"govault/internal/logger"
)

// ── BanList ──────────────────────────────────────────────────────────────────
// Thread-safe temporary IP ban list. Bans expire automatically; a background
// goroutine purges stale entries every 5 minutes so the map doesn't grow
// unbounded across long uptimes.

type BanList struct {
	mu      sync.RWMutex
	entries map[string]banEntry
}

type banEntry struct {
	expiry time.Time
	reason string
}

func newBanList() *BanList {
	b := &BanList{entries: make(map[string]banEntry)}
	go b.cleanupLoop()
	return b
}

// Ban adds ip to the ban list for duration. Overwrites any existing entry.
func (b *BanList) Ban(ip, reason string, duration time.Duration, log *logger.Logger) {
	expiry := time.Now().Add(duration)
	b.mu.Lock()
	b.entries[ip] = banEntry{expiry: expiry, reason: reason}
	b.mu.Unlock()
	if log != nil {
		log.Warnf("stratum", "banned %s for %v: %s", ip, duration, reason)
	}
}

// IsBanned returns true if ip is currently banned, along with the expiry time.
// Expired entries are removed lazily on lookup.
func (b *BanList) IsBanned(ip string) (bool, time.Time) {
	b.mu.RLock()
	e, ok := b.entries[ip]
	b.mu.RUnlock()
	if !ok {
		return false, time.Time{}
	}
	if time.Now().After(e.expiry) {
		b.mu.Lock()
		delete(b.entries, ip)
		b.mu.Unlock()
		return false, time.Time{}
	}
	return true, e.expiry
}

func (b *BanList) cleanupLoop() {
	t := time.NewTicker(5 * time.Minute)
	defer t.Stop()
	for range t.C {
		now := time.Now()
		b.mu.Lock()
		for ip, e := range b.entries {
			if now.After(e.expiry) {
				delete(b.entries, ip)
			}
		}
		b.mu.Unlock()
	}
}

// ── msgLimiter ────────────────────────────────────────────────────────────────
// Per-session token-bucket message rate limiter.
// NOT goroutine-safe — only call from the session's Handle() goroutine.

type msgLimiter struct {
	tokens   float64
	capacity float64
	rate     float64 // tokens per second
	lastTime time.Time
}

func newMsgLimiter(perSec, burst float64) *msgLimiter {
	return &msgLimiter{
		tokens:   burst,
		capacity: burst,
		rate:     perSec,
		lastTime: time.Now(),
	}
}

// allow consumes one token and reports whether the message is permitted.
func (m *msgLimiter) allow() bool {
	now := time.Now()
	elapsed := now.Sub(m.lastTime).Seconds()
	m.lastTime = now
	m.tokens += elapsed * m.rate
	if m.tokens > m.capacity {
		m.tokens = m.capacity
	}
	if m.tokens < 1 {
		return false
	}
	m.tokens--
	return true
}

// ── spamGuard ────────────────────────────────────────────────────────────────
// Per-session sliding-window invalid-share detector.
// Tracks the last N share outcomes and fires when the rejection rate exceeds
// the configured threshold.
// NOT goroutine-safe — only call from the session's Handle() goroutine.

type spamGuard struct {
	window    []bool  // ring buffer: true = accepted, false = rejected
	head      int
	filled    int
	size      int
	threshold float64 // rejection fraction that triggers disconnect (e.g. 0.5)
}

func newSpamGuard(windowSize int, threshold float64) *spamGuard {
	if windowSize < 2 {
		windowSize = 2
	}
	return &spamGuard{
		window:    make([]bool, windowSize),
		size:      windowSize,
		threshold: threshold,
	}
}

// record adds a share result and returns true when the ban threshold is exceeded.
// Returns false until the window is fully populated (avoids false positives
// from the first few shares right after a miner connects).
func (g *spamGuard) record(accepted bool) bool {
	g.window[g.head] = accepted
	g.head = (g.head + 1) % g.size
	if g.filled < g.size {
		g.filled++
	}
	if g.filled < g.size {
		return false
	}
	var rejected int
	for _, ok := range g.window {
		if !ok {
			rejected++
		}
	}
	return float64(rejected)/float64(g.size) > g.threshold
}
