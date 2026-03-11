package main

import (
	"testing"
)

// --- Basic patterns ---

func TestGlobToRegex_ExactMatch(t *testing.T) {
	re := globToRegex("read_file")
	assertMatch(t, re, "read_file", true)
	assertMatch(t, re, "read_files", false)
	assertMatch(t, re, "aread_file", false)
}

func TestGlobToRegex_CaseInsensitive(t *testing.T) {
	re := globToRegex("Read_File")
	assertMatch(t, re, "read_file", true)
	assertMatch(t, re, "READ_FILE", true)
}

// --- Single asterisk (*) ---

func TestGlobToRegex_Star_AnyChars(t *testing.T) {
	re := globToRegex("*file*")
	assertMatch(t, re, "read_file", true)
	assertMatch(t, re, "file_utils", true)
	assertMatch(t, re, "my_file_tool", true)
	assertMatch(t, re, "file", true)
}

func TestGlobToRegex_Star_NoSlash(t *testing.T) {
	re := globToRegex("server/*")
	assertMatch(t, re, "server/tool", true)
	assertMatch(t, re, "server/", true)
	assertMatch(t, re, "server/sub/tool", false)
}

func TestGlobToRegex_Star_Prefix(t *testing.T) {
	re := globToRegex("read_*")
	assertMatch(t, re, "read_file", true)
	assertMatch(t, re, "read_directory", true)
	assertMatch(t, re, "write_file", false)
}

func TestGlobToRegex_Star_Suffix(t *testing.T) {
	re := globToRegex("*_file")
	assertMatch(t, re, "read_file", true)
	assertMatch(t, re, "write_file", true)
	assertMatch(t, re, "file_reader", false)
}

// --- Double asterisk (**) - globstar ---

func TestGlobToRegex_Globstar_IncludesSlashes(t *testing.T) {
	re := globToRegex("server/**")
	assertMatch(t, re, "server/tool", true)
	assertMatch(t, re, "server/sub/tool", true)
	assertMatch(t, re, "server/", true)
}

func TestGlobToRegex_Globstar_AtStart(t *testing.T) {
	re := globToRegex("**/tool")
	assertMatch(t, re, "server/tool", true)
	assertMatch(t, re, "path/to/tool", true)
}

func TestGlobToRegex_Globstar_InMiddle(t *testing.T) {
	re := globToRegex("**test**")
	assertMatch(t, re, "test", true)
	assertMatch(t, re, "my_test_tool", true)
	assertMatch(t, re, "testing", true)
	assertMatch(t, re, "unit_test", true)
	assertMatch(t, re, "server/test/tool", true)
}

func TestGlobToRegex_TripleAsterisk(t *testing.T) {
	re := globToRegex("***file***")
	assertMatch(t, re, "file", true)
	assertMatch(t, re, "myfile", true)
	assertMatch(t, re, "file_utils", true)
}

// --- Question mark (?) ---

func TestGlobToRegex_QuestionMark_SingleChar(t *testing.T) {
	re := globToRegex("file?")
	assertMatch(t, re, "file1", true)
	assertMatch(t, re, "files", true)
	assertMatch(t, re, "file", false)
	assertMatch(t, re, "file12", false)
}

func TestGlobToRegex_QuestionMark_NoSlash(t *testing.T) {
	re := globToRegex("a?b")
	assertMatch(t, re, "aXb", true)
	assertMatch(t, re, "a/b", false)
}

// --- Special regex characters ---

func TestGlobToRegex_EscapesDots(t *testing.T) {
	re := globToRegex("file.txt")
	assertMatch(t, re, "file.txt", true)
	assertMatch(t, re, "fileXtxt", false)
}

func TestGlobToRegex_EscapesBrackets(t *testing.T) {
	re := globToRegex("[test]")
	assertMatch(t, re, "[test]", true)
	assertMatch(t, re, "test", false)
}

func TestGlobToRegex_EscapesParentheses(t *testing.T) {
	re := globToRegex("func(arg)")
	assertMatch(t, re, "func(arg)", true)
}

func TestGlobToRegex_EscapesPlusCaret(t *testing.T) {
	re := globToRegex("a+b^c")
	assertMatch(t, re, "a+b^c", true)
}

// --- Real-world patterns ---

func TestGlobToRegex_FilesystemTools(t *testing.T) {
	re := globToRegex("*file*")
	assertMatch(t, re, "read_file", true)
	assertMatch(t, re, "write_file", true)
	assertMatch(t, re, "list_directory", false)
}

func TestGlobToRegex_ToolNamesOnly(t *testing.T) {
	re := globToRegex("read_*")
	assertMatch(t, re, "read_file", true)
	assertMatch(t, re, "read_directory", true)
	assertMatch(t, re, "write_file", false)
}

func TestGlobToRegex_SearchTools(t *testing.T) {
	re := globToRegex("*search*")
	assertMatch(t, re, "search", true)
	assertMatch(t, re, "search_repos", true)
	assertMatch(t, re, "full_text_search", true)
	assertMatch(t, re, "find_files", false)
}

// --- Helper ---

func assertMatch(t *testing.T, re interface{ MatchString(string) bool }, input string, expected bool) {
	t.Helper()
	got := re.MatchString(input)
	if got != expected {
		t.Errorf("pattern.MatchString(%q) = %v, want %v", input, got, expected)
	}
}
