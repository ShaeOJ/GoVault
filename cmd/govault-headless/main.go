// Command govault-headless runs GoVault's full application core (solo + proxy,
// settings, DB stats) without the desktop window, serving the same Svelte UI
// over HTTP. Intended for headless boxes and SBCs (e.g. Orange Pi Zero 2W).
package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"io/fs"

	"govault/internal/appcore"
	"govault/internal/webhost"
	"govault/internal/webui"
)

func main() {
	addr := flag.String("addr", ":8080", "HTTP listen address for the dashboard/API")
	flag.Parse()

	app := appcore.NewApp()

	// Quit path: the frontend Shutdown() (or a signal) stops the server.
	var stopOnce sync.Once
	stop := make(chan struct{})
	quit := func() { stopOnce.Do(func() { close(stop) }) }

	host := webhost.NewSSEHost(quit)
	app.SetHost(host)
	app.OnStartup(context.Background())

	var webFS fs.FS
	if b, ok := webui.FS(); ok {
		webFS = b
		fmt.Println("serving embedded web UI")
	} else {
		fmt.Println("web UI not bundled — API only (build with vite.config.web.ts)")
	}

	srv := webhost.NewServer(app, host, webFS, *addr)
	go func() {
		fmt.Printf("GoVault headless listening on %s\n", *addr)
		if err := srv.Start(); err != nil && err != http.ErrServerClosed {
			fmt.Fprintf(os.Stderr, "server error: %v\n", err)
			quit()
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	select {
	case s := <-sig:
		fmt.Printf("\nreceived %s, shutting down...\n", s)
	case <-stop:
		fmt.Println("shutdown requested, stopping...")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(ctx)
	app.OnShutdown(context.Background())
	fmt.Println("stopped.")
}
