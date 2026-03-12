package main

import (
	"strings"
	"testing"
)

func TestFormatCliError_AllFields(t *testing.T) {
	err := &CliError{
		Code:       ErrorCodeClientError,
		Type:       "TEST_ERROR",
		Message:    "Something went wrong",
		Details:    "More info here",
		Suggestion: "Try this fix",
	}
	output := formatCliError(err)
	assertContains(t, output, "Error [TEST_ERROR]")
	assertContains(t, output, "Something went wrong")
	assertContains(t, output, "Details: More info here")
	assertContains(t, output, "Suggestion: Try this fix")
}

func TestFormatCliError_WithoutOptionalFields(t *testing.T) {
	err := &CliError{
		Code:    ErrorCodeClientError,
		Type:    "SIMPLE_ERROR",
		Message: "Basic error",
	}
	output := formatCliError(err)
	assertContains(t, output, "Error [SIMPLE_ERROR]")
	assertContains(t, output, "Basic error")
	assertNotContains(t, output, "Details:")
	assertNotContains(t, output, "Suggestion:")
}

// --- Config Errors ---

func TestConfigNotFoundError(t *testing.T) {
	err := configNotFoundError("/path/to/config.json")
	assertEqual(t, err.Type, "CONFIG_NOT_FOUND")
	assertContains(t, err.Message, "/path/to/config.json")
	assertNotEmpty(t, err.Suggestion)
}

func TestConfigSearchError(t *testing.T) {
	err := configSearchError()
	assertEqual(t, err.Type, "CONFIG_NOT_FOUND")
	assertContains(t, err.Details, "Searched:")
	assertContains(t, err.Suggestion, "mcp_servers.json")
}

func TestConfigInvalidJSONError(t *testing.T) {
	err := configInvalidJSONError("/config.json", "Unexpected token")
	assertEqual(t, err.Type, "CONFIG_INVALID_JSON")
	assertContains(t, err.Details, "Unexpected token")
}

func TestConfigMissingFieldError(t *testing.T) {
	err := configMissingFieldError("/config.json")
	assertEqual(t, err.Type, "CONFIG_MISSING_FIELD")
	assertContains(t, err.Message, "mcpServers")
}

// --- Server Errors ---

func TestServerNotFoundError_WithAvailable(t *testing.T) {
	err := serverNotFoundError("unknown", []string{"github", "filesystem"})
	assertEqual(t, err.Type, "SERVER_NOT_FOUND")
	assertContains(t, err.Message, "unknown")
	assertContains(t, err.Details, "github")
	assertContains(t, err.Details, "filesystem")
	assertContains(t, err.Suggestion, "mcp-cli github")
}

func TestServerNotFoundError_EmptyList(t *testing.T) {
	err := serverNotFoundError("unknown", []string{})
	assertContains(t, err.Details, "(none)")
	assertContains(t, err.Suggestion, "Add server to")
}

func TestServerConnectionError_CommandNotFound(t *testing.T) {
	err := serverConnectionError("github", "executable file not found")
	assertEqual(t, err.Type, "SERVER_CONNECTION_FAILED")
	assertContains(t, err.Suggestion, "Install")
}

func TestServerConnectionError_ConnectionRefused(t *testing.T) {
	err := serverConnectionError("remote", "connection refused")
	assertContains(t, err.Suggestion, "Check if the server is running")
}

func TestServerConnectionError_Timeout(t *testing.T) {
	err := serverConnectionError("remote", "timeout")
	assertContains(t, err.Suggestion, "network connectivity")
}

func TestServerConnectionError_Unauthorized(t *testing.T) {
	err := serverConnectionError("remote", "401 Unauthorized")
	assertContains(t, err.Suggestion, "Authorization header")
}

// --- Tool Errors ---

func TestToolNotFoundError_WithAvailable(t *testing.T) {
	err := toolNotFoundError("unknown", "github", []string{"search", "clone"})
	assertEqual(t, err.Type, "TOOL_NOT_FOUND")
	assertContains(t, err.Message, "unknown")
	assertContains(t, err.Message, "github")
	assertContains(t, err.Details, "search")
	assertContains(t, err.Suggestion, "mcp-cli github")
}

func TestToolNotFoundError_TruncatesLongList(t *testing.T) {
	tools := []string{"t1", "t2", "t3", "t4", "t5", "t6", "t7", "t8"}
	err := toolNotFoundError("x", "server", tools)
	assertContains(t, err.Details, "+3 more")
}

func TestToolExecutionError_Validation(t *testing.T) {
	err := toolExecutionError("search", "github", "validation failed")
	assertEqual(t, err.Type, "TOOL_EXECUTION_FAILED")
	assertContains(t, err.Suggestion, "input schema")
}

func TestToolExecutionError_MissingRequired(t *testing.T) {
	err := toolExecutionError("search", "github", "required field missing")
	assertContains(t, err.Suggestion, "required")
}

func TestToolExecutionError_PermissionDenied(t *testing.T) {
	err := toolExecutionError("read", "fs", "permission denied")
	assertContains(t, err.Suggestion, "permissions")
}

// --- Argument Errors ---

func TestInvalidTargetError(t *testing.T) {
	err := invalidTargetError("badformat")
	assertEqual(t, err.Type, "INVALID_TARGET")
	assertContains(t, err.Details, "server/tool")
}

func TestInvalidJSONArgsError_TruncatesLongInput(t *testing.T) {
	longInput := strings.Repeat("x", 200)
	err := invalidJSONArgsError(longInput, "")
	assertEqual(t, err.Type, "INVALID_JSON_ARGUMENTS")
	if len(err.Details) >= 150 {
		t.Errorf("expected Details < 150 chars, got %d", len(err.Details))
	}
	assertContains(t, err.Details, "...")
}

func TestInvalidJSONArgsError_WithParseError(t *testing.T) {
	err := invalidJSONArgsError("{invalid}", "Unexpected token")
	assertContains(t, err.Details, "Unexpected token")
}

func TestUnknownOptionError(t *testing.T) {
	err := unknownOptionError("--bad")
	assertEqual(t, err.Type, "UNKNOWN_OPTION")
	assertContains(t, err.Message, "--bad")
}

func TestMissingArgumentError(t *testing.T) {
	err := missingArgumentError("grep", "pattern")
	assertEqual(t, err.Type, "MISSING_ARGUMENT")
	assertContains(t, err.Message, "grep")
	assertContains(t, err.Message, "pattern")
}

// --- Error Codes ---

func TestErrorCodes(t *testing.T) {
	assertEqual(t, ErrorCodeClientError, 1)
	assertEqual(t, ErrorCodeServerError, 2)
	assertEqual(t, ErrorCodeNetworkError, 3)
	assertEqual(t, ErrorCodeAuthError, 4)
}

// --- Subcommand Errors ---

func TestAmbiguousCommandError(t *testing.T) {
	err := ambiguousCommandError("server", "tool", false)
	assertEqual(t, err.Type, "AMBIGUOUS_COMMAND")
	assertContains(t, err.Message, "did you mean to call a tool or view info")
	assertContains(t, err.Details, "Received: mcp-cli server tool")
	assertContains(t, err.Suggestion, "call server tool")
	assertContains(t, err.Suggestion, "info server tool")
}

func TestAmbiguousCommandError_WithArgs(t *testing.T) {
	err := ambiguousCommandError("server", "tool", true)
	assertContains(t, err.Details, "...")
	assertContains(t, err.Suggestion, "<json>")
}

func TestUnknownSubcommandError(t *testing.T) {
	err := unknownSubcommandError("run")
	assertEqual(t, err.Type, "UNKNOWN_SUBCOMMAND")
	assertContains(t, err.Details, "Valid subcommands:")
	assertContains(t, err.Suggestion, "Did you mean")
	assertContains(t, err.Suggestion, "call")
}

func TestUnknownSubcommandError_Aliases(t *testing.T) {
	tests := map[string]string{
		"run": "call", "execute": "call", "exec": "call", "invoke": "call",
		"list": "info", "ls": "info", "get": "info", "show": "info", "describe": "info",
		"search": "grep", "find": "grep", "query": "grep",
	}
	for alias, expected := range tests {
		err := unknownSubcommandError(alias)
		assertContains(t, err.Suggestion, expected)
	}
}

func TestUnknownSubcommandError_NoAlias(t *testing.T) {
	err := unknownSubcommandError("foobar")
	assertContains(t, err.Suggestion, "mcp-cli --help")
}

func TestUnknownOptionError_SmartSuggestions(t *testing.T) {
	err := unknownOptionError("--server")
	assertContains(t, err.Suggestion, "positional argument")

	err = unknownOptionError("--tool")
	assertContains(t, err.Suggestion, "positional argument")

	err = unknownOptionError("--args")
	assertContains(t, err.Suggestion, "JSON directly")

	err = unknownOptionError("--pattern")
	assertContains(t, err.Suggestion, "grep")

	err = unknownOptionError("--call")
	assertContains(t, err.Suggestion, "subcommand")

	err = unknownOptionError("--info")
	assertContains(t, err.Suggestion, "subcommand")
}

func TestToolDisabledError(t *testing.T) {
	err := toolDisabledError("read_file", "filesystem")
	assertEqual(t, err.Type, "TOOL_DISABLED")
	assertContains(t, err.Message, "read_file")
	assertContains(t, err.Details, "filesystem")
	assertContains(t, err.Suggestion, "allowedTools")
}

func TestTooManyArgumentsError(t *testing.T) {
	err := tooManyArgumentsError("grep", 5, 1)
	assertEqual(t, err.Type, "TOO_MANY_ARGUMENTS")
	assertContains(t, err.Message, "grep")
	assertContains(t, err.Details, "5")
	assertContains(t, err.Details, "1")
}

// --- Test helpers ---

func assertContains(t *testing.T, s, substr string) {
	t.Helper()
	if !strings.Contains(s, substr) {
		t.Errorf("expected %q to contain %q", s, substr)
	}
}

func assertNotContains(t *testing.T, s, substr string) {
	t.Helper()
	if strings.Contains(s, substr) {
		t.Errorf("expected %q to NOT contain %q", s, substr)
	}
}

func assertEqual(t *testing.T, got, want interface{}) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

func assertNotEmpty(t *testing.T, s string) {
	t.Helper()
	if s == "" {
		t.Error("expected non-empty string")
	}
}
