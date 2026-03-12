package main

import (
	"os"
	"testing"
)

func TestIsDaemonEnabled_Default(t *testing.T) {
	os.Unsetenv("MCP_NO_DAEMON")
	if !isDaemonEnabled() {
		t.Error("expected daemon to be enabled by default")
	}
}

func TestIsDaemonEnabled_Disabled(t *testing.T) {
	os.Setenv("MCP_NO_DAEMON", "1")
	defer os.Unsetenv("MCP_NO_DAEMON")
	if isDaemonEnabled() {
		t.Error("expected daemon to be disabled when MCP_NO_DAEMON=1")
	}
}

func TestIsDaemonEnabled_OtherValues(t *testing.T) {
	os.Setenv("MCP_NO_DAEMON", "0")
	defer os.Unsetenv("MCP_NO_DAEMON")
	if !isDaemonEnabled() {
		t.Error("expected daemon to be enabled when MCP_NO_DAEMON=0")
	}
}

func TestGetDaemonTimeoutMs_Default(t *testing.T) {
	os.Unsetenv("MCP_DAEMON_TIMEOUT")
	got := getDaemonTimeoutMs()
	if got != DefaultDaemonTimeoutSeconds*1000 {
		t.Errorf("got %d, want %d", got, DefaultDaemonTimeoutSeconds*1000)
	}
}

func TestGetDaemonTimeoutMs_EnvVar(t *testing.T) {
	os.Setenv("MCP_DAEMON_TIMEOUT", "120")
	defer os.Unsetenv("MCP_DAEMON_TIMEOUT")
	got := getDaemonTimeoutMs()
	if got != 120000 {
		t.Errorf("got %d, want 120000", got)
	}
}

func TestGetDaemonTimeoutMs_Invalid(t *testing.T) {
	os.Setenv("MCP_DAEMON_TIMEOUT", "abc")
	defer os.Unsetenv("MCP_DAEMON_TIMEOUT")
	got := getDaemonTimeoutMs()
	if got != DefaultDaemonTimeoutSeconds*1000 {
		t.Errorf("got %d, want default %d", got, DefaultDaemonTimeoutSeconds*1000)
	}
}

func TestGetDaemonTimeoutMs_Zero(t *testing.T) {
	os.Setenv("MCP_DAEMON_TIMEOUT", "0")
	defer os.Unsetenv("MCP_DAEMON_TIMEOUT")
	got := getDaemonTimeoutMs()
	if got != DefaultDaemonTimeoutSeconds*1000 {
		t.Errorf("got %d, want default %d", got, DefaultDaemonTimeoutSeconds*1000)
	}
}

func TestGetConfigHash_Consistent(t *testing.T) {
	config := &ServerConfig{Command: "echo", Args: []string{"hello"}}
	h1 := getConfigHash(config)
	h2 := getConfigHash(config)
	if h1 != h2 {
		t.Errorf("expected same hash, got %q and %q", h1, h2)
	}
}

func TestGetConfigHash_DifferentConfigs(t *testing.T) {
	c1 := &ServerConfig{Command: "echo"}
	c2 := &ServerConfig{Command: "cat"}
	if getConfigHash(c1) == getConfigHash(c2) {
		t.Error("expected different hashes for different configs")
	}
}

func TestReadPidFile_Nonexistent(t *testing.T) {
	result := readPidFile("nonexistent-server-xxxxx")
	if result != nil {
		t.Error("expected nil for nonexistent PID file")
	}
}

func TestIsProcessRunning_Self(t *testing.T) {
	if !isProcessRunning(os.Getpid()) {
		t.Error("expected current process to be running")
	}
}

func TestIsProcessRunning_InvalidPid(t *testing.T) {
	// PID 0 or very large PIDs shouldn't be running
	if isProcessRunning(9999999) {
		t.Error("expected invalid PID to not be running")
	}
}

func TestCleanupOrphanedDaemons_NoDir(t *testing.T) {
	// Should not panic when socket dir doesn't exist
	cleanupOrphanedDaemons()
}

func TestGetSocketDir(t *testing.T) {
	dir := getSocketDir()
	assertContains(t, dir, "mcp-cli-")
	assertContains(t, dir, "/tmp/")
}

func TestGetSocketPath(t *testing.T) {
	path := getSocketPath("my-server")
	assertContains(t, path, "my-server.sock")
}

func TestGetPidPath(t *testing.T) {
	path := getPidPath("my-server")
	assertContains(t, path, "my-server.pid")
}
