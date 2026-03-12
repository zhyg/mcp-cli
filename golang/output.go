package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"
)

// ANSI color codes.
const (
	colorReset   = "\x1b[0m"
	colorBold    = "\x1b[1m"
	colorDim     = "\x1b[2m"
	colorRed     = "\x1b[31m"
	colorGreen   = "\x1b[32m"
	colorYellow  = "\x1b[33m"
	colorBlue    = "\x1b[34m"
	colorMagenta = "\x1b[35m"
	colorCyan    = "\x1b[36m"
)

// shouldColorize returns true if stdout supports colors.
func shouldColorize() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	return term.IsTerminal(int(os.Stdout.Fd()))
}

// colorize applies ANSI color codes to text if terminal supports it.
func colorize(text, code string) string {
	if !shouldColorize() {
		return text
	}
	return code + text + colorReset
}

// ServerWithTools represents a server and its tools for display.
type ServerWithTools struct {
	Name         string
	Tools        []ToolInfo
	Instructions string
	Error        string
}

// formatServerList formats the list of servers and tools.
func formatServerList(servers []ServerWithTools, withDescriptions bool) string {
	var lines []string

	for _, server := range servers {
		lines = append(lines, colorize(server.Name, colorBold+colorCyan))

		// Show instructions if available
		if server.Instructions != "" {
			instrLines := strings.Split(server.Instructions, "\n")
			firstLine := instrLines[0]
			if len(firstLine) > 100 {
				firstLine = firstLine[:100]
			}
			suffix := ""
			if len(instrLines) > 1 || len(instrLines[0]) > 100 {
				suffix = "..."
			}
			lines = append(lines, fmt.Sprintf("  %s", colorize("Instructions: "+firstLine+suffix, colorDim)))
		}

		for _, tool := range server.Tools {
			if withDescriptions && tool.Description != "" {
				lines = append(lines, fmt.Sprintf("  • %s - %s", tool.Name, colorize(tool.Description, colorDim)))
			} else {
				lines = append(lines, fmt.Sprintf("  • %s", tool.Name))
			}
		}

		lines = append(lines, "") // Empty line between servers
	}

	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// SearchResult represents a single grep search result.
type SearchResult struct {
	Server string
	Tool   ToolInfo
}

// formatSearchResults formats grep search results.
func formatSearchResults(results []SearchResult, withDescriptions bool) string {
	var lines []string

	for _, r := range results {
		entry := colorize(r.Server, colorCyan) + "/" + colorize(r.Tool.Name, colorGreen)
		if withDescriptions && r.Tool.Description != "" {
			entry += " - " + colorize(r.Tool.Description, colorDim)
		}
		lines = append(lines, entry)
	}

	return strings.Join(lines, "\n")
}

// formatServerDetails formats detailed server info.
func formatServerDetails(serverName string, config *ServerConfig, tools []ToolInfo, withDescriptions bool, instructions string) string {
	var lines []string

	lines = append(lines, fmt.Sprintf("%s %s", colorize("Server:", colorBold), colorize(serverName, colorCyan)))

	if config.IsHTTP() {
		lines = append(lines, fmt.Sprintf("%s HTTP", colorize("Transport:", colorBold)))
		lines = append(lines, fmt.Sprintf("%s %s", colorize("URL:", colorBold), config.URL))
	} else {
		lines = append(lines, fmt.Sprintf("%s stdio", colorize("Transport:", colorBold)))
		args := strings.Join(config.Args, " ")
		lines = append(lines, fmt.Sprintf("%s %s %s", colorize("Command:", colorBold), config.Command, args))
	}

	if instructions != "" {
		lines = append(lines, "")
		lines = append(lines, colorize("Instructions:", colorBold))
		for _, line := range strings.Split(instructions, "\n") {
			lines = append(lines, "  "+line)
		}
	}

	lines = append(lines, "")
	lines = append(lines, colorize(fmt.Sprintf("Tools (%d):", len(tools)), colorBold))

	for _, tool := range tools {
		lines = append(lines, fmt.Sprintf("  %s", colorize(tool.Name, colorGreen)))
		if withDescriptions && tool.Description != "" {
			lines = append(lines, fmt.Sprintf("    %s", colorize(tool.Description, colorDim)))
		}

		// Show parameters from schema
		schema := tool.InputSchema
		if props, ok := schema["properties"].(map[string]interface{}); ok {
			lines = append(lines, fmt.Sprintf("    %s", colorize("Parameters:", colorYellow)))

			required := map[string]bool{}
			if reqList, ok := schema["required"].([]interface{}); ok {
				for _, r := range reqList {
					if s, ok := r.(string); ok {
						required[s] = true
					}
				}
			}

			for name, propRaw := range props {
				prop, ok := propRaw.(map[string]interface{})
				if !ok {
					continue
				}
				reqStr := "optional"
				if required[name] {
					reqStr = "required"
				}
				typeStr := "any"
				if t, ok := prop["type"].(string); ok {
					typeStr = t
				}
				desc := ""
				if withDescriptions {
					if d, ok := prop["description"].(string); ok {
						desc = " - " + d
					}
				}
				lines = append(lines, fmt.Sprintf("      • %s (%s, %s)%s", name, typeStr, reqStr, desc))
			}
		}

		lines = append(lines, "")
	}

	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}

// formatToolSchema formats a single tool's schema.
func formatToolSchema(serverName string, tool ToolInfo) string {
	var lines []string

	lines = append(lines, fmt.Sprintf("%s %s", colorize("Tool:", colorBold), colorize(tool.Name, colorGreen)))
	lines = append(lines, fmt.Sprintf("%s %s", colorize("Server:", colorBold), colorize(serverName, colorCyan)))
	lines = append(lines, "")

	if tool.Description != "" {
		lines = append(lines, colorize("Description:", colorBold))
		lines = append(lines, "  "+tool.Description)
		lines = append(lines, "")
	}

	lines = append(lines, colorize("Input Schema:", colorBold))
	schemaJSON, err := json.MarshalIndent(tool.InputSchema, "", "  ")
	if err != nil {
		lines = append(lines, "  (error formatting schema)")
	} else {
		lines = append(lines, string(schemaJSON))
	}

	return strings.Join(lines, "\n")
}

// formatToolResult formats a tool call result for CLI output as raw JSON.
func formatToolResult(result interface{}) string {
	data, err := json.MarshalIndent(result, "", "  ")
	if err != nil {
		return fmt.Sprintf("%v", result)
	}
	return string(data)
}
