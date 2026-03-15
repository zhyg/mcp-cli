package main

import (
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// Version is the CLI version.
const Version = "0.3.0"

// Default configuration values.
const (
	DefaultTimeoutSeconds       = 1800 // 30 minutes
	DefaultConcurrency          = 5
	DefaultMaxRetries           = 3
	DefaultRetryDelayMs         = 1000 // 1 second
	DefaultDaemonTimeoutSeconds = 60   // 60 seconds idle timeout
)

// ServerConfig represents a server configuration entry.
// It can be either stdio-based (Command set) or HTTP-based (URL set).
type ServerConfig struct {
	// stdio fields
	Command string            `json:"command,omitempty"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	Cwd     string            `json:"cwd,omitempty"`

	// HTTP fields
	URL     string            `json:"url,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
	Timeout int               `json:"timeout,omitempty"`

	// Tool filtering
	AllowedTools  []string `json:"allowedTools,omitempty"`
	DisabledTools []string `json:"disabledTools,omitempty"`
}

// IsHTTP returns true if this is an HTTP server config.
func (c *ServerConfig) IsHTTP() bool {
	return c.URL != ""
}

// IsStdio returns true if this is a stdio server config.
func (c *ServerConfig) IsStdio() bool {
	return c.Command != ""
}

// McpServersConfig represents the top-level config file structure.
type McpServersConfig struct {
	McpServers map[string]*ServerConfig `json:"mcpServers"`
}

// ToolInfo holds tool metadata from an MCP server.
type ToolInfo struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	InputSchema map[string]interface{} `json:"inputSchema"`
}

// --- Tool Filtering ---

// matchesGlobPattern matches a tool name against a glob pattern (supports * and ?).
func matchesGlobPattern(name, pattern string) bool {
	// Convert glob to regex
	regexPattern := regexp.QuoteMeta(pattern)
	regexPattern = strings.ReplaceAll(regexPattern, `\*`, ".*")
	regexPattern = strings.ReplaceAll(regexPattern, `\?`, ".")
	re, err := regexp.Compile("(?i)^" + regexPattern + "$")
	if err != nil {
		return false
	}
	return re.MatchString(name)
}

// matchesAnyPattern returns true if name matches any of the given patterns.
func matchesAnyPattern(name string, patterns []string) bool {
	for _, p := range patterns {
		if matchesGlobPattern(name, p) {
			return true
		}
	}
	return false
}

// filterTools filters tools based on allowedTools and disabledTools configuration.
func filterTools(tools []ToolInfo, config *ServerConfig) []ToolInfo {
	if len(config.AllowedTools) == 0 && len(config.DisabledTools) == 0 {
		return tools
	}

	var result []ToolInfo
	for _, tool := range tools {
		// disabledTools takes precedence
		if len(config.DisabledTools) > 0 && matchesAnyPattern(tool.Name, config.DisabledTools) {
			continue
		}
		// allowedTools check
		if len(config.AllowedTools) > 0 && !matchesAnyPattern(tool.Name, config.AllowedTools) {
			continue
		}
		result = append(result, tool)
	}
	return result
}

// isToolAllowed checks if a specific tool is allowed by the config.
func isToolAllowed(toolName string, config *ServerConfig) bool {
	if len(config.DisabledTools) > 0 && matchesAnyPattern(toolName, config.DisabledTools) {
		return false
	}
	if len(config.AllowedTools) > 0 && !matchesAnyPattern(toolName, config.AllowedTools) {
		return false
	}
	return true
}

// --- Environment ---

// debugLog logs a message to stderr if MCP_DEBUG is set.
func debugLog(format string, args ...interface{}) {
	if os.Getenv("MCP_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "[mcp-cli] "+format+"\n", args...)
	}
}

// getTimeoutMs returns configured timeout in milliseconds.
func getTimeoutMs() int {
	if v := os.Getenv("MCP_TIMEOUT"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return secs * 1000
		}
	}
	return DefaultTimeoutSeconds * 1000
}

// getConcurrencyLimit returns configured concurrency limit.
func getConcurrencyLimit() int {
	if v := os.Getenv("MCP_CONCURRENCY"); v != "" {
		if limit, err := strconv.Atoi(v); err == nil && limit > 0 {
			return limit
		}
	}
	return DefaultConcurrency
}

// getMaxRetries returns configured max retry attempts.
func getMaxRetries() int {
	if v := os.Getenv("MCP_MAX_RETRIES"); v != "" {
		if retries, err := strconv.Atoi(v); err == nil && retries >= 0 {
			return retries
		}
	}
	return DefaultMaxRetries
}

// getRetryDelayMs returns configured base retry delay.
func getRetryDelayMs() int {
	if v := os.Getenv("MCP_RETRY_DELAY"); v != "" {
		if delay, err := strconv.Atoi(v); err == nil && delay > 0 {
			return delay
		}
	}
	return DefaultRetryDelayMs
}

// isDaemonEnabled returns true if daemon mode is enabled.
func isDaemonEnabled() bool {
	return os.Getenv("MCP_NO_DAEMON") != "1"
}

// getDaemonTimeoutMs returns configured daemon idle timeout in milliseconds.
func getDaemonTimeoutMs() int {
	if v := os.Getenv("MCP_DAEMON_TIMEOUT"); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return secs * 1000
		}
	}
	return DefaultDaemonTimeoutSeconds * 1000
}

// getSocketDir returns the socket directory for daemon connections.
func getSocketDir() string {
	uid := os.Getuid()
	return filepath.Join("/tmp", fmt.Sprintf("mcp-cli-%d", uid))
}

// getSocketPath returns socket path for a specific server.
func getSocketPath(serverName string) string {
	return filepath.Join(getSocketDir(), serverName+".sock")
}

// getPidPath returns PID file path for a specific server daemon.
func getPidPath(serverName string) string {
	return filepath.Join(getSocketDir(), serverName+".pid")
}

// getConfigHash generates a hash of server config for stale detection.
func getConfigHash(config *ServerConfig) string {
	data, _ := json.Marshal(config)
	h := sha256.Sum256(data)
	return fmt.Sprintf("%x", h[:8])
}

// isStrictEnvMode returns true if strict env var mode is enabled.
func isStrictEnvMode() bool {
	v := strings.ToLower(os.Getenv("MCP_STRICT_ENV"))
	return v != "false" && v != "0"
}

// envVarRegex matches ${VAR_NAME} patterns.
var envVarRegex = regexp.MustCompile(`\$\{([^}]+)\}`)

// substituteEnvVars replaces ${VAR_NAME} patterns in a string with env var values.
func substituteEnvVars(value string) (string, error) {
	var missingVars []string

	result := envVarRegex.ReplaceAllStringFunc(value, func(match string) string {
		varName := envVarRegex.FindStringSubmatch(match)[1]
		envValue, ok := os.LookupEnv(varName)
		if !ok {
			missingVars = append(missingVars, varName)
			return ""
		}
		return envValue
	})

	if len(missingVars) > 0 {
		varList := strings.Join(missingVars, ", ")
		msg := fmt.Sprintf("Missing environment variable(s): %s", varList)
		if isStrictEnvMode() {
			return "", fmt.Errorf("%s", msg)
		}
		fmt.Fprintf(os.Stderr, "Warning: %s (using empty values)\n", msg)
	}

	return result, nil
}

// substituteEnvVarsInObject recursively substitutes ${VAR} patterns in all string values.
func substituteEnvVarsInObject(v interface{}) (interface{}, error) {
	switch val := v.(type) {
	case string:
		return substituteEnvVars(val)
	case []interface{}:
		result := make([]interface{}, len(val))
		for i, item := range val {
			substituted, err := substituteEnvVarsInObject(item)
			if err != nil {
				return nil, err
			}
			result[i] = substituted
		}
		return result, nil
	case map[string]interface{}:
		result := make(map[string]interface{}, len(val))
		for k, item := range val {
			substituted, err := substituteEnvVarsInObject(item)
			if err != nil {
				return nil, err
			}
			result[k] = substituted
		}
		return result, nil
	default:
		return v, nil
	}
}

// substituteEnvVarsInConfig recursively processes env var substitution for all string values in a config.
func substituteEnvVarsInConfig(config *McpServersConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}

	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	substituted, err := substituteEnvVarsInObject(raw)
	if err != nil {
		return err
	}

	data, err = json.Marshal(substituted)
	if err != nil {
		return err
	}

	return json.Unmarshal(data, config)
}

// --- Config Loading ---

// loadConfig loads and validates the MCP servers config file.
func loadConfig(configPath string) (*McpServersConfig, string, error) {
	path, err := resolveConfigPath(configPath)
	if err != nil {
		return nil, "", err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", configNotFoundError(path)
	}

	var config McpServersConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return nil, path, configInvalidJSONError(path, err.Error())
	}

	if config.McpServers == nil {
		return nil, path, configMissingFieldError(path)
	}

	if len(config.McpServers) == 0 {
		fmt.Fprintln(os.Stderr, "[mcp-cli] Warning: No servers configured in mcpServers. Add server configurations to use MCP tools.")
	}

	// Validate each server config
	for name, server := range config.McpServers {
		if server == nil {
			return nil, path, &CliError{
				Code:    ErrorCodeClientError,
				Type:    "CONFIG_INVALID_SERVER",
				Message: fmt.Sprintf("Invalid server configuration for %q: value is null", name),
				Details: fmt.Sprintf("File: %s", path),
				Suggestion: `Each server must have either "command" (stdio) or "url" (HTTP) field`,
			}
		}
		if server.Command == "" && server.URL == "" {
			return nil, path, &CliError{
				Code:    ErrorCodeClientError,
				Type:    "CONFIG_INVALID_SERVER",
				Message: fmt.Sprintf("Server %q missing required field: must have either \"command\" or \"url\"", name),
				Details: fmt.Sprintf("File: %s", path),
				Suggestion: `Add "command" for stdio servers or "url" for HTTP servers`,
			}
		}
		if server.Command != "" && server.URL != "" {
			return nil, path, &CliError{
				Code:    ErrorCodeClientError,
				Type:    "CONFIG_INVALID_SERVER",
				Message: fmt.Sprintf("Server %q has both \"command\" and \"url\" - pick one", name),
				Details: fmt.Sprintf("File: %s", path),
				Suggestion: `Use "command" for local stdio servers or "url" for remote HTTP servers, not both`,
			}
		}
	}

	// Substitute environment variables
	if err := substituteEnvVarsInConfig(&config); err != nil {
		return nil, path, &CliError{
			Code:       ErrorCodeClientError,
			Type:       "MISSING_ENV_VAR",
			Message:    err.Error(),
			Suggestion: "Set the variable(s) before running mcp-cli, or set MCP_STRICT_ENV=false to allow empty values",
		}
	}

	return &config, path, nil
}

// resolveConfigPath finds the config file path.
func resolveConfigPath(configPath string) (string, error) {
	// Check explicit paths first
	candidates := []string{}

	if configPath != "" {
		candidates = append(candidates, configPath)
	}
	if v := os.Getenv("MCP_CONFIG_PATH"); v != "" {
		candidates = append(candidates, v)
	}

	// Check explicit paths
	for _, p := range candidates {
		absPath, _ := filepath.Abs(p)
		if _, err := os.Stat(absPath); err == nil {
			return absPath, nil
		}
		return "", configNotFoundError(p)
	}

	// Search default locations
	home, _ := os.UserHomeDir()
	searchPaths := []string{
		"./mcp_servers.json",
	}
	if home != "" {
		searchPaths = append(searchPaths,
			filepath.Join(home, ".mcp_servers.json"),
			filepath.Join(home, ".config", "mcp", "mcp_servers.json"),
		)
	}

	for _, p := range searchPaths {
		absPath, _ := filepath.Abs(p)
		if _, err := os.Stat(absPath); err == nil {
			return absPath, nil
		}
	}

	return "", configSearchError()
}

// getServerConfig retrieves a server config by name.
func getServerConfig(config *McpServersConfig, serverName string) (*ServerConfig, error) {
	sc, ok := config.McpServers[serverName]
	if !ok {
		available := listServerNames(config)
		return nil, serverNotFoundError(serverName, available)
	}
	return sc, nil
}

// listServerNames returns sorted server names from config.
func listServerNames(config *McpServersConfig) []string {
	names := make([]string, 0, len(config.McpServers))
	for name := range config.McpServers {
		names = append(names, name)
	}
	return names
}
