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

	path string
	mu   sync.RWMutex
}

type NodeConfig struct {
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	UseSSL   bool   `json:"useSSL"`
}

type StratumConfig struct {
	Port      int  `json:"port"`
	MaxConn   int  `json:"maxConn"`
	AutoStart bool `json:"autoStart"`
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
	Theme    string `json:"theme"`
	LogLevel string `json:"logLevel"`
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

func Load() (*Config, error) {
	dir, err := configDir()
	if err != nil {
		return nil, fmt.Errorf("config dir: %w", err)
	}

	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("create config dir: %w", err)
	}

	path := filepath.Join(dir, "config.json")
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
	c.mu.Unlock()
	return c.Save()
}

func (c *Config) Validate() error {
	if c.Stratum.Port < 1 || c.Stratum.Port > 65535 {
		return fmt.Errorf("invalid stratum port: %d", c.Stratum.Port)
	}
	if c.Node.Port < 1 || c.Node.Port > 65535 {
		return fmt.Errorf("invalid node port: %d", c.Node.Port)
	}
	if c.Mining.PayoutAddress != "" {
		coinDef := coin.Get(c.Mining.Coin)
		if valid, _ := coin.ValidateAddress(coinDef, c.Mining.PayoutAddress); !valid {
			return fmt.Errorf("invalid %s address format: %s", coinDef.Name, c.Mining.PayoutAddress)
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

func (c *Config) LogDir() string {
	return filepath.Join(filepath.Dir(c.path), "logs")
}

func (c *Config) DBPath() string {
	return filepath.Join(filepath.Dir(c.path), "govault.db")
}
