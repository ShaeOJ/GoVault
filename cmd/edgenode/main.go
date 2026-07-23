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
	initFlag   := flag.Bool("init", false, "run the interactive setup wizard and write config files")
	apiPort    := flag.Int("api-port", 0, "HTTP dashboard port (overrides edge.json, default 8080)")
	logLevel   := flag.String("log-level", "", "override log level: debug, info, warn, error")
	adminToken := flag.String("admin-token", "", "admin token for remote management (overrides edge.json)")
	flag.Parse()

	if *initFlag {
		RunSetup()
		return
	}

	eng, err := NewEngine(staticFiles, *apiPort, *logLevel, *adminToken)
	if err != nil {
		fmt.Fprintf(os.Stderr, "init error: %v\n", err)
		os.Exit(1)
	}

	if err := eng.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "start error: %v\n\nRun with --init to configure the node.\n", err)
		os.Exit(1)
	}

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	received := <-sig
	fmt.Printf("\nreceived %s, shutting down...\n", received)

	eng.Stop()
	fmt.Println("edge node stopped.")
}
