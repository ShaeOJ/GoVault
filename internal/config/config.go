package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"govault/internal/coin"
)

type Config struct {
	Node    NodeConfig    `json:"node"`
	Stratum StratumConfig `json:"stratum"`
	Mining  MiningConfig  `json:"mining"`
	Vardiff VardiffConfig `json:"vardiff"`
	App     AppConfig     `json:"app"`
	Proxy   ProxyConfig   `json:"proxy"`

	// MiningMode selects "solo" (local node) or "proxy" (upstream pool).
	MiningMode string `json:"miningMode"`

	path string
	mu   sync.RWMutex
}

type ProxyConfig struct {
	URL        string `json:"url"`
	WorkerName string `json:"workerName"`
	Password   string `json:"password"`
	// PassThrough, when true, gives each connected miner its OWN upstream
	// connection authorized with the miner's own worker name (so the pool sees
	// every worker separately and vardiffs each device individually). When false
	// (default) the relay runs shared mode: one upstream connection under
	// WorkerName, all miners aggregated as a single worker on the pool.
	PassThrough bool `json:"passThrough,omitempty"`
}

type NodeConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	UseSSL   bool   `json:"useSSL"`
}

type StratumConfig struct {
	Port           int  `json:"port"`
	MaxConn        int  `json:"maxConn"`
	MaxConnPerIP   int  `json:"maxConnPerIP"`
	AuthTimeoutSec int  `json:"authTimeoutSec"`
	AutoStart      bool `json:"autoStart"`

	// Security / anti-spam
	BanDurationMinutes   int     `json:"banDurationMinutes"`   // 0 = banning disabled
	InvalidShareThreshold float64 `json:"invalidShareThreshold"` // rejection fraction 0–1, 0 = disabled
	InvalidShareWindow   int     `json:"invalidShareWindow"`   // sliding window size (shares)
	MaxMsgPerSec         float64 `json:"maxMsgPerSec"`         // per-connection msg rate, 0 = disabled
	MaxMsgBurst          int     `json:"maxMsgBurst"`          // burst capacity for rate limiter
	MaxLineSizeKB        int     `json:"maxLineSizeKB"`        // max JSON line length, 0 = disabled
	NTimeMaxDriftSec     int     `json:"nTimeMaxDriftSec"`     // max ntime drift vs now, 0 = disabled
}

type MiningConfig struct {
	Coin          string `json:"coin"`
	PayoutAddress string `json:"payoutAddress"`
	CoinbaseTag   string `json:"coinbaseTag"`
}

type VardiffConfig struct {
	MinDiff         float64 `json:"minDiff"`
	StartDiff       float64 `json:"startDiff"`
	MaxDiff         float64 `json:"maxDiff"`
	TargetTimeSec   int     `json:"targetTimeSec"`
	RetargetTimeSec int     `json:"retargetTimeSec"`
	VariancePct     float64 `json:"variancePct"`
}

type AppConfig struct {
	Theme           string  `json:"theme"`
	LogLevel        string  `json:"logLevel"`
	ElectricityCost float64 `json:"electricityCost"`
}

func configDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("resolve executable path: %w", err)
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		return "", fmt.Errorf("resolve symlinks: %w", err)
	}
	return filepath.Join(filepath.Dir(exe), "data"), nil
}

// configFilePath returns the config.json path. GOVAULT_CONFIG_FILE overrides it
// (default: <exe>/data/config.json) — appliance builds point it at a persistent,
// writable partition so dashboard edits survive reboots. Default behaviour is
// unchanged when the env var is unset.
func configFilePath() (string, error) {
	if p := os.Getenv("GOVAULT_CONFIG_FILE"); p != "" {
		return p, nil
	}
	dir, err := configDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "config.json"), nil
}

func Load() (*Config, error) {
	path, err := configFilePath()
	if err != nil {
		return nil, fmt.Errorf("config path: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	cfg := Defaults()
	cfg.path = path

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			if saveErr := cfg.Save(); saveErr != nil {
				return nil, fmt.Errorf("save default config: %w", saveErr)
			}
			return cfg, nil
		}
		return nil, fmt.Errorf("read config: %w", err)
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}

	// Backward compat: empty mining mode defaults to solo
	if cfg.MiningMode == "" {
		cfg.MiningMode = "solo"
	}

	return cfg, nil
}

func (c *Config) Save() error {
	c.mu.RLock()
	defer c.mu.RUnlock()

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}

	tmpPath := c.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, 0644); err != nil {
		return fmt.Errorf("write config tmp: %w", err)
	}

	if err := os.Rename(tmpPath, c.path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename config: %w", err)
	}

	return nil
}

func (c *Config) Update(newCfg *Config) error {
	c.mu.Lock()
	c.Node = newCfg.Node
	c.Stratum = newCfg.Stratum
	c.Mining = newCfg.Mining
	c.Vardiff = newCfg.Vardiff
	c.App = newCfg.App
	c.Proxy = newCfg.Proxy
	c.MiningMode = newCfg.MiningMode
	c.mu.Unlock()
	return c.Save()
}

func (c *Config) Validate() error {
	// Normalize empty mining mode to "solo" for backward compatibility
	if c.MiningMode == "" {
		c.MiningMode = "solo"
	}

	if c.Stratum.Port < 1 || c.Stratum.Port > 65535 {
		return fmt.Errorf("invalid stratum port: %d", c.Stratum.Port)
	}

	if c.MiningMode == "proxy" {
		if c.Proxy.URL == "" {
			return fmt.Errorf("proxy mode requires a pool URL")
		}
		// In pass-through mode each miner authorizes with its own worker name, so
		// a top-level worker isn't required. Shared mode needs one.
		if c.Proxy.WorkerName == "" && !c.Proxy.PassThrough {
			return fmt.Errorf("proxy mode requires a worker name")
		}
	} else {
		if c.Node.Port < 1 || c.Node.Port > 65535 {
			return fmt.Errorf("invalid node port: %d", c.Node.Port)
		}
		if c.Mining.PayoutAddress != "" {
			coinDef := coin.Get(c.Mining.Coin)
			if valid, _ := coin.ValidateAddress(coinDef, c.Mining.PayoutAddress); !valid {
				return fmt.Errorf("invalid %s address format: %s", coinDef.Name, c.Mining.PayoutAddress)
			}
		}
	}

	if c.Vardiff.MinDiff <= 0 {
		return fmt.Errorf("vardiff min_diff must be positive")
	}
	if c.Vardiff.TargetTimeSec < 1 {
		return fmt.Errorf("vardiff target_time must be at least 1 second")
	}
	return nil
}

func (c *Config) GetPath() string {
	return c.path
}

// dataDir is where the DB and logs live. GOVAULT_DATA_DIR overrides it (default:
// the config.json directory) — appliance builds point it at tmpfs so the SQLite
// DB and logs stay off the (vfat, wear-sensitive) config partition. Default
// behaviour is unchanged when the env var is unset.
func (c *Config) dataDir() string {
	if d := os.Getenv("GOVAULT_DATA_DIR"); d != "" {
		return d
	}
	return filepath.Dir(c.path)
}

func (c *Config) LogDir() string {
	return filepath.Join(c.dataDir(), "logs")
}

func (c *Config) DBPath() string {
	return filepath.Join(c.dataDir(), "govault.db")
}
