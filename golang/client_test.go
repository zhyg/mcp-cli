package main

import (
	"errors"
	"os"
	"testing"
)

// --- isTransientError ---

func TestIsTransientError_HTTPCodes(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"HTTP 502 Bad Gateway", true},
		{"HTTP 503 Service Unavailable", true},
		{"HTTP 504 Gateway Timeout", true},
		{"HTTP 429 Too Many Requests", true},
		{"502 Bad Gateway", true},
		{"503 Service Unavailable", true},
	}
	for _, tt := range tests {
		got := isTransientError(errors.New(tt.msg))
		if got != tt.want {
			t.Errorf("isTransientError(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestIsTransientError_NetworkErrors(t *testing.T) {
	tests := []struct {
		msg  string
		want bool
	}{
		{"network error occurred", true},
		{"network timeout", true},
		{"connection reset by peer", true},
		{"connection refused", true},
		{"request timeout", true},
	}
	for _, tt := range tests {
		got := isTransientError(errors.New(tt.msg))
		if got != tt.want {
			t.Errorf("isTransientError(%q) = %v, want %v", tt.msg, got, tt.want)
		}
	}
}

func TestIsTransientError_NonTransient(t *testing.T) {
	tests := []string{
		"Invalid JSON",
		"Permission denied",
		"Not found",
	}
	for _, msg := range tests {
		if isTransientError(errors.New(msg)) {
			t.Errorf("isTransientError(%q) = true, want false", msg)
		}
	}
}

// --- getTimeoutMs ---

func TestGetTimeoutMs_Default(t *testing.T) {
	os.Unsetenv("MCP_TIMEOUT")
	assertEqual(t, getTimeoutMs(), 1800000)
}

func TestGetTimeoutMs_EnvVar(t *testing.T) {
	os.Setenv("MCP_TIMEOUT", "60")
	defer os.Unsetenv("MCP_TIMEOUT")
	assertEqual(t, getTimeoutMs(), 60000)
}

func TestGetTimeoutMs_Invalid(t *testing.T) {
	os.Setenv("MCP_TIMEOUT", "invalid")
	defer os.Unsetenv("MCP_TIMEOUT")
	assertEqual(t, getTimeoutMs(), 1800000)
}

func TestGetTimeoutMs_Negative(t *testing.T) {
	os.Setenv("MCP_TIMEOUT", "-5")
	defer os.Unsetenv("MCP_TIMEOUT")
	assertEqual(t, getTimeoutMs(), 1800000)
}

func TestGetTimeoutMs_Zero(t *testing.T) {
	os.Setenv("MCP_TIMEOUT", "0")
	defer os.Unsetenv("MCP_TIMEOUT")
	assertEqual(t, getTimeoutMs(), 1800000)
}

// --- getConcurrencyLimit ---

func TestGetConcurrencyLimit_Default(t *testing.T) {
	os.Unsetenv("MCP_CONCURRENCY")
	assertEqual(t, getConcurrencyLimit(), 5)
}

func TestGetConcurrencyLimit_EnvVar(t *testing.T) {
	os.Setenv("MCP_CONCURRENCY", "10")
	defer os.Unsetenv("MCP_CONCURRENCY")
	assertEqual(t, getConcurrencyLimit(), 10)
}

func TestGetConcurrencyLimit_Negative(t *testing.T) {
	os.Setenv("MCP_CONCURRENCY", "-3")
	defer os.Unsetenv("MCP_CONCURRENCY")
	assertEqual(t, getConcurrencyLimit(), 5)
}

func TestGetConcurrencyLimit_Zero(t *testing.T) {
	os.Setenv("MCP_CONCURRENCY", "0")
	defer os.Unsetenv("MCP_CONCURRENCY")
	assertEqual(t, getConcurrencyLimit(), 5)
}

func TestGetConcurrencyLimit_Invalid(t *testing.T) {
	os.Setenv("MCP_CONCURRENCY", "many")
	defer os.Unsetenv("MCP_CONCURRENCY")
	assertEqual(t, getConcurrencyLimit(), 5)
}

// --- getMaxRetries ---

func TestGetMaxRetries_Default(t *testing.T) {
	os.Unsetenv("MCP_MAX_RETRIES")
	assertEqual(t, getMaxRetries(), 3)
}

func TestGetMaxRetries_EnvVar(t *testing.T) {
	os.Setenv("MCP_MAX_RETRIES", "5")
	defer os.Unsetenv("MCP_MAX_RETRIES")
	assertEqual(t, getMaxRetries(), 5)
}

func TestGetMaxRetries_Zero(t *testing.T) {
	os.Setenv("MCP_MAX_RETRIES", "0")
	defer os.Unsetenv("MCP_MAX_RETRIES")
	assertEqual(t, getMaxRetries(), 0)
}

func TestGetMaxRetries_Negative(t *testing.T) {
	os.Setenv("MCP_MAX_RETRIES", "-1")
	defer os.Unsetenv("MCP_MAX_RETRIES")
	assertEqual(t, getMaxRetries(), 3)
}

// --- getRetryDelayMs ---

func TestGetRetryDelayMs_Default(t *testing.T) {
	os.Unsetenv("MCP_RETRY_DELAY")
	assertEqual(t, getRetryDelayMs(), 1000)
}

func TestGetRetryDelayMs_EnvVar(t *testing.T) {
	os.Setenv("MCP_RETRY_DELAY", "2000")
	defer os.Unsetenv("MCP_RETRY_DELAY")
	assertEqual(t, getRetryDelayMs(), 2000)
}

func TestGetRetryDelayMs_Zero(t *testing.T) {
	os.Setenv("MCP_RETRY_DELAY", "0")
	defer os.Unsetenv("MCP_RETRY_DELAY")
	assertEqual(t, getRetryDelayMs(), 1000)
}

func TestGetRetryDelayMs_Negative(t *testing.T) {
	os.Setenv("MCP_RETRY_DELAY", "-500")
	defer os.Unsetenv("MCP_RETRY_DELAY")
	assertEqual(t, getRetryDelayMs(), 1000)
}

// --- Type guards ---

func TestServerConfig_TypeGuards(t *testing.T) {
	httpConfig := &ServerConfig{URL: "https://mcp.example.com", Headers: map[string]string{"Authorization": "Bearer token"}}
	stdioConfig := &ServerConfig{Command: "node", Args: []string{"./server.js"}, Env: map[string]string{"DEBUG": "true"}}
	minHTTP := &ServerConfig{URL: "https://example.com"}
	minStdio := &ServerConfig{Command: "echo"}

	if !httpConfig.IsHTTP() {
		t.Error("expected HTTP config to be identified as HTTP")
	}
	if httpConfig.IsStdio() {
		t.Error("expected HTTP config to NOT be stdio")
	}

	if !stdioConfig.IsStdio() {
		t.Error("expected stdio config to be identified as stdio")
	}
	if stdioConfig.IsHTTP() {
		t.Error("expected stdio config to NOT be HTTP")
	}

	if !minHTTP.IsHTTP() {
		t.Error("expected minimal HTTP config to be HTTP")
	}
	if !minStdio.IsStdio() {
		t.Error("expected minimal stdio config to be stdio")
	}
}
