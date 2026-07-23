package main

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type NodeEntry struct {
	Name  string `json:"name"`
	URL   string `json:"url"`
	Token string `json:"token"`
}

type AdminConfig struct {
	AdminPort  int         `json:"adminPort"`
	UIPassword string      `json:"uiPassword,omitempty"`
	Nodes      []NodeEntry `json:"nodes"`
}

func LoadAdminConfig() *AdminConfig {
	cfg := &AdminConfig{AdminPort: 9090}
	path := adminConfigPath()
	data, err := os.ReadFile(path)
	if err != nil {
		saveAdminConfig(path, cfg)
		return cfg
	}
	json.Unmarshal(data, cfg)
	if cfg.AdminPort == 0 {
		cfg.AdminPort = 9090
	}
	return cfg
}

func saveAdminConfig(path string, cfg *AdminConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func adminConfigPath() string {
	exe, err := os.Executable()
	if err != nil {
		return "data/admin.json"
	}
	return filepath.Join(filepath.Dir(exe), "data", "admin.json")
}

func dataDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "data"
	}
	return filepath.Join(filepath.Dir(exe), "data")
}

// GenerateToken creates a random 32-char hex token.
func GenerateToken() string {
	b := make([]byte, 16)
	rand.Read(b)
	return fmt.Sprintf("%x", b)
}
