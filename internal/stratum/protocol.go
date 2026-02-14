package stratum

import (
	"encoding/json"
	"fmt"
)

// Stratum V1 error codes
const (
	ErrOther         = 20
	ErrStaleJob      = 21
	ErrDuplicate     = 22
	ErrLowDifficulty = 23
	ErrUnauthorized  = 24
	ErrNotSubscribed = 25
)

// Request is a JSON-RPC request from a miner.
type Request struct {
	ID     interface{}       `json:"id"`
	Method string            `json:"method"`
	Params []json.RawMessage `json:"params"`
}

// Response is a JSON-RPC response sent to a miner.
type Response struct {
	ID     interface{}   `json:"id"`
	Result interface{}   `json:"result"`
	Error  *StratumError `json:"error"`
}

// Notification is a server-initiated message (id is always null).
type Notification struct {
	ID     interface{} `json:"id"`
	Method string      `json:"method"`
	Params interface{} `json:"params"`
}

// StratumError represents a Stratum protocol error.
type StratumError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *StratumError) Error() string {
	return fmt.Sprintf("stratum error %d: %s", e.Code, e.Message)
}

// ParseRequest parses a raw JSON line into a Request.
func ParseRequest(data []byte) (*Request, error) {
	var req Request
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, fmt.Errorf("invalid JSON-RPC: %w", err)
	}
	if req.Method == "" {
		return nil, fmt.Errorf("missing method")
	}
	return &req, nil
}

// EncodeResponse marshals a response with a trailing newline.
func EncodeResponse(id interface{}, result interface{}, stratumErr *StratumError) []byte {
	resp := Response{
		ID:     id,
		Result: result,
		Error:  stratumErr,
	}
	data, _ := json.Marshal(resp)
	return append(data, '\n')
}

// EncodeNotification marshals a server notification with a trailing newline.
func EncodeNotification(method string, params interface{}) []byte {
	notif := Notification{
		ID:     nil,
		Method: method,
		Params: params,
	}
	data, _ := json.Marshal(notif)
	return append(data, '\n')
}

// NewError creates a StratumError.
func NewError(code int, msg string) *StratumError {
	return &StratumError{Code: code, Message: msg}
}

// ParamString extracts a string parameter from raw params.
func ParamString(params []json.RawMessage, index int) (string, error) {
	if index >= len(params) {
		return "", fmt.Errorf("param index %d out of range (have %d)", index, len(params))
	}
	var s string
	if err := json.Unmarshal(params[index], &s); err != nil {
		return "", fmt.Errorf("param %d not a string: %w", index, err)
	}
	return s, nil
}

// ParamFloat extracts a float64 parameter from raw params.
func ParamFloat(params []json.RawMessage, index int) (float64, error) {
	if index >= len(params) {
		return 0, fmt.Errorf("param index %d out of range", index)
	}
	var f float64
	if err := json.Unmarshal(params[index], &f); err != nil {
		return 0, fmt.Errorf("param %d not a number: %w", index, err)
	}
	return f, nil
}

// ParamJobID extracts a job ID, handling both string ("1") and numeric (1)
// formats. Some miners send job IDs as JSON numbers instead of strings.
func ParamJobID(params []json.RawMessage, index int) (string, error) {
	if index >= len(params) {
		return "", fmt.Errorf("param index %d out of range (have %d)", index, len(params))
	}

	// Try as string first (standard)
	var s string
	if err := json.Unmarshal(params[index], &s); err == nil {
		return s, nil
	}

	// Fallback: numeric job ID — convert to hex to match our "%x" format
	var n float64
	if err := json.Unmarshal(params[index], &n); err == nil {
		return fmt.Sprintf("%x", int64(n)), nil
	}

	return "", fmt.Errorf("param %d: not a valid job ID", index)
}
