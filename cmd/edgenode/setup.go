package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
)

// RunSetup is the interactive first-run wizard invoked by --init.
// It writes data/config.json and data/edge.json next to the binary.
func RunSetup() {
	r := bufio.NewReader(os.Stdin)

	fmt.Println()
	fmt.Println("  ╔══════════════════════════════════════════╗")
	fmt.Println("  ║     GoVault Edge Node — Setup Wizard     ║")
	fmt.Println("  ╚══════════════════════════════════════════╝")
	fmt.Println()
	fmt.Println("  This wizard creates data/config.json and data/edge.json.")
	fmt.Println("  Run once before deploying. Re-run any time to reconfigure.")
	fmt.Println()

	// ── Step 1: Node name ────────────────────────────────────────────────
	nodeName := prompt(r, "Node name (e.g. east, west-van, toronto-01)", "edge")

	// ── Step 2: Select endpoints ─────────────────────────────────────────
	fmt.Println()
	fmt.Println("  Available Firepool endpoints:")
	fmt.Println()
	fmt.Printf("  %-4s  %-6s  %-6s  %-6s  %s\n", "#", "Coin", "Mode", "Port", "Description")
	fmt.Println("  " + strings.Repeat("─", 54))
	for i, p := range FirepoolPools {
		fmt.Printf("  %-4d  %-6s  %-6s  %-6d  %s\n", i+1, p.Coin, string(p.Mode), p.PrimaryPort, p.Description)
	}
	fmt.Println()

	selectedIndices := promptMultiSelect(r, len(FirepoolPools))

	// ── Step 3: Confirm local ports ───────────────────────────────────────
	fmt.Println()
	fmt.Println("  Confirm local stratum ports (miners connect here).")
	fmt.Println("  Defaults match Firepool's ports — change only if there is a conflict.")
	fmt.Println()

	var endpoints []EdgeEndpoint
	for _, idx := range selectedIndices {
		pool := FirepoolPools[idx]
		localPort := promptInt(r, fmt.Sprintf("  %s/%s local port", pool.Coin, string(pool.Mode)), pool.PrimaryPort, 1, 65535)
		endpoints = append(endpoints, EdgeEndpoint{
			Coin:        pool.Coin,
			CoinID:      pool.CoinID,
			Mode:        string(pool.Mode),
			LocalPort:   localPort,
			UpstreamURL: pool.URL(),
			WorkerName:  pool.WorkerName,
			Password:    "x",
			Enabled:     true,
		})
	}

	// ── Step 4: Dashboard port ────────────────────────────────────────────
	fmt.Println()
	apiPort := promptInt(r, "Dashboard port (HTTP UI)", 8080, 1, 65535)

	// ── Step 5: Admin panel beacon (optional) ─────────────────────────────
	fmt.Println()
	fmt.Println("  ─────────────────────────────────────────────────────────")
	fmt.Println("  Admin panel auto-registration (optional)")
	fmt.Println("  If you run the GoVault admin panel, the edge node can")
	fmt.Println("  register itself automatically on startup.")
	fmt.Println("  ─────────────────────────────────────────────────────────")
	fmt.Println()
	beaconURL := prompt(r, "Admin panel URL (e.g. http://firepool.ca:9090, blank to skip)", "")
	selfURL := ""
	if beaconURL != "" {
		selfURL = prompt(r, "This node's public URL (blank to auto-detect)", "")
	}

	// ── Write data/config.json ────────────────────────────────────────────
	dataDir := dataDirectory()
	if err := os.MkdirAll(dataDir, 0755); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: cannot create data dir: %v\n", err)
		os.Exit(1)
	}

	// Write a minimal config.json with sensible defaults.
	// The stratum port is unused at engine level (each pair has its own),
	// but kept for StratumConfig.MaxConn and similar shared settings.
	configPath := filepath.Join(dataDir, "config.json")
	// If config.json already exists, leave it alone (operator may have tuned vardiff).
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		defaultCfg := map[string]interface{}{
			"miningMode": "proxy",
			"stratum": map[string]interface{}{
				"port":           3333,
				"maxConn":        500,
				"maxConnPerIP":   50,
				"authTimeoutSec": 30,
				"autoStart":      true,
			},
			"vardiff": map[string]interface{}{
				"minDiff":         0.001,
				"startDiff":       1000,
				"maxDiff":         0,
				"targetTimeSec":   15,
				"retargetTimeSec": 90,
				"variancePct":     30,
			},
			"app": map[string]interface{}{
				"logLevel":        "info",
				"electricityCost": 0.10,
			},
		}
		if err := writeJSON(configPath, defaultCfg); err != nil {
			fmt.Fprintf(os.Stderr, "ERROR: write config.json: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("  ✓ data/config.json written (defaults)")
	} else {
		fmt.Println("  ✓ data/config.json unchanged (already exists)")
	}

	// ── Write data/edge.json ──────────────────────────────────────────────
	edgePath := filepath.Join(dataDir, "edge.json")
	existingEdge := LoadEdgeConfig() // preserve nodeId if it already exists
	existingEdge.NodeName = nodeName
	existingEdge.APIPort = apiPort
	existingEdge.Endpoints = endpoints
	existingEdge.BeaconURL = beaconURL
	existingEdge.SelfURL = selfURL
	saveEdgeConfig(edgePath, existingEdge)
	fmt.Println("  ✓ data/edge.json written")

	// ── Generate firewall script ──────────────────────────────────────────
	fwPath := filepath.Join(dataDir, "setup-firewall.sh")
	if err := generateFirewallScript(fwPath, endpoints); err != nil {
		fmt.Fprintf(os.Stderr, "  WARNING: could not write firewall script: %v\n", err)
	} else {
		fmt.Println("  ✓ data/setup-firewall.sh written")
	}

	// ── Summary ───────────────────────────────────────────────────────────
	fmt.Println()
	fmt.Println("  ┌─────────────────────────────────────────────────────────┐")
	fmt.Printf("  │  Node name   : %-40s│\n", nodeName)
	fmt.Printf("  │  Dashboard   : %-40s│\n", fmt.Sprintf("http://0.0.0.0:%d", apiPort))
	fmt.Println("  ├─────────────────────────────────────────────────────────┤")
	fmt.Printf("  │  %-6s  %-6s  %-7s  %-30s│\n", "Coin", "Mode", "Port", "Upstream")
	fmt.Println("  │  " + strings.Repeat("─", 53) + "  │")
	for _, ep := range endpoints {
		fmt.Printf("  │  %-6s  %-6s  %-7d  %-30s│\n",
			ep.Coin, ep.Mode, ep.LocalPort, truncate(ep.UpstreamURL, 30))
	}
	fmt.Println("  └─────────────────────────────────────────────────────────┘")

	fmt.Println()
	fmt.Println("  Next steps:")
	for _, ep := range endpoints {
		fmt.Printf("    • Port-forward TCP %d → this machine  (%s/%s)\n", ep.LocalPort, ep.Coin, ep.Mode)
	}
	fmt.Println("    • Point your subdomain DNS → this machine's public IP")
	fmt.Printf("    • Run:  ./gostratum-edge\n")

	// Offer to run the firewall script immediately on Linux.
	if runtime.GOOS == "linux" && fwPath != "" {
		fmt.Println()
		runNow := promptYesNo(r, "Configure UFW firewall now?", true)
		if runNow {
			runFirewallScript(fwPath)
		} else {
			fmt.Println()
			fmt.Println("  Run manually later:")
			fmt.Printf("    sudo bash %s\n", fwPath)
		}
	} else {
		fmt.Println()
		fmt.Println("  To configure the firewall:")
		fmt.Printf("    sudo bash %s\n", fwPath)
	}

	// ── nginx reverse proxy setup ─────────────────────────────────────────
	fmt.Println()
	fmt.Println("  ─────────────────────────────────────────────────────────")
	fmt.Println("  nginx reverse proxy (optional)")
	fmt.Println("  Exposes the dashboard on port 80/443 with a domain name.")
	fmt.Println("  ─────────────────────────────────────────────────────────")
	fmt.Println()
	wantNginx := promptYesNo(r, "Set up nginx reverse proxy for the dashboard?", false)
	if wantNginx {
		domain := prompt(r, "Domain name (e.g. edge.example.com, blank = IP only)", "")

		// Write nginx config
		nginxConfPath := filepath.Join(dataDir, "nginx-edgenode.conf")
		if err := os.WriteFile(nginxConfPath, []byte(nginxConfig(domain, apiPort)), 0644); err != nil {
			fmt.Fprintf(os.Stderr, "  WARNING: could not write nginx config: %v\n", err)
		} else {
			fmt.Printf("  ✓ data/nginx-edgenode.conf written\n")
		}

		if runtime.GOOS == "linux" {
			// Linux: bash script (apt + certbot)
			nginxScriptPath := filepath.Join(dataDir, "setup-nginx.sh")
			script := nginxSetupScriptLinux(domain, nginxConfPath)
			if err := os.WriteFile(nginxScriptPath, []byte(script), 0755); err != nil {
				fmt.Fprintf(os.Stderr, "  WARNING: could not write nginx script: %v\n", err)
			} else {
				fmt.Printf("  ✓ data/setup-nginx.sh written\n")
			}
			fmt.Println()
			runNow := promptYesNo(r, "Install nginx and configure SSL now?", true)
			if runNow {
				runNginxScript(nginxScriptPath)
			} else {
				fmt.Println()
				fmt.Println("  Run manually later:")
				fmt.Printf("    sudo bash %s\n", nginxScriptPath)
			}
		} else {
			// Windows: PowerShell script
			nginxScriptPath := filepath.Join(dataDir, "setup-nginx.ps1")
			script := nginxSetupScriptWindows(domain, nginxConfPath)
			if err := os.WriteFile(nginxScriptPath, []byte(script), 0644); err != nil {
				fmt.Fprintf(os.Stderr, "  WARNING: could not write nginx script: %v\n", err)
			} else {
				fmt.Printf("  ✓ data/setup-nginx.ps1 written\n")
			}
			fmt.Println()
			fmt.Println("  Run in an Administrator PowerShell:")
			fmt.Printf("    Set-ExecutionPolicy Bypass -Scope Process; .\\data\\setup-nginx.ps1\n")
			if domain != "" {
				fmt.Println()
				fmt.Println("  For HTTPS after running the script, install win-acme:")
				fmt.Println("    https://www.win-acme.com/")
				fmt.Printf("    wacs --source manual --host %s --installation nginx\n", domain)
			}
		}
	}

	fmt.Println()
}

// generateFirewallScript writes a UFW setup script for the configured endpoints.
func generateFirewallScript(path string, endpoints []EdgeEndpoint) error {
	var b strings.Builder

	b.WriteString("#!/bin/bash\n")
	b.WriteString("# GoVault Edge Node — UFW firewall setup\n")
	b.WriteString("# Generated by --init wizard. Safe to re-run.\n")
	b.WriteString("set -e\n\n")

	b.WriteString("echo '  Configuring UFW for GoVault Edge Node...'\n\n")

	b.WriteString("# Allow SSH first to prevent lockout\n")
	b.WriteString("ufw allow 22/tcp\n\n")

	b.WriteString("# Stratum ports — miners connect here\n")
	for _, ep := range endpoints {
		b.WriteString(fmt.Sprintf("ufw allow %d/tcp   # %s %s\n", ep.LocalPort, ep.Coin, ep.Mode))
	}

	b.WriteString("\n# Enable UFW (--force skips the interactive prompt)\n")
	b.WriteString("ufw --force enable\n\n")

	b.WriteString("echo\n")
	b.WriteString("echo '  UFW status:'\n")
	b.WriteString("ufw status numbered\n")
	b.WriteString("echo\n")
	b.WriteString("echo '  Firewall configured.'\n")

	return os.WriteFile(path, []byte(b.String()), 0755)
}

// runFirewallScript executes the firewall script with sudo.
func runFirewallScript(path string) {
	fmt.Println()
	fmt.Println("  Running firewall setup (sudo required)...")
	fmt.Println()

	cmd := exec.Command("sudo", "bash", path)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Stdin = os.Stdin

	if err := cmd.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "\n  ERROR: firewall setup failed: %v\n", err)
		fmt.Fprintf(os.Stderr, "  Run manually: sudo bash %s\n\n", path)
	}
}

// promptMultiSelect shows a prompt for comma-separated indices (1-based) or "all".
func promptMultiSelect(r *bufio.Reader, max int) []int {
	for {
		fmt.Printf("  Select endpoints (e.g. 1,3,7 or 'all') [all]: ")
		line, _ := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" || strings.EqualFold(line, "all") {
			result := make([]int, max)
			for i := range result {
				result[i] = i
			}
			return result
		}
		parts := strings.Split(line, ",")
		seen := make(map[int]bool)
		var indices []int
		valid := true
		for _, part := range parts {
			n, err := strconv.Atoi(strings.TrimSpace(part))
			if err != nil || n < 1 || n > max {
				fmt.Printf("  (enter numbers 1–%d separated by commas, or 'all')\n", max)
				valid = false
				break
			}
			if !seen[n-1] {
				seen[n-1] = true
				indices = append(indices, n-1)
			}
		}
		if !valid {
			continue
		}
		if len(indices) == 0 {
			fmt.Println("  (select at least one endpoint)")
			continue
		}
		sort.Ints(indices)
		return indices
	}
}

// ── helpers ──────────────────────────────────────────────────────────────────

func promptYesNo(r *bufio.Reader, label string, defaultYes bool) bool {
	def := "Y/n"
	if !defaultYes {
		def = "y/N"
	}
	fmt.Printf("  %s [%s]: ", label, def)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	if line == "" {
		return defaultYes
	}
	return line == "y" || line == "yes"
}

func prompt(r *bufio.Reader, label, defaultVal string) string {
	fmt.Printf("  %s [%s]: ", label, defaultVal)
	line, _ := r.ReadString('\n')
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal
	}
	return line
}

func promptRequired(r *bufio.Reader, label string) string {
	for {
		fmt.Printf("  %s: ", label)
		line, _ := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line != "" {
			return line
		}
		fmt.Println("  (required — cannot be empty)")
	}
}

func promptInt(r *bufio.Reader, label string, defaultVal, min, max int) int {
	for {
		fmt.Printf("  %s [%d]: ", label, defaultVal)
		line, _ := r.ReadString('\n')
		line = strings.TrimSpace(line)
		if line == "" {
			return defaultVal
		}
		n, err := strconv.Atoi(line)
		if err != nil || n < min || n > max {
			fmt.Printf("  (enter a number between %d and %d)\n", min, max)
			continue
		}
		return n
	}
}

func dataDirectory() string {
	exe, err := os.Executable()
	if err != nil {
		return "data"
	}
	return filepath.Join(filepath.Dir(exe), "data")
}

func writeJSON(path string, v interface{}) error {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, 0644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n-3] + "..."
}
