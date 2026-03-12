package main

import (
	"fmt"
	"os"
	"os/signal"
	"regexp"
	"strings"
	"syscall"
)

// Known subcommands.
var knownSubcommands = map[string]bool{
	"info": true,
	"grep": true,
	"call": true,
}

// Possible subcommand aliases users might try.
var possibleSubcommands = map[string]bool{
	"run": true, "execute": true, "exec": true, "invoke": true,
	"list": true, "ls": true, "get": true, "show": true,
	"describe": true, "search": true, "find": true, "query": true,
}

// ParsedArgs holds the parsed CLI arguments.
type ParsedArgs struct {
	Command          string // list, info, grep, call, help, version
	Server           string
	Tool             string
	Pattern          string
	Args             string
	WithDescriptions bool
	ConfigPath       string
}

// parseServerTool parses "server/tool" or "server tool" format.
func parseServerTool(args []string) (server, tool string) {
	if len(args) == 0 {
		return "", ""
	}

	first := args[0]

	// Check for slash format: server/tool
	if idx := strings.Index(first, "/"); idx >= 0 {
		return first[:idx], first[idx+1:]
	}

	// Space format: server tool
	server = first
	if len(args) > 1 {
		tool = args[1]
	}
	return
}

// parseArgs parses command line arguments.
func parseArgs(args []string) ParsedArgs {
	result := ParsedArgs{
		Command: "info",
	}

	var positional []string

	for i := 0; i < len(args); i++ {
		arg := args[i]

		switch arg {
		case "-h", "--help":
			result.Command = "help"
			return result
		case "-v", "--version":
			result.Command = "version"
			return result
		case "-d", "--with-descriptions":
			result.WithDescriptions = true
		case "-c", "--config":
			i++
			if i >= len(args) {
				fmt.Fprintln(os.Stderr, formatCliError(missingArgumentError("-c/--config", "path")))
				os.Exit(ErrorCodeClientError)
			}
			result.ConfigPath = args[i]
		default:
			if strings.HasPrefix(arg, "-") && arg != "-" {
				fmt.Fprintln(os.Stderr, formatCliError(unknownOptionError(arg)))
				os.Exit(ErrorCodeClientError)
			}
			positional = append(positional, arg)
		}
	}

	// No positional args = list all servers
	if len(positional) == 0 {
		result.Command = "list"
		return result
	}

	firstArg := positional[0]

	// --- Explicit subcommand routing ---

	if firstArg == "info" {
		result.Command = "info"
		remaining := positional[1:]
		server, tool := parseServerTool(remaining)

		if server == "" {
			// Try to show available servers in error
			var availableServers []string
			if cfg, _, err := loadConfig(result.ConfigPath); err == nil {
				availableServers = listServerNames(cfg)
			}
			serverList := "(none found)"
			if len(availableServers) > 0 {
				serverList = strings.Join(availableServers, ", ")
			}
			fmt.Fprintf(os.Stderr, "Error [MISSING_ARGUMENT]: Missing required argument for info: server\n")
			fmt.Fprintf(os.Stderr, "  Available servers: %s\n", serverList)
			fmt.Fprintf(os.Stderr, "  Suggestion: Use 'mcp-cli info <server>' to see server details, or just 'mcp-cli' to list all\n")
			os.Exit(ErrorCodeClientError)
		}

		result.Server = server
		result.Tool = tool
		return result
	}

	if firstArg == "grep" {
		result.Command = "grep"
		if len(positional) < 2 {
			fmt.Fprintln(os.Stderr, formatCliError(missingArgumentError("grep", "pattern")))
			os.Exit(ErrorCodeClientError)
		}
		result.Pattern = positional[1]
		if len(positional) > 2 {
			fmt.Fprintln(os.Stderr, formatCliError(tooManyArgumentsError("grep", len(positional)-1, 1)))
			os.Exit(ErrorCodeClientError)
		}
		return result
	}

	if firstArg == "call" {
		result.Command = "call"
		remaining := positional[1:]

		if len(remaining) == 0 {
			fmt.Fprintln(os.Stderr, formatCliError(missingArgumentError("call", "server and tool")))
			os.Exit(ErrorCodeClientError)
		}

		server, tool := parseServerTool(remaining)
		result.Server = server

		if tool == "" {
			// Slash format without tool, or only server provided
			if strings.Contains(remaining[0], "/") && !strings.Contains(remaining[0][strings.Index(remaining[0], "/")+1:], "") {
				fmt.Fprintln(os.Stderr, formatCliError(missingArgumentError("call", "tool")))
				os.Exit(ErrorCodeClientError)
			}
			if len(remaining) < 2 {
				fmt.Fprintln(os.Stderr, formatCliError(missingArgumentError("call", "tool")))
				os.Exit(ErrorCodeClientError)
			}
		}

		result.Tool = tool

		// Determine where args start
		argsStartIndex := 2
		if strings.Contains(remaining[0], "/") {
			argsStartIndex = 1
		}

		if argsStartIndex < len(remaining) {
			jsonArgs := remaining[argsStartIndex:]
			argsValue := strings.Join(jsonArgs, " ")
			if argsValue != "-" {
				result.Args = argsValue
			}
		}

		return result
	}

	// --- Check for unknown subcommand ---

	if possibleSubcommands[strings.ToLower(firstArg)] {
		fmt.Fprintln(os.Stderr, formatCliError(unknownSubcommandError(firstArg)))
		os.Exit(ErrorCodeClientError)
	}

	// --- Slash format without subcommand → error ---

	if strings.Contains(firstArg, "/") {
		parts := strings.SplitN(firstArg, "/", 2)
		serverName := parts[0]
		toolName := ""
		if len(parts) > 1 {
			toolName = parts[1]
		}
		hasArgs := len(positional) > 1
		fmt.Fprintln(os.Stderr, formatCliError(ambiguousCommandError(serverName, toolName, hasArgs)))
		os.Exit(ErrorCodeClientError)
	}

	// --- Ambiguous command detection ---

	if len(positional) >= 2 {
		serverName := positional[0]
		possibleTool := positional[1]

		looksLikeJSON := strings.HasPrefix(possibleTool, "{") || strings.HasPrefix(possibleTool, "[")
		looksLikeToolName := regexp.MustCompile(`^[a-zA-Z_][a-zA-Z0-9_-]*$`).MatchString(possibleTool)

		if !looksLikeJSON && looksLikeToolName {
			hasArgs := len(positional) > 2
			fmt.Fprintln(os.Stderr, formatCliError(ambiguousCommandError(serverName, possibleTool, hasArgs)))
			os.Exit(ErrorCodeClientError)
		}
	}

	// --- Default: single server name → info ---

	result.Command = "info"
	result.Server = firstArg
	return result
}

// printHelp prints the help message.
func printHelp() {
	fmt.Printf(`
mcp-cli v%s - A lightweight CLI for MCP servers

Usage:
  mcp-cli [options]                             List all servers and tools
  mcp-cli [options] info <server>               Show server details
  mcp-cli [options] info <server> <tool>        Show tool schema
  mcp-cli [options] grep <pattern>              Search tools by glob pattern
  mcp-cli [options] call <server> <tool>        Call tool (reads JSON from stdin if no args)
  mcp-cli [options] call <server> <tool> <json> Call tool with JSON arguments

Formats (both work):
  mcp-cli info server tool      Space-separated
  mcp-cli info server/tool      Slash-separated
  mcp-cli call server tool '{}'  Space-separated
  mcp-cli call server/tool '{}'  Slash-separated

Options:
  -h, --help               Show this help message
  -v, --version            Show version number
  -d, --with-descriptions  Include tool descriptions
  -c, --config <path>      Path to mcp_servers.json config file

Output:
  mcp-cli/info/grep  Human-readable text to stdout
  call               Raw JSON to stdout (for piping)
  Errors             Always to stderr

Examples:
  mcp-cli                                        # List all servers
  mcp-cli -d                                     # List with descriptions
  mcp-cli grep "*file*"                          # Search for file tools
  mcp-cli info filesystem                        # Show server tools
  mcp-cli info filesystem read_file              # Show tool schema
  mcp-cli call filesystem read_file '{}'         # Call tool
  cat input.json | mcp-cli call server tool      # Read from stdin

Environment Variables:
  MCP_NO_DAEMON=1        Disable connection caching (force fresh connections)
  MCP_DAEMON_TIMEOUT=N   Set daemon idle timeout in seconds (default: 60)
  MCP_CONFIG_PATH        Path to config file
  MCP_DEBUG              Enable debug output (default: false)
  MCP_TIMEOUT            Request timeout in seconds (default: 1800)
  MCP_CONCURRENCY        Max parallel connections (default: 5)
  MCP_MAX_RETRIES        Max retry attempts (default: 3)
  MCP_RETRY_DELAY        Base retry delay in ms (default: 1000)
  MCP_STRICT_ENV         Error on missing env vars (default: true)
  NO_COLOR               Disable colored output
`, Version)
}

func main() {
	// Handle daemon mode (--daemon flag from spawned daemon process)
	args := os.Args[1:]
	if len(args) >= 3 && args[0] == "--daemon" {
		daemonMain(args[1], args[2])
		return
	}

	// Handle graceful shutdown on SIGINT/SIGTERM
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		sig := <-sigCh
		switch sig {
		case syscall.SIGINT:
			os.Exit(130) // 128 + SIGINT(2)
		case syscall.SIGTERM:
			os.Exit(143) // 128 + SIGTERM(15)
		}
	}()

	parsed := parseArgs(os.Args[1:])

	switch parsed.Command {
	case "help":
		printHelp()
	case "version":
		fmt.Printf("mcp-cli v%s\n", Version)
	case "list":
		listCommand(parsed.ConfigPath, parsed.WithDescriptions)
	case "info":
		infoCommand(parsed.Server, parsed.Tool, parsed.ConfigPath, parsed.WithDescriptions)
	case "grep":
		grepCommand(parsed.Pattern, parsed.ConfigPath, parsed.WithDescriptions)
	case "call":
		callCommand(parsed.Server, parsed.Tool, parsed.Args, parsed.ConfigPath)
	default:
		printHelp()
		os.Exit(ErrorCodeClientError)
	}
}
