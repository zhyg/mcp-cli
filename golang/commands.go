package main

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// --- List Command ---

// listCommand lists all servers and their tools.
func listCommand(configPath string, withDescriptions bool) {
	config, _, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(ErrorCodeClientError)
	}

	serverNames := listServerNames(config)
	if len(serverNames) == 0 {
		fmt.Fprintln(os.Stderr, "Warning: No servers configured. Add servers to mcp_servers.json")
		return
	}

	sort.Strings(serverNames)
	concurrency := getConcurrencyLimit()

	debugLog("Processing %d servers with concurrency %d", len(serverNames), concurrency)

	servers := fetchAllServerTools(serverNames, config, concurrency)

	// Sort by name
	sort.Slice(servers, func(i, j int) bool {
	return servers[i].name < servers[j].name
	})

	// Convert errors to display format
	var displayServers []ServerWithTools
	for _, s := range servers {
		swt := ServerWithTools{
			Name:         s.name,
			Tools:        s.tools,
			Instructions: s.instructions,
		}
		if s.err != "" {
			swt.Tools = []ToolInfo{{Name: fmt.Sprintf("<error: %s>", s.err)}}
		}
		displayServers = append(displayServers, swt)
	}

	fmt.Println(formatServerList(displayServers, withDescriptions))
}

type serverResult struct {
	name         string
	tools        []ToolInfo
	instructions string
	err          string
}

// fetchAllServerTools fetches tools from all servers with concurrency limit.
func fetchAllServerTools(serverNames []string, config *McpServersConfig, concurrency int) []serverResult {
	results := make([]serverResult, len(serverNames))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, name := range serverNames {
		wg.Add(1)
		go func(idx int, serverName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			results[idx] = fetchServerTools(serverName, config)
		}(i, name)
	}

	wg.Wait()
	return results
}

// fetchServerTools fetches tools from a single server.
func fetchServerTools(serverName string, config *McpServersConfig) serverResult {
	serverConfig, err := getServerConfig(config, serverName)
	if err != nil {
		return serverResult{name: serverName, err: err.Error()}
	}

	conn, err := getUnifiedConnection(serverName, serverConfig)
	if err != nil {
		debugLog("%s: connection failed - %s", serverName, err.Error())
		return serverResult{name: serverName, err: err.Error()}
	}
	defer conn.Close()

	tools, err := conn.ListTools()
	if err != nil {
		return serverResult{name: serverName, err: err.Error()}
	}

	// Apply tool filtering
	tools = filterTools(tools, serverConfig)

	instructions := conn.GetInstructions()
	debugLog("%s: loaded %d tools", serverName, len(tools))

	return serverResult{
		name:         serverName,
		tools:        tools,
		instructions: instructions,
	}
}

// --- Info Command ---

// infoCommand shows server or tool details.
func infoCommand(serverName, toolName, configPath string, withDescriptions bool) {
	config, _, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(ErrorCodeClientError)
	}

	serverConfig, err := getServerConfig(config, serverName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(ErrorCodeClientError)
	}

	conn, err := getUnifiedConnection(serverName, serverConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, formatCliError(serverConnectionError(serverName, err.Error())))
		os.Exit(ErrorCodeNetworkError)
	}
	defer conn.Close()

	tools, err := conn.ListTools()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Failed to list tools: %s\n", err.Error())
		os.Exit(ErrorCodeServerError)
	}

	// Apply tool filtering
	tools = filterTools(tools, serverConfig)

	if toolName != "" {
		// Show specific tool schema
		var tool *ToolInfo
		for i := range tools {
			if tools[i].Name == toolName {
				tool = &tools[i]
				break
			}
		}

		if tool == nil {
			var available []string
			for _, t := range tools {
				available = append(available, t.Name)
			}
			fmt.Fprintln(os.Stderr, formatCliError(toolNotFoundError(toolName, serverName, available)))
			os.Exit(ErrorCodeClientError)
		}

		fmt.Println(formatToolSchema(serverName, *tool))
	} else {
		// Show server details
		instructions := conn.GetInstructions()
		fmt.Println(formatServerDetails(serverName, serverConfig, tools, withDescriptions, instructions))
	}
}

// --- Grep Command ---

// globToRegex converts a glob pattern to a regex.
func globToRegex(pattern string) *regexp.Regexp {
	var escaped strings.Builder
	i := 0

	for i < len(pattern) {
		ch := pattern[i]

		if ch == '*' && i+1 < len(pattern) && pattern[i+1] == '*' {
			// ** (globstar) - match anything including slashes
			escaped.WriteString(".*")
			i += 2
			for i < len(pattern) && pattern[i] == '*' {
				i++
			}
		} else if ch == '*' {
			// * - match any chars except slash
			escaped.WriteString("[^/]*")
			i++
		} else if ch == '?' {
			// ? - match single char (not slash)
			escaped.WriteString("[^/]")
			i++
		} else if strings.ContainsRune("[.+^${}()|\\]", rune(ch)) {
			escaped.WriteByte('\\')
			escaped.WriteByte(ch)
			i++
		} else {
			escaped.WriteByte(ch)
			i++
		}
	}

	re, err := regexp.Compile("(?i)^" + escaped.String() + "$")
	if err != nil {
		// Fallback to literal match
		re = regexp.MustCompile("(?i)^" + regexp.QuoteMeta(pattern) + "$")
	}
	return re
}

// grepCommand searches tools by pattern across all servers.
func grepCommand(pattern, configPath string, withDescriptions bool) {
	config, _, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(ErrorCodeClientError)
	}

	re := globToRegex(pattern)
	serverNames := listServerNames(config)

	if len(serverNames) == 0 {
		fmt.Fprintln(os.Stderr, "Warning: No servers configured. Add servers to mcp_servers.json")
		return
	}

	concurrency := getConcurrencyLimit()
	debugLog("Searching %d servers for pattern %q (concurrency: %d)", len(serverNames), pattern, concurrency)

	// Search all servers in parallel
	type searchResult struct {
		serverName string
		results    []SearchResult
		err        string
	}

	allSearchResults := make([]searchResult, len(serverNames))
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i, name := range serverNames {
		wg.Add(1)
		go func(idx int, serverName string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			serverConfig, err := getServerConfig(config, serverName)
			if err != nil {
				allSearchResults[idx] = searchResult{serverName: serverName, err: err.Error()}
				return
			}

			conn, err := getUnifiedConnection(serverName, serverConfig)
			if err != nil {
				debugLog("%s: connection failed - %s", serverName, err.Error())
				allSearchResults[idx] = searchResult{serverName: serverName, err: err.Error()}
				return
			}
			defer conn.Close()

			tools, err := conn.ListTools()
			if err != nil {
				allSearchResults[idx] = searchResult{serverName: serverName, err: err.Error()}
				return
			}

			// Apply tool filtering
			tools = filterTools(tools, serverConfig)

			var results []SearchResult
			for _, tool := range tools {
				if re.MatchString(tool.Name) {
					results = append(results, SearchResult{Server: serverName, Tool: tool})
				}
			}

			debugLog("%s: found %d matches", serverName, len(results))
			allSearchResults[idx] = searchResult{serverName: serverName, results: results}
		}(i, name)
	}

	wg.Wait()

	// Collect results
	var allResults []SearchResult
	var failedServers []string
	for _, sr := range allSearchResults {
		allResults = append(allResults, sr.results...)
		if sr.err != "" {
			failedServers = append(failedServers, sr.serverName)
		}
	}

	if len(failedServers) > 0 {
		fmt.Fprintf(os.Stderr, "Warning: %d server(s) failed to connect: %s\n",
			len(failedServers), strings.Join(failedServers, ", "))
	}

	if len(allResults) == 0 {
		fmt.Printf("No tools found matching %q\n", pattern)
		fmt.Println("  Tip: Pattern matches tool names only (not server names)")
		fmt.Println("  Tip: Use '*' for wildcards, e.g. '*file*' or 'read_*'")
		fmt.Println("  Tip: Run 'mcp-cli' to list all available tools")
		return
	}

	fmt.Println(formatSearchResults(allResults, withDescriptions))
}

// --- Call Command ---

// callCommand executes a tool call.
func callCommand(serverName, toolName, argsStr, configPath string) {
	config, _, err := loadConfig(configPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(ErrorCodeClientError)
	}

	serverConfig, err := getServerConfig(config, serverName)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(ErrorCodeClientError)
	}

	// Parse arguments
	args, err := parseCallArgs(argsStr)
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		os.Exit(ErrorCodeClientError)
	}

	// Check if tool is allowed before calling
	if !isToolAllowed(toolName, serverConfig) {
		fmt.Fprintln(os.Stderr, formatCliError(toolDisabledError(toolName, serverName)))
		os.Exit(ErrorCodeClientError)
	}

	conn, err := getUnifiedConnection(serverName, serverConfig)
	if err != nil {
		fmt.Fprintln(os.Stderr, formatCliError(serverConnectionError(serverName, err.Error())))
		os.Exit(ErrorCodeNetworkError)
	}
	defer conn.Close()

	result, err := conn.CallTool(toolName, args)
	if err != nil {
		errMsg := err.Error()

		// Try to get available tools for better error message
		if strings.Contains(errMsg, "not found") || strings.Contains(errMsg, "unknown tool") {
			var available []string
			if tools, listErr := conn.ListTools(); listErr == nil {
				for _, t := range tools {
					available = append(available, t.Name)
				}
			}
			fmt.Fprintln(os.Stderr, formatCliError(toolNotFoundError(toolName, serverName, available)))
		} else {
			fmt.Fprintln(os.Stderr, formatCliError(toolExecutionError(toolName, serverName, errMsg)))
		}
		os.Exit(ErrorCodeServerError)
	}

	fmt.Println(formatToolResult(result))
}

// parseCallArgs parses JSON arguments from string or stdin.
func parseCallArgs(argsStr string) (map[string]interface{}, error) {
	var jsonString string

	if argsStr != "" {
		jsonString = argsStr
	} else {
		// Check if stdin has data (not a terminal)
		stat, _ := os.Stdin.Stat()
		if (stat.Mode() & os.ModeCharDevice) == 0 {
			timeoutMs := getTimeoutMs()
			type readResult struct {
				data []byte
				err  error
			}
			ch := make(chan readResult, 1)
			go func() {
				data, err := io.ReadAll(os.Stdin)
				ch <- readResult{data, err}
			}()
			select {
			case result := <-ch:
				if result.err != nil {
					return nil, fmt.Errorf("failed to read from stdin: %w", result.err)
				}
				jsonString = strings.TrimSpace(string(result.data))
			case <-time.After(time.Duration(timeoutMs) * time.Millisecond):
				return nil, fmt.Errorf("stdin read timed out after %dms", timeoutMs)
			}
		}
	}

	if jsonString == "" {
		return map[string]interface{}{}, nil
	}

	var args map[string]interface{}
	if err := json.Unmarshal([]byte(jsonString), &args); err != nil {
		return nil, invalidJSONArgsError(jsonString, err.Error())
	}

	return args, nil
}
