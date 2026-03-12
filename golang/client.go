package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Connection is the unified interface for both direct and daemon connections.
type Connection interface {
	ListTools() ([]ToolInfo, error)
	CallTool(toolName string, args map[string]interface{}) (interface{}, error)
	GetInstructions() string
	Close() error
}

// McpConnection wraps an MCP client session.
type McpConnection struct {
	session *mcp.ClientSession
}

// headerTransport is an http.RoundTripper that injects custom headers.
type headerTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}
	return t.base.RoundTrip(req)
}

// connectToServer creates a connection to an MCP server using the official SDK.
func connectToServer(serverName string, config *ServerConfig) (*McpConnection, error) {
	ctx := context.Background()

	client := mcp.NewClient(
		&mcp.Implementation{Name: "mcp-cli", Version: Version},
		nil,
	)

	var transport mcp.Transport

	if config.IsHTTP() {
		httpClient := &http.Client{}
		if config.Timeout > 0 {
			httpClient.Timeout = time.Duration(config.Timeout) * time.Second
		} else {
			httpClient.Timeout = time.Duration(getTimeoutMs()) * time.Millisecond
		}
		// Inject custom headers via a RoundTripper.
		if len(config.Headers) > 0 {
			httpClient.Transport = &headerTransport{
				base:    http.DefaultTransport,
				headers: config.Headers,
			}
		}
		transport = &mcp.StreamableClientTransport{
			Endpoint:   config.URL,
			HTTPClient: httpClient,
		}
	} else {
		cmd := exec.Command(config.Command, config.Args...)
		// Merge process env with config env.
		env := os.Environ()
		for k, v := range config.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
		cmd.Env = env
		if config.Cwd != "" {
			cmd.Dir = config.Cwd
		}
		// Pipe stderr and prefix with [serverName]
		stderrPipe, err := cmd.StderrPipe()
		if err != nil {
			cmd.Stderr = os.Stderr
		} else {
			go func() {
				scanner := bufio.NewScanner(stderrPipe)
				for scanner.Scan() {
					fmt.Fprintf(os.Stderr, "[%s] %s\n", serverName, scanner.Text())
				}
			}()
		}
		transport = &mcp.CommandTransport{Command: cmd}
	}

	session, err := client.Connect(ctx, transport, nil)
	if err != nil {
		return nil, fmt.Errorf("MCP connection failed: %w", err)
	}

	return &McpConnection{session: session}, nil
}

// ListTools returns all tools from the MCP server.
func (c *McpConnection) ListTools() ([]ToolInfo, error) {
	ctx := context.Background()
	var allTools []ToolInfo

	for tool, err := range c.session.Tools(ctx, nil) {
		if err != nil {
			return nil, fmt.Errorf("list tools failed: %w", err)
		}
		allTools = append(allTools, convertTool(tool))
	}
	return allTools, nil
}

// convertTool converts an *mcp.Tool to our ToolInfo display type.
func convertTool(t *mcp.Tool) ToolInfo {
	info := ToolInfo{
		Name:        t.Name,
		Description: t.Description,
	}
	// Convert InputSchema (any) to map[string]interface{} via JSON round-trip.
	if t.InputSchema != nil {
		data, err := json.Marshal(t.InputSchema)
		if err == nil {
			var schema map[string]interface{}
			if json.Unmarshal(data, &schema) == nil {
				info.InputSchema = schema
			}
		}
	}
	return info
}

// CallTool calls a tool with the given arguments.
func (c *McpConnection) CallTool(toolName string, args map[string]interface{}) (interface{}, error) {
	ctx := context.Background()
	params := &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	}

	res, err := c.session.CallTool(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("call tool failed: %w", err)
	}

	if res.IsError {
		// Extract error text from content.
		var errTexts []string
		for _, c := range res.Content {
			if tc, ok := c.(*mcp.TextContent); ok {
				errTexts = append(errTexts, tc.Text)
			}
		}
		if len(errTexts) > 0 {
			return nil, fmt.Errorf("%s", strings.Join(errTexts, "\n"))
		}
		return nil, fmt.Errorf("tool execution failed")
	}

	// Convert the result to a generic interface for formatting.
	data, err := json.Marshal(res)
	if err != nil {
		return res, nil
	}
	var result interface{}
	if json.Unmarshal(data, &result) != nil {
		return res, nil
	}
	return result, nil
}

// GetInstructions returns the server instructions from initialization.
func (c *McpConnection) GetInstructions() string {
	if initResult := c.session.InitializeResult(); initResult != nil {
		return initResult.Instructions
	}
	return ""
}

// Close closes the connection.
func (c *McpConnection) Close() error {
	return c.session.Close()
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
