package node

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"govault/internal/coin"
)

// DetectResult holds the outcome of a local node auto-detection attempt.
type DetectResult struct {
	Found       bool
	Host        string
	Port        int
	Username    string
	Password    string
	AuthMethod  string // "cookie", "config", "saved", "default"
	NodeVersion string
	Chain       string
	BlockHeight int64
	SyncPercent float64
	Tried       []string // diagnostic: what strategies were attempted and why they failed
}

// Per-coin default data directories and config file names (Windows).
// Resolved at runtime via %APPDATA%.
type coinPaths struct {
	DataDir    string // relative to APPDATA, e.g. "Bitcoin"
	ConfigFile string // e.g. "bitcoin.conf"
}

var coinDataDirs = map[string]coinPaths{
	"btc": {DataDir: "Bitcoin", ConfigFile: "bitcoin.conf"},
	"bch": {DataDir: "Bitcoin Cash", ConfigFile: "bitcoin.conf"},
	"dgb": {DataDir: "DigiByte", ConfigFile: "digibyte.conf"},
	"bc2": {DataDir: "Bitcoin", ConfigFile: "bitcoin.conf"},
	"xec": {DataDir: "Bitcoin ABC", ConfigFile: "bitcoin.conf"},
}

// DetectLocalNode probes 127.0.0.1 on the selected coin's default RPC port,
// trying saved credentials, cookie auth, config-file auth, and default
// credentials in order. Returns the first successful result or {Found: false}
// with diagnostic info about what was tried.
func DetectLocalNode(coinID string, savedHost string, savedPort int, savedUser, savedPass string) *DetectResult {
	coinDef := coin.Get(coinID)
	host := "127.0.0.1"
	port := coinDef.DefaultRPCPort

	var tried []string

	// Strategy 1: Saved credentials (verify existing config still works)
	if savedPass != "" {
		sHost := savedHost
		if sHost == "" {
			sHost = host
		}
		sPort := savedPort
		if sPort == 0 {
			sPort = port
		}
		if result := tryConnect(sHost, sPort, savedUser, savedPass, "saved", coinDef); result != nil {
			return result
		}
		tried = append(tried, fmt.Sprintf("Saved credentials (%s@%s:%d) — auth failed or unreachable", savedUser, sHost, sPort))
	}

	appdata := os.Getenv("APPDATA")
	paths, hasPaths := coinDataDirs[coinID]

	var dataDir string
	if hasPaths && appdata != "" {
		dataDir = filepath.Join(appdata, paths.DataDir)
	}

	// Strategy 2: Cookie auth
	if dataDir != "" {
		cookiePath := filepath.Join(dataDir, ".cookie")
		if user, pass, err := readCookieAuth(dataDir); err == nil {
			if result := tryConnect(host, port, user, pass, "cookie", coinDef); result != nil {
				return result
			}
			tried = append(tried, fmt.Sprintf("Cookie auth (%s) — found file but RPC connection failed", cookiePath))
		} else {
			tried = append(tried, fmt.Sprintf("Cookie auth — %s not found", cookiePath))
		}
	}

	// Strategy 3: Config file auth
	if dataDir != "" && hasPaths {
		confPath := filepath.Join(dataDir, paths.ConfigFile)
		if user, pass, err := parseConfigAuth(confPath); err == nil {
			if result := tryConnect(host, port, user, pass, "config", coinDef); result != nil {
				return result
			}
			tried = append(tried, fmt.Sprintf("Config auth (%s) — found credentials but RPC connection failed", confPath))
		} else {
			tried = append(tried, fmt.Sprintf("Config auth — %s", err))
		}
	}

	// Strategy 4: Default credentials
	if result := tryConnect(host, port, coinDef.DefaultRPCUsername, "", "default", coinDef); result != nil {
		return result
	}
	tried = append(tried, fmt.Sprintf("Default credentials (%s@%s:%d) — failed", coinDef.DefaultRPCUsername, host, port))

	return &DetectResult{Found: false, Tried: tried}
}

// tryConnect attempts an RPC connection and returns a DetectResult on success, nil on failure.
func tryConnect(host string, port int, username, password, authMethod string, coinDef *coin.CoinDef) *DetectResult {
	client := NewQuickClient(host, port, username, password, false)
	if err := client.Ping(); err != nil {
		return nil
	}

	result := &DetectResult{
		Found:      true,
		Host:       host,
		Port:       port,
		Username:   username,
		Password:   password,
		AuthMethod: authMethod,
	}

	if info, err := client.GetBlockchainInfo(); err == nil {
		result.Chain = info.Chain
		result.BlockHeight = info.Blocks
		result.SyncPercent = info.VerificationProgress * 100
	}

	if netInfo, err := client.GetNetworkInfo(); err == nil {
		result.NodeVersion = netInfo.SubVersion
	}

	return result
}

// readCookieAuth reads the .cookie file from a node's data directory.
// Cookie file format: __cookie__:abc123def456...
func readCookieAuth(dataDir string) (username, password string, err error) {
	cookiePath := filepath.Join(dataDir, ".cookie")
	data, err := os.ReadFile(cookiePath)
	if err != nil {
		return "", "", fmt.Errorf("read cookie: %w", err)
	}

	content := strings.TrimSpace(string(data))
	parts := strings.SplitN(content, ":", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid cookie format")
	}

	return parts[0], parts[1], nil
}

// parseConfigAuth reads rpcuser and rpcpassword from a coin's config file.
func parseConfigAuth(configPath string) (username, password string, err error) {
	f, err := os.Open(configPath)
	if err != nil {
		return "", "", fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "rpcuser=") {
			username = strings.TrimPrefix(line, "rpcuser=")
		} else if strings.HasPrefix(line, "rpcpassword=") {
			password = strings.TrimPrefix(line, "rpcpassword=")
		}
	}

	if username == "" || password == "" {
		return "", "", fmt.Errorf("rpcuser/rpcpassword not found in %s", configPath)
	}

	return username, password, nil
}
