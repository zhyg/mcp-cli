use std::io::{self, Read};
use std::sync::Arc;

use tokio::sync::Semaphore;

use crate::client::*;
use crate::config::*;
use crate::errors::*;
use crate::output::*;

// --- List Command ---

pub async fn list_command(config_path: Option<&str>, with_descriptions: bool) -> Result<(), CliError> {
    let (config, _) = load_config(config_path)?;
    let mut server_names = list_server_names(&config);
    server_names.sort();

    if server_names.is_empty() {
        eprintln!("Warning: No servers configured. Add servers to mcp_servers.json");
        return Ok(());
    }

    let concurrency = get_concurrency_limit();
    debug_log(&format!("Processing {} servers with concurrency {}", server_names.len(), concurrency));

    let results = fetch_all_server_tools(&server_names, &config, concurrency).await;

    let mut display_servers: Vec<ServerWithTools> = results
        .into_iter()
        .map(|r| {
            let tools = if let Some(ref err) = r.error {
                vec![ToolInfo {
                    name: format!("<error: {}>", err),
                    description: None,
                    input_schema: serde_json::json!({}),
                }]
            } else {
                r.tools
            };
            ServerWithTools {
                name: r.name,
                tools,
                instructions: r.instructions,
                error: r.error,
            }
        })
        .collect();

    display_servers.sort_by(|a, b| a.name.cmp(&b.name));
    println!("{}", format_server_list(&display_servers, with_descriptions));
    Ok(())
}

struct ServerResult {
    name: String,
    tools: Vec<ToolInfo>,
    instructions: Option<String>,
    error: Option<String>,
}

async fn fetch_all_server_tools(
    server_names: &[String],
    config: &McpServersConfig,
    concurrency: usize,
) -> Vec<ServerResult> {
    let sem = Arc::new(Semaphore::new(concurrency));
    let mut handles = Vec::new();

    for name in server_names {
        let name = name.clone();
        let config = config.clone();
        let sem = sem.clone();

        let handle = tokio::spawn(async move {
            let _permit = sem.acquire().await.unwrap();
            fetch_server_tools(&name, &config).await
        });
        handles.push(handle);
    }

    let mut results = Vec::new();
    for handle in handles {
        match handle.await {
            Ok(result) => results.push(result),
            Err(e) => results.push(ServerResult {
                name: "unknown".to_string(),
                tools: Vec::new(),
                instructions: None,
                error: Some(e.to_string()),
            }),
        }
    }
    results
}

async fn fetch_server_tools(server_name: &str, config: &McpServersConfig) -> ServerResult {
    let server_config = match get_server_config(config, server_name) {
        Ok(c) => c,
        Err(e) => return ServerResult {
            name: server_name.to_string(),
            tools: Vec::new(),
            instructions: None,
            error: Some(e.message),
        },
    };

    let connection = match get_connection(server_name, server_config).await {
        Ok(c) => c,
        Err(e) => {
            debug_log(&format!("{}: connection failed - {}", server_name, e.message));
            return ServerResult {
                name: server_name.to_string(),
                tools: Vec::new(),
                instructions: None,
                error: Some(e.message),
            };
        }
    };

    let tools = match connection.list_tools().await {
        Ok(t) => t,
        Err(e) => {
            connection.close().await;
            return ServerResult {
                name: server_name.to_string(),
                tools: Vec::new(),
                instructions: None,
                error: Some(e.message),
            };
        }
    };

    let instructions = connection.get_instructions_async().await;
    debug_log(&format!("{}: loaded {} tools", server_name, tools.len()));

    connection.close().await;

    ServerResult {
        name: server_name.to_string(),
        tools,
        instructions,
        error: None,
    }
}

// --- Info Command ---

pub async fn info_command(
    server_name: &str,
    tool_name: Option<&str>,
    config_path: Option<&str>,
    with_descriptions: bool,
) -> Result<(), CliError> {
    let (config, _) = load_config(config_path)?;
    let server_config = get_server_config(&config, server_name)?;

    let connection = get_connection(server_name, server_config).await
        .map_err(|e| {
            eprintln!("{}", format_cli_error(&server_connection_error(server_name, &e.message)));
            e
        })?;

    let tools = match connection.list_tools().await {
        Ok(t) => t,
        Err(e) => {
            connection.close().await;
            return Err(e);
        }
    };

    if let Some(tool) = tool_name {
        let found = tools.iter().find(|t| t.name == tool);
        match found {
            Some(t) => {
                println!("{}", format_tool_schema(server_name, t));
            }
            None => {
                let available: Vec<String> = tools.iter().map(|t| t.name.clone()).collect();
                connection.close().await;
                return Err(tool_not_found_error(tool, server_name, &available));
            }
        }
    } else {
        let instructions = connection.get_instructions_async().await;
        println!("{}", format_server_details(
            server_name,
            server_config,
            &tools,
            with_descriptions,
            instructions.as_deref(),
        ));
    }

    connection.close().await;
    Ok(())
}

// --- Grep Command ---

pub async fn grep_command(
    pattern: &str,
    config_path: Option<&str>,
    with_descriptions: bool,
) -> Result<(), CliError> {
    let (config, _) = load_config(config_path)?;
    let re = glob_to_regex(pattern);
    let server_names = list_server_names(&config);

    if server_names.is_empty() {
        eprintln!("Warning: No servers configured. Add servers to mcp_servers.json");
        return Ok(());
    }

    let concurrency = get_concurrency_limit();
    debug_log(&format!("Searching {} servers for pattern {:?} (concurrency: {})", server_names.len(), pattern, concurrency));

    let results = fetch_all_server_tools(&server_names, &config, concurrency).await;

    let mut all_results = Vec::new();
    let mut failed_servers = Vec::new();

    for r in &results {
        if let Some(ref err) = r.error {
            failed_servers.push(r.name.clone());
            let _ = err;
        }
        for tool in &r.tools {
            if re.is_match(&tool.name) {
                all_results.push(SearchResult {
                    server: r.name.clone(),
                    tool: tool.clone(),
                });
            }
        }
    }

    if !failed_servers.is_empty() {
        eprintln!(
            "Warning: {} server(s) failed to connect: {}",
            failed_servers.len(),
            failed_servers.join(", ")
        );
    }

    if all_results.is_empty() {
        println!("No tools found matching {:?}", pattern);
        println!("  Tip: Pattern matches tool names only (not server names)");
        println!("  Tip: Use '*' for wildcards, e.g. '*file*' or 'read_*'");
        println!("  Tip: Run 'mcp-cli' to list all available tools");
        return Ok(());
    }

    println!("{}", format_search_results(&all_results, with_descriptions));
    Ok(())
}

fn glob_to_regex(pattern: &str) -> regex::Regex {
    let mut escaped = String::new();
    let chars: Vec<char> = pattern.chars().collect();
    let mut i = 0;

    while i < chars.len() {
        let ch = chars[i];
        if ch == '*' && i + 1 < chars.len() && chars[i + 1] == '*' {
            escaped.push_str(".*");
            i += 2;
            while i < chars.len() && chars[i] == '*' {
                i += 1;
            }
        } else if ch == '*' {
            escaped.push_str("[^/]*");
            i += 1;
        } else if ch == '?' {
            escaped.push_str("[^/]");
            i += 1;
        } else if "[.+^${}()|\\]".contains(ch) {
            escaped.push('\\');
            escaped.push(ch);
            i += 1;
        } else {
            escaped.push(ch);
            i += 1;
        }
    }

    regex::Regex::new(&format!("(?i)^{}$", escaped))
        .unwrap_or_else(|_| {
            let literal = regex::escape(pattern);
            regex::Regex::new(&format!("(?i)^{}$", literal)).unwrap()
        })
}

// --- Call Command ---

pub async fn call_command(
    server_name: &str,
    tool_name: &str,
    args_str: Option<&str>,
    config_path: Option<&str>,
) -> Result<(), CliError> {
    let (config, _) = load_config(config_path)?;
    let server_config = get_server_config(&config, server_name)?;

    let args = parse_call_args(args_str)?;

    let connection = get_connection(server_name, server_config).await?;

    match connection.call_tool(tool_name, args).await {
        Ok(result) => {
            println!("{}", format_tool_result(&result));
            connection.close().await;
            Ok(())
        }
        Err(e) => {
            let err_msg = e.message.clone();

            if err_msg.contains("not found") || err_msg.contains("unknown tool") {
                let mut available = Vec::new();
                if let Ok(tools) = connection.list_tools().await {
                    available = tools.iter().map(|t| t.name.clone()).collect();
                }
                connection.close().await;
                Err(tool_not_found_error(tool_name, server_name, &available))
            } else {
                connection.close().await;
                Err(tool_execution_error(tool_name, server_name, &err_msg))
            }
        }
    }
}

fn parse_call_args(args_str: Option<&str>) -> Result<serde_json::Map<String, serde_json::Value>, CliError> {
    let json_string = if let Some(s) = args_str {
        if s.is_empty() {
            return Ok(serde_json::Map::new());
        }
        s.to_string()
    } else if !atty::is(atty::Stream::Stdin) {
        let mut buf = String::new();
        io::stdin().read_to_string(&mut buf)
            .map_err(|e| CliError {
                code: ERROR_CODE_CLIENT_ERROR,
                error_type: "STDIN_READ_ERROR".to_string(),
                message: format!("Failed to read from stdin: {}", e),
                details: None,
                suggestion: None,
            })?;
        let trimmed = buf.trim().to_string();
        if trimmed.is_empty() {
            return Ok(serde_json::Map::new());
        }
        trimmed
    } else {
        return Ok(serde_json::Map::new());
    };

    let parsed: serde_json::Value = serde_json::from_str(&json_string)
        .map_err(|e| invalid_json_args_error(&json_string, &e.to_string()))?;

    match parsed.as_object() {
        Some(map) => Ok(map.clone()),
        None => Err(invalid_json_args_error(&json_string, "Expected a JSON object")),
    }
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_glob_to_regex_exact() {
        let re = glob_to_regex("read_file");
        assert!(re.is_match("read_file"));
        assert!(re.is_match("READ_FILE"));
        assert!(!re.is_match("write_file"));
    }

    #[test]
    fn test_glob_to_regex_star() {
        let re = glob_to_regex("*file*");
        assert!(re.is_match("read_file"));
        assert!(re.is_match("file_read"));
        assert!(re.is_match("myfile"));
        assert!(!re.is_match("directory"));
    }

    #[test]
    fn test_glob_to_regex_prefix() {
        let re = glob_to_regex("read_*");
        assert!(re.is_match("read_file"));
        assert!(re.is_match("read_dir"));
        assert!(!re.is_match("write_file"));
    }

    #[test]
    fn test_glob_to_regex_question_mark() {
        let re = glob_to_regex("file?");
        assert!(re.is_match("file1"));
        assert!(re.is_match("fileA"));
        assert!(!re.is_match("file10"));
        assert!(!re.is_match("file"));
    }

    #[test]
    fn test_glob_to_regex_double_star() {
        let re = glob_to_regex("**");
        assert!(re.is_match("anything"));
        assert!(re.is_match("path/to/file"));
    }

    #[test]
    fn test_glob_to_regex_star_no_slash() {
        let re = glob_to_regex("*");
        assert!(re.is_match("file"));
        assert!(!re.is_match("path/file"));
    }

    #[test]
    fn test_glob_to_regex_special_chars() {
        let re = glob_to_regex("test.file");
        assert!(re.is_match("test.file"));
        assert!(!re.is_match("testXfile"));
    }

    #[test]
    fn test_parse_call_args_empty() {
        let result = parse_call_args(None).unwrap();
        assert!(result.is_empty());
    }

    #[test]
    fn test_parse_call_args_valid_json() {
        let result = parse_call_args(Some(r#"{"path": "./file.txt"}"#)).unwrap();
        assert_eq!(result.get("path").unwrap().as_str(), Some("./file.txt"));
    }

    #[test]
    fn test_parse_call_args_invalid_json() {
        let err = parse_call_args(Some("not json")).unwrap_err();
        assert_eq!(err.error_type, "INVALID_JSON_ARGUMENTS");
    }

    #[test]
    fn test_parse_call_args_empty_string() {
        let result = parse_call_args(Some("")).unwrap();
        assert!(result.is_empty());
    }
}
