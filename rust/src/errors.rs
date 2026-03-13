use std::fmt;

pub const ERROR_CODE_CLIENT_ERROR: i32 = 1;
pub const ERROR_CODE_SERVER_ERROR: i32 = 2;
pub const ERROR_CODE_NETWORK_ERROR: i32 = 3;
#[allow(dead_code)]
pub const ERROR_CODE_AUTH_ERROR: i32 = 4;

#[derive(Debug, Clone)]
pub struct CliError {
    pub code: i32,
    pub error_type: String,
    pub message: String,
    pub details: Option<String>,
    pub suggestion: Option<String>,
}

impl fmt::Display for CliError {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", format_cli_error(self))
    }
}

impl std::error::Error for CliError {}

pub fn format_cli_error(e: &CliError) -> String {
    let mut lines = vec![format!("Error [{}]: {}", e.error_type, e.message)];
    if let Some(ref details) = e.details {
        lines.push(format!("  Details: {}", details));
    }
    if let Some(ref suggestion) = e.suggestion {
        lines.push(format!("  Suggestion: {}", suggestion));
    }
    lines.join("\n")
}

pub fn config_not_found_error(path: &str) -> CliError {
    CliError {
        code: ERROR_CODE_CLIENT_ERROR,
        error_type: "CONFIG_NOT_FOUND".to_string(),
        message: format!("Config file not found: {}", path),
        details: None,
        suggestion: Some(r#"Create mcp_servers.json with: { "mcpServers": { "server-name": { "command": "..." } } }"#.to_string()),
    }
}

pub fn config_search_error() -> CliError {
    CliError {
        code: ERROR_CODE_CLIENT_ERROR,
        error_type: "CONFIG_NOT_FOUND".to_string(),
        message: "No mcp_servers.json found in search paths".to_string(),
        details: Some("Searched: ./mcp_servers.json, ~/.mcp_servers.json, ~/.config/mcp/mcp_servers.json".to_string()),
        suggestion: Some("Create mcp_servers.json in current directory or use -c/--config to specify path".to_string()),
    }
}

pub fn config_invalid_json_error(path: &str, parse_error: &str) -> CliError {
    CliError {
        code: ERROR_CODE_CLIENT_ERROR,
        error_type: "CONFIG_INVALID_JSON".to_string(),
        message: format!("Invalid JSON in config file: {}", path),
        details: Some(parse_error.to_string()),
        suggestion: Some("Check for syntax errors: missing commas, unquoted keys, trailing commas".to_string()),
    }
}

pub fn config_missing_field_error(path: &str) -> CliError {
    CliError {
        code: ERROR_CODE_CLIENT_ERROR,
        error_type: "CONFIG_MISSING_FIELD".to_string(),
        message: r#"Config file missing required "mcpServers" object"#.to_string(),
        details: Some(format!("File: {}", path)),
        suggestion: Some(r#"Config must have structure: { "mcpServers": { ... } }"#.to_string()),
    }
}

pub fn server_not_found_error(server_name: &str, available: &[String]) -> CliError {
    let available_list = if available.is_empty() {
        "(none)".to_string()
    } else {
        available.join(", ")
    };
    let suggestion = if !available.is_empty() {
        let parts: Vec<String> = available.iter().map(|s| format!("mcp-cli {}", s)).collect();
        format!("Use one of: {}", parts.join(", "))
    } else {
        format!(r#"Add server to mcp_servers.json: {{ "mcpServers": {{ "{}": {{ ... }} }} }}"#, server_name)
    };
    CliError {
        code: ERROR_CODE_CLIENT_ERROR,
        error_type: "SERVER_NOT_FOUND".to_string(),
        message: format!("Server {:?} not found in config", server_name),
        details: Some(format!("Available servers: {}", available_list)),
        suggestion: Some(suggestion),
    }
}

pub fn server_connection_error(server_name: &str, cause: &str) -> CliError {
    let suggestion = if cause.contains("not found") || cause.contains("executable file not found") {
        "Command not found. Install the MCP server: npx -y @modelcontextprotocol/server-<name>"
    } else if cause.contains("connection refused") {
        "Server refused connection. Check if the server is running and URL is correct"
    } else if cause.contains("timeout") {
        "Connection timed out. Check network connectivity and server availability"
    } else if cause.contains("401") || cause.contains("Unauthorized") {
        "Authentication required. Add Authorization header to config"
    } else if cause.contains("403") || cause.contains("Forbidden") {
        "Access forbidden. Check credentials and permissions"
    } else {
        "Check server configuration and ensure the server process can start"
    };
    CliError {
        code: ERROR_CODE_NETWORK_ERROR,
        error_type: "SERVER_CONNECTION_FAILED".to_string(),
        message: format!("Failed to connect to server {:?}", server_name),
        details: Some(cause.to_string()),
        suggestion: Some(suggestion.to_string()),
    }
}

pub fn tool_not_found_error(tool_name: &str, server_name: &str, available_tools: &[String]) -> CliError {
    let (tool_list, more_count) = if !available_tools.is_empty() {
        let limit = available_tools.len().min(5);
        let list = available_tools[..limit].join(", ");
        let more = if available_tools.len() > 5 {
            format!(" (+{} more)", available_tools.len() - 5)
        } else {
            String::new()
        };
        (list, more)
    } else {
        (String::new(), String::new())
    };
    let details = if !tool_list.is_empty() {
        Some(format!("Available tools: {}{}", tool_list, more_count))
    } else {
        None
    };
    CliError {
        code: ERROR_CODE_CLIENT_ERROR,
        error_type: "TOOL_NOT_FOUND".to_string(),
        message: format!("Tool {:?} not found in server {:?}", tool_name, server_name),
        details,
        suggestion: Some(format!("Run 'mcp-cli {}' to see all available tools", server_name)),
    }
}

pub fn tool_execution_error(tool_name: &str, server_name: &str, cause: &str) -> CliError {
    let suggestion = if cause.contains("validation") || cause.contains("invalid_type") {
        format!("Run 'mcp-cli {}/{}' to see the input schema, then fix arguments", server_name, tool_name)
    } else if cause.contains("required") {
        format!("Missing required argument. Run 'mcp-cli {}/{}' to see required fields", server_name, tool_name)
    } else if cause.contains("permission") || cause.contains("denied") {
        "Permission denied. Check file/resource permissions".to_string()
    } else if cause.contains("not found") || cause.contains("ENOENT") {
        "Resource not found. Verify the path or identifier exists".to_string()
    } else {
        "Check tool arguments match the expected schema".to_string()
    };
    CliError {
        code: ERROR_CODE_SERVER_ERROR,
        error_type: "TOOL_EXECUTION_FAILED".to_string(),
        message: format!("Tool {:?} execution failed", tool_name),
        details: Some(cause.to_string()),
        suggestion: Some(suggestion),
    }
}

pub fn tool_disabled_error(tool_name: &str, server_name: &str) -> CliError {
    CliError {
        code: ERROR_CODE_CLIENT_ERROR,
        error_type: "TOOL_DISABLED".to_string(),
        message: format!("Tool {:?} is disabled by configuration", tool_name),
        details: Some(format!("Server {:?} has allowedTools/disabledTools filtering configured", server_name)),
        suggestion: Some("Check the server's allowedTools/disabledTools configuration".to_string()),
    }
}

pub fn invalid_json_args_error(input: &str, parse_error: &str) -> CliError {
    let truncated = if input.len() > 100 {
        format!("{}...", &input[..100])
    } else {
        input.to_string()
    };
    let details = if !parse_error.is_empty() {
        format!("Parse error: {}", parse_error)
    } else {
        format!("Input: {}", truncated)
    };
    CliError {
        code: ERROR_CODE_CLIENT_ERROR,
        error_type: "INVALID_JSON_ARGUMENTS".to_string(),
        message: "Invalid JSON in tool arguments".to_string(),
        details: Some(details),
        suggestion: Some("Use valid JSON: '{\"path\": \"./file.txt\"}'. Run 'mcp-cli info <server> <tool>' for the schema.".to_string()),
    }
}

pub fn unknown_option_error(option: &str) -> CliError {
    CliError {
        code: ERROR_CODE_CLIENT_ERROR,
        error_type: "UNKNOWN_OPTION".to_string(),
        message: format!("Unknown option: {}", option),
        details: None,
        suggestion: Some("Valid options: -c/--config, -d/--with-descriptions".to_string()),
    }
}

pub fn missing_argument_error(command: &str, argument: &str) -> CliError {
    let suggestion = match command {
        "call" => "Use 'mcp-cli call <server> <tool> '{\"key\": \"value\"}'".to_string(),
        "grep" => r#"Use 'mcp-cli grep "*pattern*"'"#.to_string(),
        "-c/--config" => "Use 'mcp-cli -c /path/to/mcp_servers.json'".to_string(),
        _ => "Run 'mcp-cli --help' for usage examples".to_string(),
    };
    CliError {
        code: ERROR_CODE_CLIENT_ERROR,
        error_type: "MISSING_ARGUMENT".to_string(),
        message: format!("Missing required argument for {}: {}", command, argument),
        details: None,
        suggestion: Some(suggestion),
    }
}

pub fn too_many_arguments_error(command: &str, got: usize, expected: usize) -> CliError {
    CliError {
        code: ERROR_CODE_CLIENT_ERROR,
        error_type: "TOO_MANY_ARGUMENTS".to_string(),
        message: format!("Too many arguments for {}: got {}, expected {}", command, got, expected),
        details: None,
        suggestion: Some("Run 'mcp-cli --help' for usage examples".to_string()),
    }
}

pub fn unknown_subcommand_error(subcommand: &str) -> CliError {
    CliError {
        code: ERROR_CODE_CLIENT_ERROR,
        error_type: "UNKNOWN_SUBCOMMAND".to_string(),
        message: format!("Unknown subcommand: {:?}", subcommand),
        details: None,
        suggestion: Some("Valid subcommands: info, grep, call. Run 'mcp-cli --help' for usage.".to_string()),
    }
}

pub fn ambiguous_command_error(server_name: &str, tool_name: &str, has_args: bool) -> CliError {
    let cmd = if has_args {
        format!("mcp-cli call {} {} '<json>'", server_name, tool_name)
    } else {
        format!("mcp-cli call {} {}", server_name, tool_name)
    };
    CliError {
        code: ERROR_CODE_CLIENT_ERROR,
        error_type: "AMBIGUOUS_COMMAND".to_string(),
        message: "Ambiguous command: did you mean 'info' or 'call'?".to_string(),
        details: None,
        suggestion: Some(format!("Be explicit: 'mcp-cli info {} {}' or '{}'", server_name, tool_name, cmd)),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_format_cli_error_all_fields() {
        let err = CliError {
            code: ERROR_CODE_CLIENT_ERROR,
            error_type: "TEST_ERROR".to_string(),
            message: "something went wrong".to_string(),
            details: Some("more info".to_string()),
            suggestion: Some("try this".to_string()),
        };
        let formatted = format_cli_error(&err);
        assert!(formatted.contains("Error [TEST_ERROR]: something went wrong"));
        assert!(formatted.contains("Details: more info"));
        assert!(formatted.contains("Suggestion: try this"));
    }

    #[test]
    fn test_format_cli_error_no_optional() {
        let err = CliError {
            code: ERROR_CODE_CLIENT_ERROR,
            error_type: "SIMPLE".to_string(),
            message: "msg".to_string(),
            details: None,
            suggestion: None,
        };
        let formatted = format_cli_error(&err);
        assert!(formatted.contains("Error [SIMPLE]: msg"));
        assert!(!formatted.contains("Details"));
        assert!(!formatted.contains("Suggestion"));
    }

    #[test]
    fn test_error_codes() {
        assert_eq!(ERROR_CODE_CLIENT_ERROR, 1);
        assert_eq!(ERROR_CODE_SERVER_ERROR, 2);
        assert_eq!(ERROR_CODE_NETWORK_ERROR, 3);
        assert_eq!(ERROR_CODE_AUTH_ERROR, 4);
    }

    #[test]
    fn test_config_not_found_error() {
        let err = config_not_found_error("/path/to/config.json");
        assert_eq!(err.error_type, "CONFIG_NOT_FOUND");
        assert!(err.message.contains("/path/to/config.json"));
    }

    #[test]
    fn test_config_search_error() {
        let err = config_search_error();
        assert_eq!(err.error_type, "CONFIG_NOT_FOUND");
        assert!(err.details.as_ref().unwrap().contains("mcp_servers.json"));
    }

    #[test]
    fn test_config_invalid_json_error() {
        let err = config_invalid_json_error("/cfg.json", "unexpected token");
        assert_eq!(err.error_type, "CONFIG_INVALID_JSON");
        assert!(err.details.as_ref().unwrap().contains("unexpected token"));
    }

    #[test]
    fn test_config_missing_field_error() {
        let err = config_missing_field_error("/cfg.json");
        assert_eq!(err.error_type, "CONFIG_MISSING_FIELD");
        assert!(err.message.contains("mcpServers"));
    }

    #[test]
    fn test_server_not_found_error_with_available() {
        let available = vec!["fs".to_string(), "github".to_string()];
        let err = server_not_found_error("unknown", &available);
        assert_eq!(err.error_type, "SERVER_NOT_FOUND");
        assert!(err.details.as_ref().unwrap().contains("fs, github"));
        assert!(err.suggestion.as_ref().unwrap().contains("Use one of"));
    }

    #[test]
    fn test_server_not_found_error_no_available() {
        let err = server_not_found_error("unknown", &[]);
        assert!(err.details.as_ref().unwrap().contains("(none)"));
        assert!(err.suggestion.as_ref().unwrap().contains("Add server"));
    }

    #[test]
    fn test_server_connection_error_patterns() {
        let err = server_connection_error("s", "executable file not found");
        assert!(err.suggestion.as_ref().unwrap().contains("Command not found"));

        let err = server_connection_error("s", "connection refused");
        assert!(err.suggestion.as_ref().unwrap().contains("refused"));

        let err = server_connection_error("s", "timeout");
        assert!(err.suggestion.as_ref().unwrap().contains("timed out"));

        let err = server_connection_error("s", "401 Unauthorized");
        assert!(err.suggestion.as_ref().unwrap().contains("Authentication"));

        let err = server_connection_error("s", "403 Forbidden");
        assert!(err.suggestion.as_ref().unwrap().contains("forbidden"));
    }

    #[test]
    fn test_tool_not_found_error_truncation() {
        let tools: Vec<String> = (0..8).map(|i| format!("tool_{}", i)).collect();
        let err = tool_not_found_error("missing", "server", &tools);
        assert!(err.details.as_ref().unwrap().contains("tool_0"));
        assert!(err.details.as_ref().unwrap().contains("(+3 more)"));
    }

    #[test]
    fn test_tool_not_found_error_few_tools() {
        let tools = vec!["a".to_string(), "b".to_string()];
        let err = tool_not_found_error("c", "s", &tools);
        assert!(!err.details.as_ref().unwrap().contains("more"));
    }

    #[test]
    fn test_tool_execution_error_patterns() {
        let err = tool_execution_error("t", "s", "validation error");
        assert!(err.suggestion.as_ref().unwrap().contains("schema"));

        let err = tool_execution_error("t", "s", "required field missing");
        assert!(err.suggestion.as_ref().unwrap().contains("required"));

        let err = tool_execution_error("t", "s", "permission denied");
        assert!(err.suggestion.as_ref().unwrap().contains("Permission"));

        let err = tool_execution_error("t", "s", "not found");
        assert!(err.suggestion.as_ref().unwrap().contains("Resource"));
    }

    #[test]
    fn test_invalid_json_args_error_truncation() {
        let long_input = "x".repeat(200);
        let err = invalid_json_args_error(&long_input, "");
        assert!(err.details.as_ref().unwrap().len() < 200);
    }

    #[test]
    fn test_invalid_json_args_error_with_parse_error() {
        let err = invalid_json_args_error("{bad}", "unexpected token");
        assert!(err.details.as_ref().unwrap().contains("Parse error: unexpected token"));
    }

    #[test]
    fn test_unknown_option_error() {
        let err = unknown_option_error("--foo");
        assert_eq!(err.error_type, "UNKNOWN_OPTION");
        assert!(err.message.contains("--foo"));
    }

    #[test]
    fn test_missing_argument_error_command_specific() {
        let err = missing_argument_error("call", "tool");
        assert!(err.suggestion.as_ref().unwrap().contains("call"));

        let err = missing_argument_error("grep", "pattern");
        assert!(err.suggestion.as_ref().unwrap().contains("grep"));

        let err = missing_argument_error("-c/--config", "path");
        assert!(err.suggestion.as_ref().unwrap().contains("-c"));
    }

    #[test]
    fn test_too_many_arguments_error() {
        let err = too_many_arguments_error("grep", 3, 1);
        assert!(err.message.contains("got 3"));
        assert!(err.message.contains("expected 1"));
    }

    #[test]
    fn test_unknown_subcommand_error() {
        let err = unknown_subcommand_error("run");
        assert_eq!(err.error_type, "UNKNOWN_SUBCOMMAND");
        assert!(err.message.contains("run"));
    }

    #[test]
    fn test_ambiguous_command_error() {
        let err = ambiguous_command_error("server", "tool", false);
        assert_eq!(err.error_type, "AMBIGUOUS_COMMAND");
        assert!(err.suggestion.as_ref().unwrap().contains("info"));
        assert!(err.suggestion.as_ref().unwrap().contains("call"));

        let err = ambiguous_command_error("server", "tool", true);
        assert!(err.suggestion.as_ref().unwrap().contains("<json>"));
    }
}
