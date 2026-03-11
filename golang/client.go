package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// JSON-RPC 2.0 types for MCP protocol communication.

type jsonRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      int64       `json:"id"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
}

type jsonRPCResponse struct {
	JSONRPC string           `json:"jsonrpc"`
	ID      *json.RawMessage `json:"id,omitempty"`
	Result  json.RawMessage  `json:"result,omitempty"`
	Error   *jsonRPCError    `json:"error,omitempty"`
}

type jsonRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// McpConnection represents a connection to an MCP server.
type McpConnection struct {
	transport    mcpTransport
	instructions string
}

// mcpTransport is the interface for MCP transport implementations.
type mcpTransport interface {
	sendRequest(req *jsonRPCRequest) (*jsonRPCResponse, error)
	close() error
}

// --- Stdio Transport ---

type stdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	reader *bufio.Reader
	mu     sync.Mutex
	nextID int64
}

func newStdioTransport(config *ServerConfig) (*stdioTransport, error) {
	args := config.Args
	cmd := exec.Command(config.Command, args...)

	// Merge process env with config env
	env := os.Environ()
	for k, v := range config.Env {
		env = append(env, fmt.Sprintf("%s=%s", k, v))
	}
	cmd.Env = env

	if config.Cwd != "" {
		cmd.Dir = config.Cwd
	}

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	// Forward stderr
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start server process: %w", err)
	}

	return &stdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		reader: bufio.NewReader(stdout),
		nextID: 1,
	}, nil
}

func (t *stdioTransport) sendRequest(req *jsonRPCRequest) (*jsonRPCResponse, error) {
	t.mu.Lock()
	defer t.mu.Unlock()

	req.JSONRPC = "2.0"
	if req.ID == 0 {
		req.ID = atomic.AddInt64(&t.nextID, 1)
	}

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	debugLog("stdio send: %s", string(data))

	// Write request followed by newline
	if _, err := t.stdin.Write(append(data, '\n')); err != nil {
		return nil, fmt.Errorf("failed to write to server: %w", err)
	}

	// Read response line
	line, err := t.reader.ReadBytes('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	debugLog("stdio recv: %s", strings.TrimSpace(string(line)))

	var resp jsonRPCResponse
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &resp, nil
}

func (t *stdioTransport) close() error {
	t.stdin.Close()
	return t.cmd.Process.Kill()
}

// --- HTTP Transport ---

type httpTransport struct {
	url     string
	headers map[string]string
	client  *http.Client
	nextID  int64
	mu      sync.Mutex

	// Session tracking for Streamable HTTP
	sessionID string
}

func newHTTPTransport(config *ServerConfig) *httpTransport {
	timeout := time.Duration(getTimeoutMs()) * time.Millisecond
	if config.Timeout > 0 {
		timeout = time.Duration(config.Timeout) * time.Second
	}

	return &httpTransport{
		url:     config.URL,
		headers: config.Headers,
		client: &http.Client{
			Timeout: timeout,
		},
		nextID: 1,
	}
}

func (t *httpTransport) sendRequest(req *jsonRPCRequest) (*jsonRPCResponse, error) {
	t.mu.Lock()
	req.JSONRPC = "2.0"
	if req.ID == 0 {
		req.ID = atomic.AddInt64(&t.nextID, 1)
	}
	t.mu.Unlock()

	data, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	debugLog("http send: %s", string(data))

	httpReq, err := http.NewRequest("POST", t.url, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("failed to create HTTP request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/json, text/event-stream")
	for k, v := range t.headers {
		httpReq.Header.Set(k, v)
	}
	if t.sessionID != "" {
		httpReq.Header.Set("Mcp-Session-Id", t.sessionID)
	}

	httpResp, err := t.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("HTTP request failed: %w", err)
	}
	defer httpResp.Body.Close()

	// Save session ID if provided
	if sid := httpResp.Header.Get("Mcp-Session-Id"); sid != "" {
		t.sessionID = sid
	}

	if httpResp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(httpResp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", httpResp.StatusCode, strings.TrimSpace(string(body)))
	}

	contentType := httpResp.Header.Get("Content-Type")

	// Handle SSE responses (text/event-stream)
	if strings.HasPrefix(contentType, "text/event-stream") {
		return t.parseSSEResponse(httpResp.Body)
	}

	// Standard JSON response
	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	debugLog("http recv: %s", string(respBody))

	var resp jsonRPCResponse
	if err := json.Unmarshal(respBody, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &resp, nil
}

// parseSSEResponse reads a Server-Sent Events stream and returns the last JSON-RPC response.
func (t *httpTransport) parseSSEResponse(body io.Reader) (*jsonRPCResponse, error) {
	scanner := bufio.NewScanner(body)
	var lastResponse *jsonRPCResponse

	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")
			debugLog("sse recv: %s", data)

			var resp jsonRPCResponse
			if err := json.Unmarshal([]byte(data), &resp); err == nil {
				lastResponse = &resp
			}
		}
	}

	if lastResponse == nil {
		return nil, fmt.Errorf("no JSON-RPC response found in SSE stream")
	}
	return lastResponse, nil
}

func (t *httpTransport) close() error {
	return nil
}

// --- Connection Management ---

// connectToServer creates a connection to an MCP server.
func connectToServer(serverName string, config *ServerConfig) (*McpConnection, error) {
	var transport mcpTransport
	var err error

	if config.IsHTTP() {
		transport = newHTTPTransport(config)
	} else {
		transport, err = newStdioTransport(config)
		if err != nil {
			return nil, err
		}
	}

	conn := &McpConnection{
		transport: transport,
	}

	// Initialize the MCP connection
	if err := conn.initialize(); err != nil {
		transport.close()
		return nil, fmt.Errorf("MCP initialization failed: %w", err)
	}

	return conn, nil
}

// initialize performs the MCP initialize handshake.
func (c *McpConnection) initialize() error {
	req := &jsonRPCRequest{
		Method: "initialize",
		Params: map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities":    map[string]interface{}{},
			"clientInfo": map[string]interface{}{
				"name":    "mcp-cli",
				"version": Version,
			},
		},
	}

	resp, err := c.transport.sendRequest(req)
	if err != nil {
		return fmt.Errorf("initialize request failed: %w", err)
	}

	if resp.Error != nil {
		return fmt.Errorf("initialize error: %s", resp.Error.Message)
	}

	// Parse init result to get instructions
	var initResult struct {
		Instructions string `json:"instructions"`
	}
	if resp.Result != nil {
		json.Unmarshal(resp.Result, &initResult)
		c.instructions = initResult.Instructions
	}

	// Send initialized notification
	notif := &jsonRPCRequest{
		Method: "notifications/initialized",
	}
	// Notifications don't have IDs in JSON-RPC, but our transport always adds one.
	// Send it anyway; the server should ignore the id field for notifications.
	c.transport.sendRequest(notif)

	return nil
}

// ListTools returns all tools from the MCP server.
func (c *McpConnection) ListTools() ([]ToolInfo, error) {
	req := &jsonRPCRequest{
		Method: "tools/list",
	}

	resp, err := c.transport.sendRequest(req)
	if err != nil {
		return nil, fmt.Errorf("list tools failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("list tools error: %s", resp.Error.Message)
	}

	var result struct {
		Tools []ToolInfo `json:"tools"`
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse tools: %w", err)
	}

	return result.Tools, nil
}

// CallTool calls a tool with the given arguments.
func (c *McpConnection) CallTool(toolName string, args map[string]interface{}) (interface{}, error) {
	req := &jsonRPCRequest{
		Method: "tools/call",
		Params: map[string]interface{}{
			"name":      toolName,
			"arguments": args,
		},
	}

	resp, err := c.transport.sendRequest(req)
	if err != nil {
		return nil, fmt.Errorf("call tool failed: %w", err)
	}

	if resp.Error != nil {
		return nil, fmt.Errorf("%s", resp.Error.Message)
	}

	var result interface{}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	return result, nil
}

// GetInstructions returns the server instructions from initialization.
func (c *McpConnection) GetInstructions() string {
	return c.instructions
}

// Close closes the connection.
func (c *McpConnection) Close() error {
	return c.transport.close()
}

// --- Retry Logic ---

// isTransientError checks if an error is transient and worth retrying.
func isTransientError(err error) bool {
	msg := err.Error()

	// HTTP transient errors
	for _, code := range []string{"502", "503", "504", "429"} {
		if strings.HasPrefix(msg, "HTTP "+code) || strings.Contains(msg, code+" ") {
			return true
		}
	}

	// Network errors
	transientPatterns := []string{
		"connection refused",
		"connection reset",
		"timeout",
		"network",
		"ECONNREFUSED",
		"ECONNRESET",
		"ETIMEDOUT",
	}
	msgLower := strings.ToLower(msg)
	for _, pattern := range transientPatterns {
		if strings.Contains(msgLower, strings.ToLower(pattern)) {
			return true
		}
	}

	return false
}

// withRetry executes fn with retry logic for transient failures.
func withRetry(operationName string, fn func() error) error {
	maxRetries := getMaxRetries()
	baseDelay := getRetryDelayMs()
	totalBudgetMs := getTimeoutMs()
	startTime := time.Now()

	var lastErr error
	for attempt := 0; attempt <= maxRetries; attempt++ {
		elapsed := time.Since(startTime).Milliseconds()
		if elapsed >= int64(totalBudgetMs) {
			debugLog("%s: timeout budget exhausted after %dms", operationName, elapsed)
			break
		}

		lastErr = fn()
		if lastErr == nil {
			return nil
		}

		remaining := int64(totalBudgetMs) - time.Since(startTime).Milliseconds()
		shouldRetry := attempt < maxRetries && isTransientError(lastErr) && remaining > 1000

		if shouldRetry {
			delay := calculateBackoffDelay(attempt, baseDelay)
			if int64(delay) > remaining-1000 {
				delay = int(remaining - 1000)
			}
			debugLog("%s failed (attempt %d/%d): %s. Retrying in %dms...",
				operationName, attempt+1, maxRetries+1, lastErr.Error(), delay)
			time.Sleep(time.Duration(delay) * time.Millisecond)
		} else if lastErr != nil {
			return lastErr
		}
	}

	return lastErr
}

// calculateBackoffDelay calculates exponential backoff with jitter.
func calculateBackoffDelay(attempt, baseDelay int) int {
	exponential := float64(baseDelay) * math.Pow(2, float64(attempt))
	capped := math.Min(exponential, 10000)
	jitter := capped * 0.25 * (rand.Float64()*2 - 1)
	return int(capped + jitter)
}

// connectWithRetry connects to a server with retry logic.
func connectWithRetry(serverName string, config *ServerConfig) (*McpConnection, error) {
	var conn *McpConnection
	err := withRetry("connect to "+serverName, func() error {
		var err error
		conn, err = connectToServer(serverName, config)
		return err
	})
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// safeClose closes a connection, logging but not propagating errors.
func safeClose(conn *McpConnection) {
	if conn != nil {
		if err := conn.Close(); err != nil {
			debugLog("Failed to close connection: %s", err.Error())
		}
	}
}
