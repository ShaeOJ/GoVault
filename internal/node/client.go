package node

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"sync/atomic"
	"time"
)

type rpcRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      int64           `json:"id"`
	Result  json.RawMessage `json:"result"`
	Error   *rpcError       `json:"error"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *rpcError) Error() string {
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

type Client struct {
	url        string
	username   string
	password   string
	client     *http.Client
	nextID     atomic.Int64
	maxRetries int

	connected atomic.Bool
	mu        sync.RWMutex
}

func newClient(host string, port int, username, password string, useSSL bool, timeout time.Duration, maxRetries int) *Client {
	scheme := "http"
	if useSSL {
		scheme = "https"
	}

	transport := &http.Transport{
		MaxIdleConns:        10,
		MaxIdleConnsPerHost: 10,
		IdleConnTimeout:     90 * time.Second,
	}
	if useSSL {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	return &Client{
		url:      fmt.Sprintf("%s://%s:%d", scheme, host, port),
		username: username,
		password: password,
		client: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		maxRetries: maxRetries,
	}
}

func NewClient(host string, port int, username, password string, useSSL bool) *Client {
	return newClient(host, port, username, password, useSSL, 30*time.Second, 3)
}

// NewQuickClient creates a client with shorter timeout and single retry,
// suitable for interactive connection tests.
func NewQuickClient(host string, port int, username, password string, useSSL bool) *Client {
	return newClient(host, port, username, password, useSSL, 8*time.Second, 1)
}

func (c *Client) call(method string, params interface{}) (json.RawMessage, error) {
	id := c.nextID.Add(1)

	reqBody := rpcRequest{
		JSONRPC: "1.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal request: %w", err)
	}

	var lastErr error
	for attempt := 0; attempt < c.maxRetries; attempt++ {
		if attempt > 0 {
			time.Sleep(time.Duration(1<<uint(attempt-1)) * time.Second)
		}

		req, err := http.NewRequest("POST", c.url, bytes.NewReader(body))
		if err != nil {
			return nil, fmt.Errorf("create request: %w", err)
		}
		req.Header.Set("Content-Type", "application/json")
		req.SetBasicAuth(c.username, c.password)

		resp, err := c.client.Do(req)
		if err != nil {
			lastErr = err
			c.connected.Store(false)
			continue
		}

		respBody, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			lastErr = fmt.Errorf("read response: %w", err)
			continue
		}

		var rpcResp rpcResponse
		if err := json.Unmarshal(respBody, &rpcResp); err != nil {
			lastErr = fmt.Errorf("parse response: %w", err)
			continue
		}

		if rpcResp.Error != nil {
			c.connected.Store(true)
			return nil, rpcResp.Error
		}

		c.connected.Store(true)
		return rpcResp.Result, nil
	}

	return nil, fmt.Errorf("RPC call %s failed after %d attempts: %w", method, c.maxRetries, lastErr)
}

func (c *Client) IsConnected() bool {
	return c.connected.Load()
}

func (c *Client) Ping() error {
	_, err := c.call("getbestblockhash", []interface{}{})
	return err
}

func (c *Client) GetBlockTemplate(rules []string) (*BlockTemplate, error) {
	if rules == nil {
		rules = []string{}
	}
	params := []interface{}{
		map[string]interface{}{
			"rules": rules,
		},
	}

	result, err := c.call("getblocktemplate", params)
	if err != nil {
		return nil, err
	}

	var tmpl BlockTemplate
	if err := json.Unmarshal(result, &tmpl); err != nil {
		return nil, fmt.Errorf("parse block template: %w", err)
	}

	return &tmpl, nil
}

func (c *Client) SubmitBlock(blockHex string) error {
	result, err := c.call("submitblock", []interface{}{blockHex})
	if err != nil {
		return err
	}

	// submitblock returns null on success, or an error string
	var rejection string
	if err := json.Unmarshal(result, &rejection); err == nil && rejection != "" {
		return fmt.Errorf("block rejected: %s", rejection)
	}

	return nil
}

func (c *Client) GetBlockchainInfo() (*BlockchainInfo, error) {
	result, err := c.call("getblockchaininfo", []interface{}{})
	if err != nil {
		return nil, err
	}

	var info BlockchainInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return nil, fmt.Errorf("parse blockchain info: %w", err)
	}

	return &info, nil
}

func (c *Client) GetMiningInfo() (*MiningInfo, error) {
	result, err := c.call("getmininginfo", []interface{}{})
	if err != nil {
		return nil, err
	}

	var info MiningInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return nil, fmt.Errorf("parse mining info: %w", err)
	}

	return &info, nil
}

func (c *Client) GetNetworkInfo() (*NetworkInfo, error) {
	result, err := c.call("getnetworkinfo", []interface{}{})
	if err != nil {
		return nil, err
	}

	var info NetworkInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return nil, fmt.Errorf("parse network info: %w", err)
	}

	return &info, nil
}

func (c *Client) GetBestBlockHash() (string, error) {
	result, err := c.call("getbestblockhash", []interface{}{})
	if err != nil {
		return "", err
	}

	var hash string
	if err := json.Unmarshal(result, &hash); err != nil {
		return "", fmt.Errorf("parse block hash: %w", err)
	}

	return hash, nil
}

func (c *Client) ValidateAddress(addr string) (*AddressInfo, error) {
	result, err := c.call("validateaddress", []interface{}{addr})
	if err != nil {
		return nil, err
	}

	var info AddressInfo
	if err := json.Unmarshal(result, &info); err != nil {
		return nil, fmt.Errorf("parse address info: %w", err)
	}

	return &info, nil
}
