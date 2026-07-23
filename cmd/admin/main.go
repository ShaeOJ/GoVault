package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"
)

//go:embed static
var staticFiles embed.FS

func main() {
	portFlag := flag.Int("port", 0, "override dashboard port (default 9090)")
	addFlag := flag.String("add", "", "register a node: name,http://url:port,token")
	flag.Parse()

	cfg := LoadAdminConfig()
	if *portFlag > 0 {
		cfg.AdminPort = *portFlag
	}

	if *addFlag != "" {
		parts := strings.SplitN(*addFlag, ",", 3)
		if len(parts) < 2 {
			fmt.Fprintln(os.Stderr, "usage: --add name,http://url:port[,token]")
			os.Exit(1)
		}
		token := ""
		if len(parts) == 3 {
			token = parts[2]
		} else {
			token = GenerateToken()
			fmt.Printf("Generated token: %s\n", token)
		}
		cfg.Nodes = append(cfg.Nodes, NodeEntry{
			Name:  parts[0],
			URL:   parts[1],
			Token: token,
		})
		if err := saveAdminConfig(adminConfigPath(), cfg); err != nil {
			fmt.Fprintf(os.Stderr, "save failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Registered node %q at %s\n", parts[0], parts[1])
		fmt.Printf("Set adminToken = %q in the node's data/edge.json\n", token)
		return
	}

	// Generate a UI password on first run.
	if cfg.UIPassword == "" {
		cfg.UIPassword = GenerateToken()
		saveAdminConfig(adminConfigPath(), cfg)
	}

	registry := NewRegistry(cfg.Nodes)
	poller := NewPoller(registry)
	poller.Start()

	srv := NewAdminServer(cfg.AdminPort, registry, poller, cfg, staticFiles)
	if err := srv.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "ERROR: %v\n", err)
		fmt.Fprintln(os.Stderr, "Press Enter to exit...")
		fmt.Scanln()
		os.Exit(1)
	}

	url := fmt.Sprintf("http://localhost:%d", cfg.AdminPort)
	fmt.Println("╔══════════════════════════════════════════╗")
	fmt.Println("║      GoVault Admin Panel  — running      ║")
	fmt.Println("╚══════════════════════════════════════════╝")
	fmt.Printf("  Dashboard : %s\n", url)
	fmt.Printf("  Password  : %s\n", cfg.UIPassword)
	if len(cfg.Nodes) == 0 {
		fmt.Println("  No nodes registered yet. Add one with:")
		fmt.Printf("  gostratum-admin --add \"name,http://ip:8080,firepool-admin-2026\"\n")
	} else {
		fmt.Printf("  Monitoring : %d node(s)\n", len(cfg.Nodes))
	}
	fmt.Println()

	// Auto-open browser after a short delay so the server is ready.
	go func() {
		time.Sleep(500 * time.Millisecond)
		openBrowser(url)
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	fmt.Println("shutting down...")
	poller.Stop()
	srv.Stop()
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("cmd", "/c", "start", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	cmd.Start()
}
