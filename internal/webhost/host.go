// Package webhost serves appcore.App over HTTP for the headless edge node: a
// reflective JSON-RPC bridge that mirrors the Wails method calls, plus an SSE
// stream that replaces Wails runtime events. The same Svelte frontend drives
// both transports.
package webhost

import (
	"encoding/json"
	"net/http"
	"sync"
)

// SSEHost implements appcore.AppHost for a headless server. Frontend events are
// fanned out to connected SSE clients; window controls are no-ops; Quit triggers
// the supplied callback (used to shut the process down).
type SSEHost struct {
	mu      sync.Mutex
	clients map[chan sseMessage]struct{}
	onQuit  func()
}

type sseMessage struct {
	event string
	data  []byte
}

// NewSSEHost creates a host. onQuit is invoked when the frontend calls Shutdown().
func NewSSEHost(onQuit func()) *SSEHost {
	return &SSEHost{clients: make(map[chan sseMessage]struct{}), onQuit: onQuit}
}

// Emit broadcasts an event to all SSE subscribers. Matches Wails' EventsEmit
// shape: a single data arg is sent as-is, multiple as an array, none as null.
func (h *SSEHost) Emit(event string, data ...interface{}) {
	var payload interface{}
	switch len(data) {
	case 0:
		payload = nil
	case 1:
		payload = data[0]
	default:
		payload = data
	}
	b, err := json.Marshal(payload)
	if err != nil {
		b = []byte("null")
	}
	msg := sseMessage{event: event, data: b}
	h.mu.Lock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default: // drop for slow clients rather than block the caller
		}
	}
	h.mu.Unlock()
}

func (h *SSEHost) Minimise()   {} // no window when headless
func (h *SSEHost) Show()       {}
func (h *SSEHost) Unminimise() {}

// Quit signals the process to shut down.
func (h *SSEHost) Quit() {
	if h.onQuit != nil {
		h.onQuit()
	}
}

func (h *SSEHost) add() chan sseMessage {
	ch := make(chan sseMessage, 64)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()
	return ch
}

func (h *SSEHost) remove(ch chan sseMessage) {
	h.mu.Lock()
	if _, ok := h.clients[ch]; ok {
		delete(h.clients, ch)
		close(ch)
	}
	h.mu.Unlock()
}

// ServeEvents is the GET /api/events SSE handler. The frontend shim subscribes
// here and dispatches by event name, replacing Wails' EventsOn.
func (h *SSEHost) ServeEvents(w http.ResponseWriter, r *http.Request) {
	fl, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	ch := h.add()
	defer h.remove(ch)

	// Open the stream so the client's onopen fires.
	_, _ = w.Write([]byte(": connected\n\n"))
	fl.Flush()

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			_, _ = w.Write([]byte("event: " + msg.event + "\n"))
			_, _ = w.Write([]byte("data: "))
			_, _ = w.Write(msg.data)
			_, _ = w.Write([]byte("\n\n"))
			fl.Flush()
		}
	}
}
