package main

import (
	"os"
	"testing"
)

func TestFormatServerList_WithTools(t *testing.T) {
	servers := []ServerWithTools{
		{
			Name: "github",
			Tools: []ToolInfo{
				{Name: "search", Description: "Search repos"},
				{Name: "clone"},
			},
		},
	}
	output := formatServerList(servers, false)
	assertContains(t, output, "github")
	assertContains(t, output, "search")
	assertContains(t, output, "clone")
	assertNotContains(t, output, "Search repos")
}

func TestFormatServerList_WithDescriptions(t *testing.T) {
	servers := []ServerWithTools{
		{
			Name: "github",
			Tools: []ToolInfo{
				{Name: "search", Description: "Search repos"},
			},
		},
	}
	output := formatServerList(servers, true)
	assertContains(t, output, "search")
	assertContains(t, output, "Search repos")
}

func TestFormatServerList_WithInstructions(t *testing.T) {
	servers := []ServerWithTools{
		{
			Name:         "github",
			Tools:        []ToolInfo{{Name: "search"}},
			Instructions: "Use this server to interact with GitHub",
		},
	}
	output := formatServerList(servers, false)
	assertContains(t, output, "Instructions:")
	assertContains(t, output, "Use this server")
}

func TestFormatServerList_InstructionsTruncated(t *testing.T) {
	long := ""
	for i := 0; i < 120; i++ {
		long += "a"
	}
	servers := []ServerWithTools{
		{
			Name:         "test",
			Tools:        []ToolInfo{{Name: "t"}},
			Instructions: long,
		},
	}
	output := formatServerList(servers, false)
	assertContains(t, output, "...")
}

func TestFormatSearchResults_AlwaysShowsDescription(t *testing.T) {
	// grep always shows descriptions regardless of withDescriptions flag
	results := []SearchResult{
		{Server: "github", Tool: ToolInfo{Name: "search", Description: "Search repos"}},
	}
	output := formatSearchResults(results, false)
	assertContains(t, output, "github")
	assertContains(t, output, "search")
	assertContains(t, output, "Search repos")
}

func TestFormatSearchResults_SpaceSeparated(t *testing.T) {
	os.Setenv("NO_COLOR", "1")
	defer os.Unsetenv("NO_COLOR")

	results := []SearchResult{
		{Server: "github", Tool: ToolInfo{Name: "search", Description: "Search repos"}},
	}
	output := formatSearchResults(results, false)
	// Should be space separated, not slash
	assertContains(t, output, "github search")
	assertNotContains(t, output, "github/search")
}

func TestFormatSearchResults_NoDescription(t *testing.T) {
	os.Setenv("NO_COLOR", "1")
	defer os.Unsetenv("NO_COLOR")

	results := []SearchResult{
		{Server: "github", Tool: ToolInfo{Name: "search"}},
	}
	output := formatSearchResults(results, false)
	assertEqual(t, output, "github search")
}

func TestFormatToolSchema(t *testing.T) {
	tool := ToolInfo{
		Name:        "read_file",
		Description: "Read a file",
		InputSchema: map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"path": map[string]interface{}{"type": "string"},
			},
		},
	}
	output := formatToolSchema("filesystem", tool)
	assertContains(t, output, "read_file")
	assertContains(t, output, "filesystem")
	assertContains(t, output, "Read a file")
	assertContains(t, output, "Input Schema")
	assertContains(t, output, "path")
}

func TestFormatToolResult_JSON(t *testing.T) {
	result := map[string]interface{}{
		"key": "value",
	}
	output := formatToolResult(result)
	assertContains(t, output, "key")
	assertContains(t, output, "value")
}

func TestFormatServerDetails(t *testing.T) {
	config := &ServerConfig{Command: "echo", Args: []string{"hello"}}
	tools := []ToolInfo{{Name: "test_tool", Description: "A test tool"}}
	output := formatServerDetails("test-server", config, tools, false, "Test instructions")
	assertContains(t, output, "test-server")
	assertContains(t, output, "stdio")
	assertContains(t, output, "echo")
	assertContains(t, output, "test_tool")
	assertContains(t, output, "Test instructions")
}

func TestFormatServerDetails_HTTP(t *testing.T) {
	config := &ServerConfig{URL: "https://example.com/mcp"}
	tools := []ToolInfo{{Name: "test_tool"}}
	output := formatServerDetails("remote", config, tools, false, "")
	assertContains(t, output, "HTTP")
	assertContains(t, output, "https://example.com/mcp")
}
