package main

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// defaultAdminTokenValue is pre-filled in the Add Node form.
// Must match defaultAdminToken in cmd/edgenode/edgeconfig.go.
const defaultAdminTokenValue = "firepool-admin-2026"

type AdminServer struct {
	port     int
	registry *Registry
	poller   *Poller
	cfg      *AdminConfig
	client   *http.Client
	srv      *http.Server
	staticFS embed.FS

	sessionMu sync.Mutex
	sessions  map[string]time.Time // token → expiry
}

func NewAdminServer(port int, registry *Registry, poller *Poller, cfg *AdminConfig, staticFS embed.FS) *AdminServer {
	return &AdminServer{
		port:     port,
		registry: registry,
		poller:   poller,
		cfg:      cfg,
		client:   &http.Client{Timeout: 5 * time.Second},
		staticFS: staticFS,
		sessions: make(map[string]time.Time),
	}
}

const sessionCookieName = "fp_session"
const sessionTTL = 12 * time.Hour

func (s *AdminServer) createSession() string {
	tok := GenerateToken()
	s.sessionMu.Lock()
	s.sessions[tok] = time.Now().Add(sessionTTL)
	s.sessionMu.Unlock()
	return tok
}

func (s *AdminServer) validSession(r *http.Request) bool {
	if s.cfg.UIPassword == "" {
		return true
	}
	cookie, err := r.Cookie(sessionCookieName)
	if err != nil {
		return false
	}
	s.sessionMu.Lock()
	exp, ok := s.sessions[cookie.Value]
	s.sessionMu.Unlock()
	return ok && time.Now().Before(exp)
}

func (s *AdminServer) requireAuth(w http.ResponseWriter, r *http.Request) bool {
	if s.validSession(r) {
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/api/") {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		json.NewEncoder(w).Encode(map[string]string{"error": "unauthorized"})
		return false
	}
	http.Redirect(w, r, "/login", http.StatusFound)
	return false
}

func (s *AdminServer) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		r.ParseForm()
		if r.FormValue("password") == s.cfg.UIPassword {
			tok := s.createSession()
			http.SetCookie(w, &http.Cookie{
				Name:     sessionCookieName,
				Value:    tok,
				Path:     "/",
				HttpOnly: true,
				MaxAge:   int(sessionTTL.Seconds()),
			})
			http.Redirect(w, r, "/", http.StatusFound)
			return
		}
		s.serveLoginPage(w, "Invalid password")
		return
	}
	if s.validSession(r) {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	s.serveLoginPage(w, "")
}

func (s *AdminServer) handleLogout(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie(sessionCookieName); err == nil {
		s.sessionMu.Lock()
		delete(s.sessions, cookie.Value)
		s.sessionMu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *AdminServer) serveLoginPage(w http.ResponseWriter, errMsg string) {
	errHTML := ""
	if errMsg != "" {
		errHTML = `<p style="color:#ff4455;margin-bottom:16px;">` + errMsg + `</p>`
	}
	w.Header().Set("Content-Type", "text/html")
	fmt.Fprintf(w, `<!DOCTYPE html><html lang="en"><head>
<meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>Firepool Admin — Login</title>
<style>
:root{--bg:#080c14;--surface:#0c1422;--border:rgba(0,160,255,0.15);--accent:#ffd700;--text:#c8d8f0;--font:'Courier New',monospace}
*{box-sizing:border-box;margin:0;padding:0}
body{background:var(--bg);color:var(--text);font-family:var(--font);font-size:13px;min-height:100vh;display:flex;align-items:center;justify-content:center}
.card{background:var(--surface);border:1px solid var(--border);padding:40px;width:340px}
h1{color:var(--accent);font-size:16px;letter-spacing:2px;text-transform:uppercase;margin-bottom:32px;text-align:center}
label{display:block;margin-bottom:6px;color:rgba(200,216,240,0.6);font-size:11px;letter-spacing:1px;text-transform:uppercase}
input{width:100%%;background:#0a1020;border:1px solid var(--border);color:var(--text);font-family:var(--font);font-size:13px;padding:10px 12px;margin-bottom:20px;outline:none}
input:focus{border-color:rgba(0,160,255,0.4)}
button{width:100%%;background:var(--accent);color:#080c14;font-family:var(--font);font-size:13px;font-weight:bold;letter-spacing:1px;padding:11px;border:none;cursor:pointer}
button:hover{opacity:0.9}
</style></head><body>
<div class="card">
<h1>Firepool Admin</h1>
%s
<form method="POST" action="/login">
<label>Password</label>
<input type="password" name="password" autofocus>
<button type="submit">Sign In</button>
</form>
</div>
</body></html>`, errHTML)
}

func (s *AdminServer) Start() error {
	mux := http.NewServeMux()

	mux.HandleFunc("/login", s.handleLogin)
	mux.HandleFunc("/logout", s.handleLogout)

	// POST /api/nodes is the beacon endpoint — no auth required so edge nodes can self-register.
	// All other routes require a valid session.
	mux.HandleFunc("/api/nodes", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			s.addNode(w, r)
			return
		}
		if !s.requireAuth(w, r) {
			return
		}
		s.handleNodes(w, r)
	})
	mux.HandleFunc("/api/nodes/", func(w http.ResponseWriter, r *http.Request) {
		if !s.requireAuth(w, r) {
			return
		}
		s.handleNode(w, r)
	})

	staticSub, err := fs.Sub(s.staticFS, "static")
	if err != nil {
		return fmt.Errorf("static FS: %w", err)
	}
	fileServer := http.FileServer(http.FS(staticSub))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if !s.requireAuth(w, r) {
			return
		}
		fileServer.ServeHTTP(w, r)
	})

	s.srv = &http.Server{
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	ln, err := net.Listen("tcp", fmt.Sprintf("0.0.0.0:%d", s.port))
	if err != nil {
		return fmt.Errorf("listen :%d: %w", s.port, err)
	}
	go func() { s.srv.Serve(ln) }()
	return nil
}

func (s *AdminServer) Stop() {
	if s.srv == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	s.srv.Shutdown(ctx)
}

// handleNodes: GET /api/nodes — fleet overview; POST /api/nodes — add node
func (s *AdminServer) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodPost {
		s.addNode(w, r)
		return
	}
	states := s.registry.GetAll()
	jsonOK(w, states)
}

// handleNode: /api/nodes/{name}[/settings|/restart]
func (s *AdminServer) handleNode(w http.ResponseWriter, r *http.Request) {
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/api/nodes/"), "/")
	name := parts[0]
	action := ""
	if len(parts) > 1 {
		action = parts[1]
	}

	state := s.registry.GetByName(name)
	if state == nil {
		http.NotFound(w, r)
		return
	}

	switch action {
	case "":
		if r.Method == http.MethodDelete {
			s.removeNode(w, r, name)
			return
		}
		// GET detail: fetch pairs + miners on-demand
		pairs, miners, err := s.poller.FetchDetail(state.Entry)
		if err != nil {
			jsonError(w, err.Error())
			return
		}
		state.Pairs = pairs
		state.Miners = miners
		jsonOK(w, state)

	case "settings":
		s.proxyToNode(w, r, state.Entry, "/api/admin/settings")

	case "restart":
		s.proxyToNode(w, r, state.Entry, "/api/admin/restart")

	case "nginx":
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		// apt install + certbot can take 1-2 minutes — extend both the server write
		// deadline and use a dedicated long-timeout HTTP client for the upstream call.
		rc := http.NewResponseController(w)
		rc.SetWriteDeadline(time.Now().Add(5 * time.Minute))
		slowClient := &http.Client{Timeout: 5 * time.Minute}
		s.proxyToNodeWithClient(w, r, state.Entry, "/api/admin/nginx", slowClient)

	default:
		http.NotFound(w, r)
	}
}

func (s *AdminServer) removeNode(w http.ResponseWriter, r *http.Request, name string) {
	newNodes := s.cfg.Nodes[:0]
	found := false
	for _, n := range s.cfg.Nodes {
		if n.Name == name {
			found = true
		} else {
			newNodes = append(newNodes, n)
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	s.cfg.Nodes = newNodes
	if err := saveAdminConfig(adminConfigPath(), s.cfg); err != nil {
		jsonError(w, "save failed: "+err.Error())
		return
	}
	s.registry.Remove(name)
	jsonOK(w, map[string]bool{"ok": true})
}

// proxyToNode forwards the request to an edge node with its auth token.
func (s *AdminServer) proxyToNode(w http.ResponseWriter, r *http.Request, entry NodeEntry, path string) {
	s.proxyToNodeWithClient(w, r, entry, path, s.client)
}

// proxyToNodeWithClient is like proxyToNode but uses a caller-supplied HTTP client.
// Use this for long-running operations (e.g. nginx setup) where the default 5s client timeout is too short.
func (s *AdminServer) proxyToNodeWithClient(w http.ResponseWriter, r *http.Request, entry NodeEntry, path string, client *http.Client) {
	var body []byte
	if r.Body != nil {
		body, _ = io.ReadAll(r.Body)
	}
	method := r.Method
	if method == "" {
		method = http.MethodGet
	}
	req, err := http.NewRequest(method, entry.URL+path, bytes.NewReader(body))
	if err != nil {
		jsonError(w, err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Admin-Token", entry.Token)

	resp, err := client.Do(req)
	if err != nil {
		jsonError(w, "node unreachable: "+err.Error())
		return
	}
	defer resp.Body.Close()
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

func (s *AdminServer) addNode(w http.ResponseWriter, r *http.Request) {
	var entry NodeEntry
	if err := json.NewDecoder(r.Body).Decode(&entry); err != nil {
		jsonError(w, "invalid JSON")
		return
	}
	if entry.Name == "" || entry.URL == "" {
		jsonError(w, "name and url are required")
		return
	}
	// Reject duplicate names.
	for _, n := range s.cfg.Nodes {
		if n.Name == entry.Name {
			jsonError(w, "a node named \""+entry.Name+"\" already exists")
			return
		}
	}
	if entry.Token == "" {
		entry.Token = defaultAdminTokenValue
	}
	s.cfg.Nodes = append(s.cfg.Nodes, entry)
	if err := saveAdminConfig(adminConfigPath(), s.cfg); err != nil {
		jsonError(w, "save failed: "+err.Error())
		return
	}
	s.registry.Add(entry)
	s.poller.AddNode(entry)
	jsonOK(w, map[string]interface{}{"ok": true, "token": entry.Token})
}

func jsonOK(w http.ResponseWriter, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	json.NewEncoder(w).Encode(v)
}

func jsonError(w http.ResponseWriter, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}
