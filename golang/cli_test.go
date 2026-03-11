package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var cliBinary string

func TestMain(m *testing.M) {
	// Build the binary once for all integration tests.
	dir, err := os.MkdirTemp("", "mcp-cli-test-*")
	if err != nil {
		panic(err)
	}
	cliBinary = filepath.Join(dir, "mcp-cli")

	cmd := exec.Command("go", "build", "-o", cliBinary, ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		panic("failed to build binary: " + err.Error() + "\n" + string(out))
	}

	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type cliResult struct {
	stdout   string
	stderr   string
	exitCode int
}

// getDummyConfigPath returns a temp config file with a dummy stdio server.
// This allows call commands to pass config loading and reach JSON parsing.
func getDummyConfigPath(t *testing.T) string {
	dir := t.TempDir()
	path := filepath.Join(dir, "mcp_servers.json")
	os.WriteFile(path, []byte(`{"mcpServers":{"filesystem":{"command":"echo"}}}`), 0644)
	return path
}

func runCli(args ...string) cliResult {
	cmd := exec.Command(cliBinary, args...)
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			exitCode = 1
		}
	}

	return cliResult{
		stdout:   stdout.String(),
		stderr:   stderr.String(),
		exitCode: exitCode,
	}
}

// --- Ambiguous command errors ---

func TestCli_AmbiguousServerTool(t *testing.T) {
	r := runCli("someserver", "sometool")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "AMBIGUOUS_COMMAND")
	assertContains(t, r.stderr, "call")
	assertContains(t, r.stderr, "info")
}

func TestCli_AmbiguousServerToolJSON(t *testing.T) {
	r := runCli("someserver", "sometool", "{}")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "AMBIGUOUS_COMMAND")
}

// --- Unknown subcommand errors ---

func TestCli_UnknownSubcommand_Run(t *testing.T) {
	r := runCli("run", "server", "tool")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "UNKNOWN_SUBCOMMAND")
	assertContains(t, r.stderr, "call")
}

func TestCli_UnknownSubcommand_Execute(t *testing.T) {
	r := runCli("execute", "server/tool")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "call")
}

func TestCli_UnknownSubcommand_Get(t *testing.T) {
	r := runCli("get", "server")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "info")
}

func TestCli_UnknownSubcommand_List(t *testing.T) {
	r := runCli("list")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "info")
}

func TestCli_UnknownSubcommand_Search(t *testing.T) {
	r := runCli("search", "*file*")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "grep")
}

func TestCli_UnknownSubcommand_Find(t *testing.T) {
	r := runCli("find", "*file*")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "grep")
}

// --- Missing argument errors ---

func TestCli_CallNoArgs(t *testing.T) {
	r := runCli("call")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "MISSING_ARGUMENT")
	assertContains(t, r.stderr, "server")
}

func TestCli_CallServerNoTool(t *testing.T) {
	r := runCli("call", "server")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "MISSING_ARGUMENT")
	assertContains(t, r.stderr, "tool")
}

func TestCli_GrepNoPattern(t *testing.T) {
	r := runCli("grep")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "MISSING_ARGUMENT")
	assertContains(t, r.stderr, "pattern")
}

// --- Unknown option errors ---

func TestCli_UnknownOption_Server(t *testing.T) {
	r := runCli("info", "--server", "fs")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "UNKNOWN_OPTION")
}

func TestCli_UnknownOption_Args(t *testing.T) {
	r := runCli("call", "server", "tool", "--args", "{}")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "UNKNOWN_OPTION")
}

func TestCli_UnknownOption_DashCall(t *testing.T) {
	r := runCli("--call", "server", "tool")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "UNKNOWN_OPTION")
}

func TestCli_MissingConfigPath(t *testing.T) {
	r := runCli("-c")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "MISSING_ARGUMENT")
}

// --- Help and version ---

func TestCli_Help(t *testing.T) {
	r := runCli("--help")
	assertEqual(t, r.exitCode, 0)
	assertContains(t, r.stdout, "info")
	assertContains(t, r.stdout, "grep")
	assertContains(t, r.stdout, "call")
}

func TestCli_HelpShort(t *testing.T) {
	r := runCli("-h")
	assertEqual(t, r.exitCode, 0)
}

func TestCli_Version(t *testing.T) {
	r := runCli("--version")
	assertEqual(t, r.exitCode, 0)
	assertContains(t, strings.TrimSpace(r.stdout), Version)
}

// --- Invalid JSON arguments ---

func TestCli_InvalidJSON_UnquotedKeys(t *testing.T) {
	cfg := getDummyConfigPath(t)
	r := runCli("-c", cfg, "call", "filesystem", "read_file", `{path:"test"}`)
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "Invalid JSON")
}

func TestCli_InvalidJSON_KeyValue(t *testing.T) {
	cfg := getDummyConfigPath(t)
	r := runCli("-c", cfg, "call", "filesystem", "read_file", "path=./README.md")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "Invalid JSON")
}

func TestCli_InvalidJSON_UnquotedValue(t *testing.T) {
	cfg := getDummyConfigPath(t)
	r := runCli("-c", cfg, "call", "filesystem", "read_file", `{"path": test}`)
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "Invalid JSON")
}

func TestCli_InvalidJSON_TrailingComma(t *testing.T) {
	cfg := getDummyConfigPath(t)
	r := runCli("-c", cfg, "call", "filesystem", "read_file", `{"path": "test",}`)
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "Invalid JSON")
}

func TestCli_InvalidJSON_SingleQuotes(t *testing.T) {
	cfg := getDummyConfigPath(t)
	r := runCli("-c", cfg, "call", "filesystem", "read_file", `{'path': 'test'}`)
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "Invalid JSON")
}

func TestCli_InvalidJSON_PlainText(t *testing.T) {
	cfg := getDummyConfigPath(t)
	r := runCli("-c", cfg, "call", "filesystem", "read_file", "just plain text")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "Invalid JSON")
}

// --- Slash format without subcommand ---

func TestCli_SlashFormatAmbiguous(t *testing.T) {
	r := runCli("server/tool", "{}")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "AMBIGUOUS_COMMAND")
}

// --- Valid commands parse correctly ---

func TestCli_InfoParsesCorrectly(t *testing.T) {
	r := runCli("info")
	// Should not error on parsing (will error on missing server arg)
	assertNotContains(t, r.stderr, "AMBIGUOUS_COMMAND")
	assertNotContains(t, r.stderr, "UNKNOWN_SUBCOMMAND")
}

func TestCli_GrepParsesCorrectly(t *testing.T) {
	r := runCli("grep", "*")
	assertNotContains(t, r.stderr, "UNKNOWN_SUBCOMMAND")
}

func TestCli_CallSlashFormatParsesCorrectly(t *testing.T) {
	r := runCli("call", "server/tool", "{}")
	// Will fail on server connection, but should not error on parsing
	assertNotContains(t, r.stderr, "AMBIGUOUS_COMMAND")
}

// --- Malformed target paths ---

func TestCli_TrailingSlash(t *testing.T) {
	r := runCli("call", "filesystem/")
	assertEqual(t, r.exitCode, 1)
	assertContains(t, r.stderr, "MISSING_ARGUMENT")
}
