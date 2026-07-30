package webhost

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
)

// errorType is used to detect an `error` return value.
var errorType = reflect.TypeOf((*error)(nil)).Elem()

// rpcDenied lists exported App methods that must NOT be reachable over RPC —
// lifecycle hooks and host wiring the frontend never calls directly.
var rpcDenied = map[string]bool{
	"OnStartup":     true,
	"OnDomReady":    true,
	"OnShutdown":    true,
	"OnBeforeClose": true,
	"SetHost":       true,
}

// handleRPC dispatches POST /api/rpc/<Method>. The JSON body is an array of the
// method's arguments (or empty). Return values become the JSON response; a
// trailing error return becomes an HTTP 500 with {"error": "..."}. This mirrors
// how Wails invokes bound methods, so the same frontend calls work unchanged.
func (s *Server) handleRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST only", http.StatusMethodNotAllowed)
		return
	}
	name := strings.TrimPrefix(r.URL.Path, "/api/rpc/")
	if name == "" || strings.Contains(name, "/") || rpcDenied[name] {
		http.Error(w, "unknown method", http.StatusNotFound)
		return
	}
	m := reflect.ValueOf(s.app).MethodByName(name)
	if !m.IsValid() {
		http.Error(w, "unknown method: "+name, http.StatusNotFound)
		return
	}
	mt := m.Type()

	// Decode args (body may be empty for zero-arg methods).
	var raw []json.RawMessage
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&raw)
	}
	if len(raw) != mt.NumIn() {
		http.Error(w, fmt.Sprintf("method %s expects %d args, got %d", name, mt.NumIn(), len(raw)), http.StatusBadRequest)
		return
	}
	in := make([]reflect.Value, mt.NumIn())
	for i := 0; i < mt.NumIn(); i++ {
		p := reflect.New(mt.In(i))
		if err := json.Unmarshal(raw[i], p.Interface()); err != nil {
			http.Error(w, fmt.Sprintf("arg %d: %v", i, err), http.StatusBadRequest)
			return
		}
		in[i] = p.Elem()
	}

	out := m.Call(in)

	// Split off a trailing error return, if any.
	var errStr string
	if n := len(out); n > 0 && out[n-1].Type() == errorType {
		if !out[n-1].IsNil() {
			errStr = out[n-1].Interface().(error).Error()
		}
		out = out[:n-1]
	}

	w.Header().Set("Content-Type", "application/json")
	if errStr != "" {
		w.WriteHeader(http.StatusInternalServerError)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": errStr})
		return
	}

	var result interface{}
	switch len(out) {
	case 0:
		result = nil
	case 1:
		result = out[0].Interface()
	default:
		arr := make([]interface{}, len(out))
		for i := range out {
			arr[i] = out[i].Interface()
		}
		result = arr
	}
	_ = json.NewEncoder(w).Encode(result)
}
