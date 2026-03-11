package main

import (
	"testing"
)

var sampleTools = []ToolInfo{
	{Name: "read_file", Description: "Read a file"},
	{Name: "write_file", Description: "Write a file"},
	{Name: "delete_file", Description: "Delete a file"},
	{Name: "list_directory", Description: "List directory contents"},
	{Name: "search_files", Description: "Search files"},
}

func toolNames(tools []ToolInfo) []string {
	names := make([]string, len(tools))
	for i, t := range tools {
		names[i] = t.Name
	}
	return names
}

func containsName(tools []ToolInfo, name string) bool {
	for _, t := range tools {
		if t.Name == name {
			return true
		}
	}
	return false
}

// --- filterTools ---

func TestFilterTools_NoFiltering(t *testing.T) {
	config := &ServerConfig{Command: "test"}
	result := filterTools(sampleTools, config)
	assertEqual(t, len(result), 5)
}

func TestFilterTools_AllowedExact(t *testing.T) {
	config := &ServerConfig{
		Command:      "test",
		AllowedTools: []string{"read_file", "list_directory"},
	}
	result := filterTools(sampleTools, config)
	assertEqual(t, len(result), 2)
	if !containsName(result, "read_file") {
		t.Error("expected read_file in result")
	}
	if !containsName(result, "list_directory") {
		t.Error("expected list_directory in result")
	}
}

func TestFilterTools_AllowedWildcard(t *testing.T) {
	config := &ServerConfig{
		Command:      "test",
		AllowedTools: []string{"*file*"},
	}
	result := filterTools(sampleTools, config)
	assertEqual(t, len(result), 4)
	if !containsName(result, "read_file") {
		t.Error("expected read_file")
	}
	if !containsName(result, "write_file") {
		t.Error("expected write_file")
	}
	if !containsName(result, "delete_file") {
		t.Error("expected delete_file")
	}
	if !containsName(result, "search_files") {
		t.Error("expected search_files")
	}
}

func TestFilterTools_AllowedPrefix(t *testing.T) {
	config := &ServerConfig{
		Command:      "test",
		AllowedTools: []string{"read_*"},
	}
	result := filterTools(sampleTools, config)
	assertEqual(t, len(result), 1)
	assertEqual(t, result[0].Name, "read_file")
}

func TestFilterTools_DisabledExact(t *testing.T) {
	config := &ServerConfig{
		Command:       "test",
		DisabledTools: []string{"delete_file"},
	}
	result := filterTools(sampleTools, config)
	assertEqual(t, len(result), 4)
	if containsName(result, "delete_file") {
		t.Error("expected delete_file to be filtered out")
	}
}

func TestFilterTools_DisabledWildcard(t *testing.T) {
	config := &ServerConfig{
		Command:       "test",
		DisabledTools: []string{"*file"},
	}
	result := filterTools(sampleTools, config)
	assertEqual(t, len(result), 2)
	if !containsName(result, "list_directory") {
		t.Error("expected list_directory")
	}
	if !containsName(result, "search_files") {
		t.Error("expected search_files")
	}
}

func TestFilterTools_DisabledTakesPrecedence(t *testing.T) {
	config := &ServerConfig{
		Command:       "test",
		AllowedTools:  []string{"*file*"},
		DisabledTools: []string{"write_file", "delete_file"},
	}
	result := filterTools(sampleTools, config)
	assertEqual(t, len(result), 2)
	if !containsName(result, "read_file") {
		t.Error("expected read_file")
	}
	if !containsName(result, "search_files") {
		t.Error("expected search_files")
	}
	if containsName(result, "write_file") {
		t.Error("expected write_file to be filtered out")
	}
	if containsName(result, "delete_file") {
		t.Error("expected delete_file to be filtered out")
	}
}

func TestFilterTools_CombineAllowedAndDisabled(t *testing.T) {
	config := &ServerConfig{
		Command:       "test",
		AllowedTools:  []string{"read_file", "write_file", "delete_file"},
		DisabledTools: []string{"delete_file"},
	}
	result := filterTools(sampleTools, config)
	assertEqual(t, len(result), 2)
	if !containsName(result, "read_file") {
		t.Error("expected read_file")
	}
	if !containsName(result, "write_file") {
		t.Error("expected write_file")
	}
}

func TestFilterTools_CaseInsensitive(t *testing.T) {
	config := &ServerConfig{
		Command:      "test",
		AllowedTools: []string{"READ_FILE"},
	}
	result := filterTools(sampleTools, config)
	assertEqual(t, len(result), 1)
	assertEqual(t, result[0].Name, "read_file")
}

func TestFilterTools_QuestionMarkWildcard(t *testing.T) {
	tools := []ToolInfo{
		{Name: "file1"},
		{Name: "file2"},
		{Name: "file10"},
	}
	config := &ServerConfig{
		Command:      "test",
		AllowedTools: []string{"file?"},
	}
	result := filterTools(tools, config)
	assertEqual(t, len(result), 2)
	if !containsName(result, "file1") {
		t.Error("expected file1")
	}
	if !containsName(result, "file2") {
		t.Error("expected file2")
	}
}

// --- isToolAllowed ---

func TestIsToolAllowed_NoFiltering(t *testing.T) {
	config := &ServerConfig{Command: "test"}
	if !isToolAllowed("any_tool", config) {
		t.Error("expected tool to be allowed with no filtering")
	}
}

func TestIsToolAllowed_Allowed(t *testing.T) {
	config := &ServerConfig{
		Command:      "test",
		AllowedTools: []string{"read_file"},
	}
	if !isToolAllowed("read_file", config) {
		t.Error("expected read_file to be allowed")
	}
}

func TestIsToolAllowed_NotAllowed(t *testing.T) {
	config := &ServerConfig{
		Command:      "test",
		AllowedTools: []string{"read_file"},
	}
	if isToolAllowed("write_file", config) {
		t.Error("expected write_file to NOT be allowed")
	}
}

func TestIsToolAllowed_Disabled(t *testing.T) {
	config := &ServerConfig{
		Command:       "test",
		DisabledTools: []string{"delete_file"},
	}
	if isToolAllowed("delete_file", config) {
		t.Error("expected delete_file to be disabled")
	}
}

func TestIsToolAllowed_NotDisabled(t *testing.T) {
	config := &ServerConfig{
		Command:       "test",
		DisabledTools: []string{"delete_file"},
	}
	if !isToolAllowed("read_file", config) {
		t.Error("expected read_file to be allowed")
	}
}

func TestIsToolAllowed_DisabledPrecedence(t *testing.T) {
	config := &ServerConfig{
		Command:       "test",
		AllowedTools:  []string{"*file*"},
		DisabledTools: []string{"write_file"},
	}
	if isToolAllowed("write_file", config) {
		t.Error("expected write_file to be disabled despite allowed pattern")
	}
	if !isToolAllowed("read_file", config) {
		t.Error("expected read_file to be allowed")
	}
}

func TestIsToolAllowed_WildcardPatterns(t *testing.T) {
	config := &ServerConfig{
		Command:      "test",
		AllowedTools: []string{"read_*"},
	}
	if !isToolAllowed("read_file", config) {
		t.Error("expected read_file to match read_*")
	}
	if !isToolAllowed("read_directory", config) {
		t.Error("expected read_directory to match read_*")
	}
	if isToolAllowed("write_file", config) {
		t.Error("expected write_file to NOT match read_*")
	}
}
