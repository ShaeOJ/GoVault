package api

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"sync"
	"time"

	"govault/internal/config"
	"govault/internal/logger"
	"govault/internal/miner"
)

// PairStatus describes the state of one upstream-pool ↔ local-stratum proxy pair.
type PairStatus struct {
	Coin           string  `json:"coin"`
	CoinID         string  `json:"coinId"`
	Mode           string  `json:"mode"`
	LocalPort      int     `json:"localPort"`
	UpstreamURL    string  `json:"upstreamUrl"`
	Connected      bool    `json:"connected"`
	Authorized     bool    `json:"authorized"`
	Running        bool    `json:"running"`
	UpstreamDiff   float64 `json:"upstreamDiff"`
	MinerCount     int     `json:"minerCount"`
	NetworkDiff    float64 `json:"networkDiff"`
	BlockHeight    int64   `json:"blockHeight"`
	TotalHashrate  float64 `json:"totalHashrate"`
}

// PairEndpoint is one endpoint entry in a SetupRequest.
type PairEndpoint struct {
	Coin        string `json:"coin"`
	CoinID      string `json:"coinId"`
	Mode        string `json:"mode"`
	LocalPort   int    `json:"localPort"`
	UpstreamURL string `json:"upstreamUrl"`
	WorkerName  string `json:"workerName"`
	Password    string `json:"password"`
	Enabled     bool   `json:"enabled"`
}

// SetupRequest is the JSON body for POST /api/setup.
type SetupRequest struct {
	NodeName   string         `json:"nodeName"`
	WorkerName string         `json:"workerName"`
	APIPort    int            `json:"apiPort"`
	Endpoints  []PairEndpoint `json:"endpoints"`
	BeaconURL  string         `json:"beaconUrl,omitempty"`
	SelfURL    string         `json:"selfUrl,omitempty"`
}

// StatsProvider is implemented by the edge node Engine.
// The api package only depends on this interface — no import of cmd/edgenode.
type StatsProvider interface {
	GetDashboardStats() miner.DashboardStats
	GetMiners() []miner.MinerInfo
	GetPairStatus() []PairStatus
	GetConfig() *config.Config
	Uptime() time.Duration
	NodeID() string
	NodeName() string
}

// SettingsRequest is the JSON body for POST /api/admin/settings.
type SettingsRequest struct {
	MaxConn        int    `json:"maxConn,omitempty"`
	MaxConnPerIP   int    `json:"maxConnPerIP,omitempty"`
	AuthTimeoutSec int    `json:"authTimeoutSec,omitempty"`
	LogLevel       string `json:"logLevel,omitempty"`
}

// SettingsResponse is the JSON response for GET/POST /api/admin/settings.
type SettingsResponse struct {
	MaxConn        int    `json:"maxConn"`
	MaxConnPerIP   int    `json:"maxConnPerIP"`
	AuthTimeoutSec int    `json:"authTimeoutSec"`
	LogLevel       string `json:"logLevel"`
}

// Server serves the HTTP dashboard and REST API.
type Server struct {
	port       int
	provider   StatsProvider
	log        *logger.Logger
	staticFS   embed.FS
	srv        *http.Server
	OnSetup    func(req SetupRequest) error
	configured bool

	// OnConfigUpdate applies a full config (solo/proxy) from POST /api/config.
	// Nil on nodes that don't support web config editing (e.g. edgenode).
	OnConfigUpdate func(*config.Config) error

	// Admin management fields (optional — only active when AdminToken is set).
	AdminToken     string
	OnSettings     func(req SettingsRequest) error
	OnRestart      func()
	OnNginxSetup   func(domain string) (string, error)
}

// NewServer creates the HTTP server. staticFS must contain a "static" subdirectory
// with at least index.html.
func NewServer(port int, provider StatsProvider, staticFS embed.FS, log *logger.Logger) *Server {
	return &Server{
		port:     port,
		provider: provider,
		log:      log,
		staticFS: staticFS,
	}
}

// SetConfigured marks the server as fully configured (proxy pairs running).
func (s *Server) SetConfigured(v bool) {
	s.configured = v
}

// ipRateLimiter is a simple per-IP token-bucket rate limiter.
// It allows up to burst requests immediately, then refills at rate per second.
type ipRateLimiter struct {
	mu     sync.Mutex
	counts map[string]*rateBucket
}

type rateBucket struct {
	tokens   float64
	lastSeen time.Time
}

func newIPRateLimiter() *ipRateLimiter {
	rl := &ipRateLimiter{counts: make(map[string]*rateBucket)}
	// Periodically evict idle entries to prevent unbounded map growth.
	go func() {
		for range time.Tick(5 * time.Minute) {
			rl.mu.Lock()
			cutoff := time.Now().Add(-10 * time.Minute)
			for ip, b := range rl.counts {
				if b.lastSeen.Before(cutoff) {
					delete(rl.counts, ip)
				}
			}
			rl.mu.Unlock()
		}
	}()
	return rl
}

// allow returns true if the request from ip should be allowed.
// rate = max sustained requests/sec, burst = initial burst capacity.
func (rl *ipRateLimiter) allow(ip string, rate, burst float64) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	b, ok := rl.counts[ip]
	if !ok {
		b = &rateBucket{tokens: burst}
		rl.counts[ip] = b
	}
	elapsed := now.Sub(b.lastSeen).Seconds()
	b.lastSeen = now
	b.tokens += elapsed * rate
	if b.tokens > burst {
		b.tokens = burst
	}
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// rateLimitMiddleware wraps a handler with per-IP rate limiting.
// API endpoints: 30 req/s burst, 10 req/s sustained.
// Admin endpoints: 5 req/s burst, 2 req/s sustained.
func rateLimitMiddleware(rl *ipRateLimiter, rate, burst float64, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip, _, _ := net.SplitHostPort(r.RemoteAddr)
		if ip == "" {
			ip = r.RemoteAddr
		}
		if !rl.allow(ip, rate, burst) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.WriteHeader(http.StatusTooManyRequests)
			w.Write([]byte(`{"error":"rate limit exceeded"}`))
			return
		}
		next(w, r)
	}
}

// Start binds the port and begins serving in a background goroutine.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	rl := newIPRateLimiter()

	h := &handlers{provider: s.provider, server: s}

	// API endpoints: 30 req burst, 10 req/s sustained per IP.
	api := func(fn http.HandlerFunc) http.HandlerFunc { return rateLimitMiddleware(rl, 10, 30, fn) }
	// Admin endpoints: 5 req burst, 2 req/s sustained per IP.
	adm := func(fn http.HandlerFunc) http.HandlerFunc { return rateLimitMiddleware(rl, 2, 5, fn) }

	mux.HandleFunc("/health", api(h.health))
	mux.HandleFunc("/api/stats", api(h.stats))
	mux.HandleFunc("/api/miners", api(h.miners))
	mux.HandleFunc("/api/pairs", api(h.pairs))
	mux.HandleFunc("/api/config", api(h.configRouter))
	mux.HandleFunc("/api/coins", api(h.coins))
	mux.HandleFunc("/api/test-node", api(h.testNode))
	mux.HandleFunc("/api/test-upstream", api(h.testUpstream))
	mux.HandleFunc("/api/setup", api(h.setup))
	mux.HandleFunc("/api/configured", api(h.configured))
	mux.HandleFunc("/api/network", api(h.network))
	mux.HandleFunc("/api/admin/settings", adm(h.adminSettings))
	mux.HandleFunc("/api/admin/restart", adm(h.adminRestart))
	mux.HandleFunc("/api/admin/nginx", adm(h.adminNginx))

	// Serve the embedded dashboard from the "static" sub-tree.
	staticSub, err := fs.Sub(s.staticFS, "static")
	if err != nil {
		return fmt.Errorf("static FS: %w", err)
	}
	mux.Handle("/", http.FileServer(http.FS(staticSub)))

	s.srv = &http.Server{
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", s.port))
	if err != nil {
		return fmt.Errorf("listen :%d: %w", s.port, err)
	}

	go func() {
		if err := s.srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			if s.log != nil {
				s.log.Errorf("api", "HTTP server error: %v", err)
			}
		}
	}()

	return nil
}

// Stop shuts down the HTTP server gracefully with a 5s deadline.
func (s *Server) Stop() {
	if s.srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.srv.Shutdown(ctx)
}
