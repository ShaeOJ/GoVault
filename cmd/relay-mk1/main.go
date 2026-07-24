// Command relay-mk1 is the headless GoVault mining server for the ReTek Inc.
// Relay Unit Mk.I appliance (repurposed Antminer S9 control board) and any other
// headless host. It runs the full GoVault engine — solo (against a coin node's
// RPC) or proxy (to an upstream pool), selected by config.json's miningMode —
// and serves the monitoring dashboard. Unlike cmd/edgenode it has no FirePool
// beacon or hardcoded wallets; it reuses the shared internal/ engine, so the
// desktop app is untouched.
package main

import (
	"embed"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"
)

//go:embed static
var staticFiles embed.FS

func main() {
	apiPort := flag.Int("api-port", 8080, "HTTP dashboard port")
	logLevel := flag.String("log-level", "", "override log level: debug, info, warn, error")
	flag.Parse()

	eng, err := NewEngine(staticFiles, *apiPort, *logLevel)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init error: %v\n", err)
		os.Exit(1)
	}

	if err := eng.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start error: %v\n", err)
		os.Exit(1)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	received := <-sig
	fmt.Printf("\nreceived %s, shutting down...\n", received)

	eng.Stop()
	fmt.Println("relay-mk1 stopped.")
}
