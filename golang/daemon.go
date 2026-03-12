package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// DaemonRequest represents a request sent to the daemon over Unix socket.
type DaemonRequest struct {
	ID       string                 `json:"id"`
	Type     string                 `json:"type"` // ping, listTools, callTool, getInstructions, close
	ToolName string                 `json:"toolName,omitempty"`
	Args     map[string]interface{} `json:"args,omitempty"`
}

// DaemonResponse represents a response from the daemon.
type DaemonResponse struct {
	ID      string       `json:"id"`
	Success bool         `json:"success"`
	Data    interface{}  `json:"data,omitempty"`
	Error   *DaemonError `json:"error,omitempty"`
}

// DaemonError represents an error in a daemon response.
type DaemonError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// PidFileContent holds the PID file data for stale detection.
type PidFileContent struct {
	PID        int    `json:"pid"`
	ConfigHash string `json:"configHash"`
	StartedAt  string `json:"startedAt"`
}

// writePidFile writes a PID file with config hash for stale detection.
func writePidFile(serverName, configHash string) error {
	pidPath := getPidPath(serverName)
	dir := filepath.Dir(pidPath)

	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	content := PidFileContent{
		PID:        os.Getpid(),
		ConfigHash: configHash,
		StartedAt:  time.Now().UTC().Format(time.RFC3339),
	}

	data, err := json.Marshal(content)
	if err != nil {
		return err
	}

	return os.WriteFile(pidPath, data, 0600)
}

// readPidFile reads a PID file.
func readPidFile(serverName string) *PidFileContent {
	pidPath := getPidPath(serverName)
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return nil
	}

	var content PidFileContent
	if err := json.Unmarshal(data, &content); err != nil {
		return nil
	}
	return &content
}

// removePidFile removes a PID file.
func removePidFile(serverName string) {
	os.Remove(getPidPath(serverName))
}

// removeSocketFile removes a socket file.
func removeSocketFile(serverName string) {
	os.Remove(getSocketPath(serverName))
}

// isProcessRunning checks if a process is running by sending signal 0.
func isProcessRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	// Signal 0 checks process existence without sending a signal.
	err = proc.Signal(syscall0)
	return err == nil
}

// killProcess sends SIGTERM to a process by PID.
func killProcess(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Kill() == nil
}

// runDaemon runs the daemon process for a server.
func runDaemon(serverName string, config *ServerConfig) error {
	socketPath := getSocketPath(serverName)
	configHash := getConfigHash(config)
	timeoutMs := getDaemonTimeoutMs()

	var mcpConn *McpConnection
	var listener net.Listener
	var mu sync.Mutex
	var idleTimer *time.Timer

	cleanup := func() {
		debugLog("[daemon:%s] Shutting down...", serverName)

		mu.Lock()
		if idleTimer != nil {
			idleTimer.Stop()
		}
		mu.Unlock()

		if listener != nil {
			listener.Close()
		}

		if mcpConn != nil {
			mcpConn.Close()
		}

		removeSocketFile(serverName)
		removePidFile(serverName)
		debugLog("[daemon:%s] Cleanup complete", serverName)
	}

	resetIdleTimer := func() {
		mu.Lock()
		defer mu.Unlock()
		if idleTimer != nil {
			idleTimer.Stop()
		}
		idleTimer = time.AfterFunc(time.Duration(timeoutMs)*time.Millisecond, func() {
			debugLog("[daemon:%s] Idle timeout reached, shutting down", serverName)
			cleanup()
			os.Exit(0)
		})
	}

	// Ensure socket dir exists
	socketDir := getSocketDir()
	if err := os.MkdirAll(socketDir, 0700); err != nil {
		return fmt.Errorf("failed to create socket dir: %w", err)
	}

	// Remove stale socket
	removeSocketFile(serverName)

	// Write PID file
	if err := writePidFile(serverName, configHash); err != nil {
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Connect to MCP server
	debugLog("[daemon:%s] Connecting to MCP server...", serverName)
	var err error
	mcpConn, err = connectToServer(serverName, config)
	if err != nil {
		cleanup()
		return fmt.Errorf("failed to connect: %w", err)
	}
	debugLog("[daemon:%s] Connected to MCP server", serverName)

	// Handle requests
	handleRequest := func(data []byte) *DaemonResponse {
		resetIdleTimer()

		var req DaemonRequest
		if err := json.Unmarshal(data, &req); err != nil {
			return &DaemonResponse{
				ID:      "unknown",
				Success: false,
				Error:   &DaemonError{Code: "INVALID_REQUEST", Message: "Invalid JSON"},
			}
		}

		debugLog("[daemon:%s] Request: %s (%s)", serverName, req.Type, req.ID)

		if mcpConn == nil {
			return &DaemonResponse{
				ID:      req.ID,
				Success: false,
				Error:   &DaemonError{Code: "NOT_CONNECTED", Message: "MCP client not connected"},
			}
		}

		switch req.Type {
		case "ping":
			return &DaemonResponse{ID: req.ID, Success: true, Data: "pong"}

		case "listTools":
			tools, err := mcpConn.ListTools()
			if err != nil {
				return &DaemonResponse{
					ID:      req.ID,
					Success: false,
					Error:   &DaemonError{Code: "EXECUTION_ERROR", Message: err.Error()},
				}
			}
			return &DaemonResponse{ID: req.ID, Success: true, Data: tools}

		case "callTool":
			if req.ToolName == "" {
				return &DaemonResponse{
					ID:      req.ID,
					Success: false,
					Error:   &DaemonError{Code: "MISSING_TOOL", Message: "toolName required"},
				}
			}
			args := req.Args
			if args == nil {
				args = map[string]interface{}{}
			}
			result, err := mcpConn.CallTool(req.ToolName, args)
			if err != nil {
				return &DaemonResponse{
					ID:      req.ID,
					Success: false,
					Error:   &DaemonError{Code: "EXECUTION_ERROR", Message: err.Error()},
				}
			}
			return &DaemonResponse{ID: req.ID, Success: true, Data: result}

		case "getInstructions":
			instructions := mcpConn.GetInstructions()
			return &DaemonResponse{ID: req.ID, Success: true, Data: instructions}

		case "close":
			go func() {
				time.Sleep(100 * time.Millisecond)
				cleanup()
				os.Exit(0)
			}()
			return &DaemonResponse{ID: req.ID, Success: true, Data: "closing"}

		default:
			return &DaemonResponse{
				ID:      req.ID,
				Success: false,
				Error:   &DaemonError{Code: "UNKNOWN_TYPE", Message: fmt.Sprintf("Unknown request type: %s", req.Type)},
			}
		}
	}

	// Start Unix socket server
	listener, err = net.Listen("unix", socketPath)
	if err != nil {
		cleanup()
		return fmt.Errorf("failed to listen on socket: %w", err)
	}

	debugLog("[daemon:%s] Listening on %s", serverName, socketPath)
	resetIdleTimer()

	// Signal readiness
	fmt.Println("DAEMON_READY")

	// Accept connections
	for {
		conn, err := listener.Accept()
		if err != nil {
			debugLog("[daemon:%s] Accept error: %s", serverName, err.Error())
			break
		}

		go func(c net.Conn) {
			defer c.Close()
			buf := make([]byte, 64*1024)
			n, err := c.Read(buf)
			if err != nil {
				return
			}
			resp := handleRequest(buf[:n])
			data, _ := json.Marshal(resp)
			c.Write(append(data, '\n'))
		}(conn)
	}

	return nil
}

// cleanupOrphanedDaemons cleans up orphaned daemon processes and sockets.
func cleanupOrphanedDaemons() {
	socketDir := getSocketDir()
	entries, err := os.ReadDir(socketDir)
	if err != nil {
		return
	}

	for _, entry := range entries {
		if !strings.HasSuffix(entry.Name(), ".pid") {
			continue
		}
		serverName := strings.TrimSuffix(entry.Name(), ".pid")
		pidInfo := readPidFile(serverName)
		if pidInfo != nil && !isProcessRunning(pidInfo.PID) {
			debugLog("[daemon-client] Cleaning up orphaned daemon: %s", serverName)
			removePidFile(serverName)
			removeSocketFile(serverName)
		}
	}
}

// isDaemonValid checks if daemon is running and has matching config.
func isDaemonValid(serverName string, config *ServerConfig) bool {
	pidInfo := readPidFile(serverName)
	if pidInfo == nil {
		debugLog("[daemon-client] No PID file for %s", serverName)
		return false
	}

	if !isProcessRunning(pidInfo.PID) {
		debugLog("[daemon-client] Process %d not running, cleaning up", pidInfo.PID)
		removePidFile(serverName)
		removeSocketFile(serverName)
		return false
	}

	currentHash := getConfigHash(config)
	if pidInfo.ConfigHash != currentHash {
		debugLog("[daemon-client] Config hash mismatch for %s, killing old daemon", serverName)
		killProcess(pidInfo.PID)
		removePidFile(serverName)
		removeSocketFile(serverName)
		return false
	}

	socketPath := getSocketPath(serverName)
	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		debugLog("[daemon-client] Socket missing for %s, cleaning up", serverName)
		killProcess(pidInfo.PID)
		removePidFile(serverName)
		return false
	}

	return true
}

// spawnDaemon spawns a new daemon process for a server.
func spawnDaemon(serverName string, config *ServerConfig) bool {
	debugLog("[daemon-client] Spawning daemon for %s", serverName)

	execPath, err := os.Executable()
	if err != nil {
		debugLog("[daemon-client] Failed to get executable path: %s", err.Error())
		return false
	}

	configJSON, err := json.Marshal(config)
	if err != nil {
		return false
	}

	cmd := exec.Command(execPath, "--daemon", serverName, string(configJSON))

	// Create a pipe to read stdout for DAEMON_READY signal
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return false
	}
	cmd.Stderr = os.Stderr

	if err := cmd.Start(); err != nil {
		debugLog("[daemon-client] Failed to start daemon: %s", err.Error())
		return false
	}

	// Wait for DAEMON_READY or timeout
	readyCh := make(chan bool, 1)
	go func() {
		buf := make([]byte, 1024)
		for {
			n, err := stdout.Read(buf)
			if err != nil {
				readyCh <- false
				return
			}
			if strings.Contains(string(buf[:n]), "DAEMON_READY") {
				readyCh <- true
				return
			}
		}
	}()

	select {
	case ready := <-readyCh:
		if ready {
			go cmd.Wait()
			return true
		}
		return false
	case <-time.After(5 * time.Second):
		debugLog("[daemon-client] Daemon spawn timeout for %s", serverName)
		cmd.Process.Kill()
		return false
	}
}

// sendDaemonRequest sends a request to the daemon and waits for response.
func sendDaemonRequest(socketPath string, req *DaemonRequest) (*DaemonResponse, error) {
	conn, err := net.DialTimeout("unix", socketPath, 5*time.Second)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	conn.SetDeadline(time.Now().Add(5 * time.Second))

	data, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}

	if _, err := conn.Write(data); err != nil {
		return nil, err
	}

	buf := make([]byte, 256*1024)
	n, err := conn.Read(buf)
	if err != nil {
		return nil, err
	}

	var resp DaemonResponse
	if err := json.Unmarshal(buf[:n], &resp); err != nil {
		return nil, fmt.Errorf("invalid response from daemon")
	}

	return &resp, nil
}

// generateRequestID generates a unique request ID.
func generateRequestID() string {
	return fmt.Sprintf("%d-%d", time.Now().UnixMilli(), os.Getpid())
}

// DaemonConnection represents a connection to a daemon for a specific server.
type DaemonConnection struct {
	serverName string
	socketPath string
}

// ListTools lists tools via daemon.
func (d *DaemonConnection) ListTools() ([]ToolInfo, error) {
	resp, err := sendDaemonRequest(d.socketPath, &DaemonRequest{
		ID:   generateRequestID(),
		Type: "listTools",
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		msg := "listTools failed"
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		return nil, fmt.Errorf("%s", msg)
	}

	// Convert data to []ToolInfo via JSON round-trip
	data, err := json.Marshal(resp.Data)
	if err != nil {
		return nil, err
	}
	var tools []ToolInfo
	if err := json.Unmarshal(data, &tools); err != nil {
		return nil, err
	}
	return tools, nil
}

// CallTool calls a tool via daemon.
func (d *DaemonConnection) CallTool(toolName string, args map[string]interface{}) (interface{}, error) {
	resp, err := sendDaemonRequest(d.socketPath, &DaemonRequest{
		ID:       generateRequestID(),
		Type:     "callTool",
		ToolName: toolName,
		Args:     args,
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		msg := "callTool failed"
		if resp.Error != nil {
			msg = resp.Error.Message
		}
		return nil, fmt.Errorf("%s", msg)
	}
	return resp.Data, nil
}

// GetInstructions gets instructions via daemon.
func (d *DaemonConnection) GetInstructions() string {
	resp, err := sendDaemonRequest(d.socketPath, &DaemonRequest{
		ID:   generateRequestID(),
		Type: "getInstructions",
	})
	if err != nil {
		return ""
	}
	if !resp.Success {
		return ""
	}
	if s, ok := resp.Data.(string); ok {
		return s
	}
	return ""
}

// Close disconnects from the daemon (does not shut it down).
func (d *DaemonConnection) Close() error {
	debugLog("[daemon-client] Disconnecting from %s daemon", d.serverName)
	return nil
}

// getDaemonConnection gets or creates a daemon connection for a server.
// Returns nil, err if daemon mode fails (caller should fallback to direct connection).
func getDaemonConnection(serverName string, config *ServerConfig) (*DaemonConnection, error) {
	socketPath := getSocketPath(serverName)

	if !isDaemonValid(serverName, config) {
		if !spawnDaemon(serverName, config) {
			debugLog("[daemon-client] Failed to spawn daemon for %s", serverName)
			return nil, fmt.Errorf("failed to spawn daemon")
		}
		time.Sleep(100 * time.Millisecond)
	}

	if _, err := os.Stat(socketPath); os.IsNotExist(err) {
		debugLog("[daemon-client] Socket not found after spawn for %s", serverName)
		return nil, fmt.Errorf("socket not found")
	}

	// Test connection with ping
	resp, err := sendDaemonRequest(socketPath, &DaemonRequest{
		ID:   generateRequestID(),
		Type: "ping",
	})
	if err != nil {
		debugLog("[daemon-client] Connection test failed for %s: %s", serverName, err.Error())
		return nil, err
	}
	if !resp.Success {
		debugLog("[daemon-client] Ping failed for %s", serverName)
		return nil, fmt.Errorf("ping failed")
	}

	debugLog("[daemon-client] Connected to daemon for %s", serverName)
	return &DaemonConnection{serverName: serverName, socketPath: socketPath}, nil
}

// getUnifiedConnection returns a Connection (daemon or direct) to an MCP server.
func getUnifiedConnection(serverName string, config *ServerConfig) (Connection, error) {
	cleanupOrphanedDaemons()

	if isDaemonEnabled() {
		daemonConn, err := getDaemonConnection(serverName, config)
		if err == nil && daemonConn != nil {
			debugLog("Using daemon connection for %s", serverName)
			return daemonConn, nil
		}
		debugLog("Daemon connection failed for %s, falling back to direct", serverName)
	}

	debugLog("Using direct connection for %s", serverName)
	conn, err := connectWithRetry(serverName, config)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

// daemonMain is the entry point for daemon mode (--daemon flag).
func daemonMain(serverName, configJSON string) {
	var config ServerConfig
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		fmt.Fprintln(os.Stderr, "Invalid config JSON")
		os.Exit(1)
	}

	if err := runDaemon(serverName, &config); err != nil {
		fmt.Fprintf(os.Stderr, "Daemon failed: %s\n", err.Error())
		os.Exit(1)
	}
}
