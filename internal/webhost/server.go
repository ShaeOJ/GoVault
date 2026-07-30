package webhost

import (
	"context"
	"io/fs"
	"net/http"

	"govault/internal/appcore"
)

// Server exposes an appcore.App over HTTP: the reflective RPC bridge, the SSE
// event stream, and (optionally) the embedded Svelte web bundle.
type Server struct {
	app *appcore.App
	srv *http.Server
}

// NewServer builds the HTTP server. web is the embedded web UI (may be nil until
// the web bundle is wired in — the API still works without it).
func NewServer(app *appcore.App, host *SSEHost, web fs.FS, addr string) *Server {
	s := &Server{app: app}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/rpc/", s.handleRPC)
	mux.HandleFunc("/api/events", host.ServeEvents)

	if web != nil {
		mux.Handle("/", http.FileServer(http.FS(web)))
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/" {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			_, _ = w.Write([]byte("GoVault headless\n\nWeb UI is not bundled in this build.\nAPI: POST /api/rpc/<Method>  ·  SSE: GET /api/events\n"))
		})
	}

	s.srv = &http.Server{Addr: addr, Handler: mux}
	return s
}

// Start blocks serving requests; returns http.ErrServerClosed on graceful stop.
func (s *Server) Start() error { return s.srv.ListenAndServe() }

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error { return s.srv.Shutdown(ctx) }
