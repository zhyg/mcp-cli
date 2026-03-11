package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// helper to write a temp config file and return its path.
func writeTempConfig(t *testing.T, data interface{}) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp_servers.json")
	b, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("json.Marshal: %v", err)
	}
	if err := os.WriteFile(path, b, 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func writeTempConfigRaw(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp_servers.json")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

// --- loadConfig ---

func TestLoadConfig_ValidConfig(t *testing.T) {
	path := writeTempConfig(t, map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"test": map[string]interface{}{
				"command": "echo",
				"args":    []string{"hello"},
			},
		},
	})

	config, _, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if config.McpServers["test"] == nil {
		t.Fatal("expected test server to be defined")
	}
	assertEqual(t, config.McpServers["test"].Command, "echo")
}

func TestLoadConfig_MissingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nonexistent.json")

	_, _, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing file")
	}
	assertContains(t, err.Error(), "not found")
}

func TestLoadConfig_InvalidJSON(t *testing.T) {
	path := writeTempConfigRaw(t, "not valid json")

	_, _, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
	assertContains(t, err.Error(), "Invalid JSON")
}

func TestLoadConfig_MissingMcpServers(t *testing.T) {
	path := writeTempConfig(t, map[string]interface{}{
		"servers": map[string]interface{}{},
	})

	_, _, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing mcpServers")
	}
	assertContains(t, err.Error(), "mcpServers")
}

func TestLoadConfig_SubstitutesEnvVars(t *testing.T) {
	os.Setenv("TEST_MCP_TOKEN", "secret123")
	defer os.Unsetenv("TEST_MCP_TOKEN")

	path := writeTempConfig(t, map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"test": map[string]interface{}{
				"url":     "https://example.com",
				"headers": map[string]string{"Authorization": "Bearer ${TEST_MCP_TOKEN}"},
			},
		},
	})

	config, _, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEqual(t, config.McpServers["test"].Headers["Authorization"], "Bearer secret123")
}

func TestLoadConfig_MissingEnvVar_NonStrict(t *testing.T) {
	os.Setenv("MCP_STRICT_ENV", "false")
	defer os.Unsetenv("MCP_STRICT_ENV")
	os.Unsetenv("NONEXISTENT_VAR")

	path := writeTempConfig(t, map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"test": map[string]interface{}{
				"command": "echo",
				"env":     map[string]string{"TOKEN": "${NONEXISTENT_VAR}"},
			},
		},
	})

	config, _, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEqual(t, config.McpServers["test"].Env["TOKEN"], "")
}

func TestLoadConfig_MissingEnvVar_Strict(t *testing.T) {
	os.Unsetenv("MCP_STRICT_ENV")
	os.Unsetenv("ANOTHER_NONEXISTENT_VAR")

	path := writeTempConfig(t, map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"test": map[string]interface{}{
				"command": "echo",
				"env":     map[string]string{"TOKEN": "${ANOTHER_NONEXISTENT_VAR}"},
			},
		},
	})

	_, _, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for missing env var in strict mode")
	}
	assertContains(t, err.Error(), "MISSING_ENV_VAR")
}

func TestLoadConfig_EmptyServerConfig(t *testing.T) {
	path := writeTempConfig(t, map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"badserver": map[string]interface{}{},
		},
	})

	_, _, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for empty server config")
	}
	assertContains(t, err.Error(), "missing required field")
}

func TestLoadConfig_BothCommandAndURL(t *testing.T) {
	path := writeTempConfig(t, map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"mixed": map[string]interface{}{
				"command": "echo",
				"url":     "https://example.com",
			},
		},
	})

	_, _, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for server with both command and url")
	}
	assertContains(t, err.Error(), "both")
}

func TestLoadConfig_NullServerConfig(t *testing.T) {
	path := writeTempConfigRaw(t, `{"mcpServers":{"nullserver":null}}`)

	_, _, err := loadConfig(path)
	if err == nil {
		t.Fatal("expected error for null server config")
	}
	assertContains(t, err.Error(), "Invalid server configuration")
}

// --- getServerConfig ---

func TestGetServerConfig_Found(t *testing.T) {
	path := writeTempConfig(t, map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"server1": map[string]interface{}{"command": "cmd1"},
			"server2": map[string]interface{}{"command": "cmd2"},
		},
	})

	config, _, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sc, err := getServerConfig(config, "server1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	assertEqual(t, sc.Command, "cmd1")
}

func TestGetServerConfig_NotFound(t *testing.T) {
	path := writeTempConfig(t, map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"known": map[string]interface{}{"command": "cmd"},
		},
	})

	config, _, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, err = getServerConfig(config, "unknown")
	if err == nil {
		t.Fatal("expected error for unknown server")
	}
	assertContains(t, err.Error(), "not found")
}

// --- listServerNames ---

func TestListServerNames(t *testing.T) {
	path := writeTempConfig(t, map[string]interface{}{
		"mcpServers": map[string]interface{}{
			"alpha": map[string]interface{}{"command": "a"},
			"beta":  map[string]interface{}{"command": "b"},
			"gamma": map[string]interface{}{"url": "https://example.com"},
		},
	})

	config, _, err := loadConfig(path)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	names := listServerNames(config)
	if len(names) != 3 {
		t.Fatalf("expected 3 names, got %d", len(names))
	}

	nameSet := map[string]bool{}
	for _, n := range names {
		nameSet[n] = true
	}
	for _, expected := range []string{"alpha", "beta", "gamma"} {
		if !nameSet[expected] {
			t.Errorf("expected %q in names", expected)
		}
	}
}

// --- Type guards ---

func TestIsHTTP(t *testing.T) {
	http := &ServerConfig{URL: "https://example.com"}
	stdio := &ServerConfig{Command: "echo"}

	if !http.IsHTTP() {
		t.Error("expected HTTP config to be identified as HTTP")
	}
	if http.IsStdio() {
		t.Error("expected HTTP config to NOT be identified as stdio")
	}
	if !stdio.IsStdio() {
		t.Error("expected stdio config to be identified as stdio")
	}
	if stdio.IsHTTP() {
		t.Error("expected stdio config to NOT be identified as HTTP")
	}
}
