package main

import (
	"fmt"
	"strings"
)

// ErrorCode represents CLI exit codes.
const (
	ErrorCodeClientError  = 1 // Invalid arguments, config issues
	ErrorCodeServerError  = 2 // Tool execution failed
	ErrorCodeNetworkError = 3 // Connection failed
	ErrorCodeAuthError    = 4 // Authentication failed
)

// CliError is a structured error for CLI output.
type CliError struct {
	Code       int
	Type       string
	Message    string
	Details    string
	Suggestion string
}

func (e *CliError) Error() string {
	return formatCliError(e)
}

// formatCliError formats a CliError for stderr output.
func formatCliError(e *CliError) string {
	var lines []string
	lines = append(lines, fmt.Sprintf("Error [%s]: %s", e.Type, e.Message))
	if e.Details != "" {
		lines = append(lines, fmt.Sprintf("  Details: %s", e.Details))
	}
	if e.Suggestion != "" {
		lines = append(lines, fmt.Sprintf("  Suggestion: %s", e.Suggestion))
	}
	return strings.Join(lines, "\n")
}

// --- Config Errors ---

func configNotFoundError(path string) *CliError {
	return &CliError{
		Code:       ErrorCodeClientError,
		Type:       "CONFIG_NOT_FOUND",
		Message:    fmt.Sprintf("Config file not found: %s", path),
		Suggestion: `Create mcp_servers.json with: { "mcpServers": { "server-name": { "command": "..." } } }`,
	}
}

func configSearchError() *CliError {
	return &CliError{
		Code:       ErrorCodeClientError,
		Type:       "CONFIG_NOT_FOUND",
		Message:    "No mcp_servers.json found in search paths",
		Details:    "Searched: ./mcp_servers.json, ~/.mcp_servers.json, ~/.config/mcp/mcp_servers.json",
		Suggestion: "Create mcp_servers.json in current directory or use -c/--config to specify path",
	}
}

func configInvalidJSONError(path string, parseError string) *CliError {
	return &CliError{
		Code:       ErrorCodeClientError,
		Type:       "CONFIG_INVALID_JSON",
		Message:    fmt.Sprintf("Invalid JSON in config file: %s", path),
		Details:    parseError,
		Suggestion: "Check for syntax errors: missing commas, unquoted keys, trailing commas",
	}
}

func configMissingFieldError(path string) *CliError {
	return &CliError{
		Code:       ErrorCodeClientError,
		Type:       "CONFIG_MISSING_FIELD",
		Message:    `Config file missing required "mcpServers" object`,
		Details:    fmt.Sprintf("File: %s", path),
		Suggestion: `Config must have structure: { "mcpServers": { ... } }`,
	}
}

// --- Server Errors ---

func serverNotFoundError(serverName string, available []string) *CliError {
	availableList := "(none)"
	if len(available) > 0 {
		availableList = strings.Join(available, ", ")
	}
	suggestion := fmt.Sprintf(`Add server to mcp_servers.json: { "mcpServers": { "%s": { ... } } }`, serverName)
	if len(available) > 0 {
		parts := make([]string, len(available))
		for i, s := range available {
			parts[i] = fmt.Sprintf("mcp-cli %s", s)
		}
		suggestion = "Use one of: " + strings.Join(parts, ", ")
	}
	return &CliError{
		Code:       ErrorCodeClientError,
		Type:       "SERVER_NOT_FOUND",
		Message:    fmt.Sprintf("Server %q not found in config", serverName),
		Details:    fmt.Sprintf("Available servers: %s", availableList),
		Suggestion: suggestion,
	}
}

func serverConnectionError(serverName string, cause string) *CliError {
	suggestion := "Check server configuration and ensure the server process can start"

	if strings.Contains(cause, "not found") || strings.Contains(cause, "executable file not found") {
		suggestion = "Command not found. Install the MCP server: npx -y @modelcontextprotocol/server-<name>"
	} else if strings.Contains(cause, "connection refused") {
		suggestion = "Server refused connection. Check if the server is running and URL is correct"
	} else if strings.Contains(cause, "timeout") {
		suggestion = "Connection timed out. Check network connectivity and server availability"
	} else if strings.Contains(cause, "401") || strings.Contains(cause, "Unauthorized") {
		suggestion = "Authentication required. Add Authorization header to config"
	} else if strings.Contains(cause, "403") || strings.Contains(cause, "Forbidden") {
		suggestion = "Access forbidden. Check credentials and permissions"
	}

	return &CliError{
		Code:       ErrorCodeNetworkError,
		Type:       "SERVER_CONNECTION_FAILED",
		Message:    fmt.Sprintf("Failed to connect to server %q", serverName),
		Details:    cause,
		Suggestion: suggestion,
	}
}

// --- Tool Errors ---

func toolNotFoundError(toolName, serverName string, availableTools []string) *CliError {
	toolList := ""
	moreCount := ""
	if len(availableTools) > 0 {
		limit := 5
		if len(availableTools) < limit {
			limit = len(availableTools)
		}
		toolList = strings.Join(availableTools[:limit], ", ")
		if len(availableTools) > 5 {
			moreCount = fmt.Sprintf(" (+%d more)", len(availableTools)-5)
		}
	}

	details := ""
	if toolList != "" {
		details = fmt.Sprintf("Available tools: %s%s", toolList, moreCount)
	}

	return &CliError{
		Code:       ErrorCodeClientError,
		Type:       "TOOL_NOT_FOUND",
		Message:    fmt.Sprintf("Tool %q not found in server %q", toolName, serverName),
		Details:    details,
		Suggestion: fmt.Sprintf("Run 'mcp-cli %s' to see all available tools", serverName),
	}
}

func toolExecutionError(toolName, serverName, cause string) *CliError {
	suggestion := "Check tool arguments match the expected schema"

	if strings.Contains(cause, "validation") || strings.Contains(cause, "invalid_type") {
		suggestion = fmt.Sprintf("Run 'mcp-cli %s/%s' to see the input schema, then fix arguments", serverName, toolName)
	} else if strings.Contains(cause, "required") {
		suggestion = fmt.Sprintf("Missing required argument. Run 'mcp-cli %s/%s' to see required fields", serverName, toolName)
	} else if strings.Contains(cause, "permission") || strings.Contains(cause, "denied") {
		suggestion = "Permission denied. Check file/resource permissions"
	} else if strings.Contains(cause, "not found") || strings.Contains(cause, "ENOENT") {
		suggestion = "Resource not found. Verify the path or identifier exists"
	}

	return &CliError{
		Code:       ErrorCodeServerError,
		Type:       "TOOL_EXECUTION_FAILED",
		Message:    fmt.Sprintf("Tool %q execution failed", toolName),
		Details:    cause,
		Suggestion: suggestion,
	}
}

// --- Argument Errors ---

func invalidTargetError(target string) *CliError {
	return &CliError{
		Code:       ErrorCodeClientError,
		Type:       "INVALID_TARGET",
		Message:    fmt.Sprintf("Invalid target format: %q", target),
		Details:    "Expected format: server/tool",
		Suggestion: `Use 'mcp-cli call <server> <tool> '{"key":"value"}'' format`,
	}
}

func invalidJSONArgsError(input, parseError string) *CliError {
	truncated := input
	if len(truncated) > 100 {
		truncated = truncated[:100] + "..."
	}
	details := fmt.Sprintf("Input: %s", truncated)
	if parseError != "" {
		details = fmt.Sprintf("Parse error: %s", parseError)
	}
	return &CliError{
		Code:       ErrorCodeClientError,
		Type:       "INVALID_JSON_ARGUMENTS",
		Message:    "Invalid JSON in tool arguments",
		Details:    details,
		Suggestion: `Use valid JSON: '{"path": "./file.txt"}'. Run 'mcp-cli info <server> <tool>' for the schema.`,
	}
}

func toolDisabledError(toolName, serverName string) *CliError {
	return &CliError{
		Code:       ErrorCodeClientError,
		Type:       "TOOL_DISABLED",
		Message:    fmt.Sprintf("Tool %q is disabled by configuration", toolName),
		Details:    fmt.Sprintf("Server %q has allowedTools/disabledTools filtering configured", serverName),
		Suggestion: fmt.Sprintf("Check your mcp_servers.json config. Remove %q from disabledTools or add it to allowedTools.", toolName),
	}
}

func unknownOptionError(option string) *CliError {
	optionLower := strings.ToLower(strings.TrimLeft(option, "-"))
	var suggestion string

	switch {
	case optionLower == "server" || optionLower == "s":
		suggestion = "Server is a positional argument. Use 'mcp-cli info <server>'"
	case optionLower == "tool" || optionLower == "t":
		suggestion = "Tool is a positional argument. Use 'mcp-cli call <server> <tool>'"
	case optionLower == "args" || optionLower == "arguments" || optionLower == "a" || optionLower == "input":
		suggestion = `Pass JSON directly: 'mcp-cli call <server> <tool> '{"key": "value"}''`
	case optionLower == "pattern" || optionLower == "p" || optionLower == "search" || optionLower == "query":
		suggestion = `Use 'mcp-cli grep "*pattern*"'`
	case optionLower == "call" || optionLower == "run" || optionLower == "exec":
		suggestion = "Use 'call' as a subcommand, not option: 'mcp-cli call <server> <tool>'"
	case optionLower == "info" || optionLower == "list" || optionLower == "get":
		suggestion = "Use 'info' as a subcommand, not option: 'mcp-cli info <server>'"
	default:
		suggestion = "Valid options: -c/--config, -d/--with-descriptions"
	}

	return &CliError{
		Code:       ErrorCodeClientError,
		Type:       "UNKNOWN_OPTION",
		Message:    fmt.Sprintf("Unknown option: %s", option),
		Suggestion: suggestion,
	}
}

func missingArgumentError(command, argument string) *CliError {
	suggestion := "Run 'mcp-cli --help' for usage examples"
	switch command {
	case "call":
		suggestion = "Use 'mcp-cli call <server> <tool> '{\"key\": \"value\"}'"
	case "grep":
		suggestion = `Use 'mcp-cli grep "*pattern*"'`
	case "-c/--config":
		suggestion = "Use 'mcp-cli -c /path/to/mcp_servers.json'"
	}
	return &CliError{
		Code:       ErrorCodeClientError,
		Type:       "MISSING_ARGUMENT",
		Message:    fmt.Sprintf("Missing required argument for %s: %s", command, argument),
		Suggestion: suggestion,
	}
}

func tooManyArgumentsError(command string, got, expected int) *CliError {
	return &CliError{
		Code:       ErrorCodeClientError,
		Type:       "TOO_MANY_ARGUMENTS",
		Message:    fmt.Sprintf("Too many arguments for %s", command),
		Details:    fmt.Sprintf("Received %d arguments, maximum is %d", got, expected),
		Suggestion: "Run 'mcp-cli --help' for correct usage",
	}
}

func unknownSubcommandError(subcommand string) *CliError {
	suggestions := map[string]string{
		"run": "call", "execute": "call", "exec": "call", "invoke": "call",
		"list": "info", "ls": "info", "get": "info", "show": "info", "describe": "info",
		"search": "grep", "find": "grep", "query": "grep",
	}

	suggested := suggestions[strings.ToLower(subcommand)]
	validCommands := "info, grep, call"

	suggestion := "Use 'mcp-cli --help' to see available commands"
	if suggested != "" {
		suggestion = fmt.Sprintf("Did you mean 'mcp-cli %s'?", suggested)
	}

	return &CliError{
		Code:       ErrorCodeClientError,
		Type:       "UNKNOWN_SUBCOMMAND",
		Message:    fmt.Sprintf("Unknown subcommand: %q", subcommand),
		Details:    fmt.Sprintf("Valid subcommands: %s", validCommands),
		Suggestion: suggestion,
	}
}

func ambiguousCommandError(serverName, toolName string, hasArgs bool) *CliError {
	cmd := fmt.Sprintf("mcp-cli call %s %s", serverName, toolName)
	if hasArgs {
		cmd += " '<json>'"
	}
	details := fmt.Sprintf("Received: mcp-cli %s %s", serverName, toolName)
	if hasArgs {
		details += " ..."
	}
	return &CliError{
		Code:       ErrorCodeClientError,
		Type:       "AMBIGUOUS_COMMAND",
		Message:    "Ambiguous command: did you mean to call a tool or view info?",
		Details:    details,
		Suggestion: fmt.Sprintf("Use '%s' to execute, or 'mcp-cli info %s %s' to view schema", cmd, serverName, toolName),
	}
}
