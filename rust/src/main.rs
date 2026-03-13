mod client;
mod commands;
mod config;
mod daemon;
mod daemon_client;
mod errors;
mod output;

use config::VERSION;
use errors::*;
use regex::Regex;

#[derive(Debug)]
struct ParsedArgs {
    command: String,
    server: Option<String>,
    tool: Option<String>,
    pattern: Option<String>,
    args: Option<String>,
    with_descriptions: bool,
    config_path: Option<String>,
}

#[allow(dead_code)]
const KNOWN_SUBCOMMANDS: &[&str] = &["info", "grep", "call"];

fn is_possible_subcommand(arg: &str) -> bool {
    let aliases = [
        "run", "execute", "exec", "invoke",
        "list", "ls", "get", "show",
        "describe", "search", "find", "query",
    ];
    aliases.contains(&arg.to_lowercase().as_str())
}

fn parse_server_tool(args: &[String]) -> (String, Option<String>) {
    if args.is_empty() {
        return (String::new(), None);
    }
    let first = &args[0];
    if let Some(idx) = first.find('/') {
        let server = first[..idx].to_string();
        let tool = &first[idx + 1..];
        let tool = if tool.is_empty() { None } else { Some(tool.to_string()) };
        (server, tool)
    } else {
        let server = first.clone();
        let tool = args.get(1).cloned();
        (server, tool)
    }
}

fn parse_args(args: Vec<String>) -> ParsedArgs {
    let mut result = ParsedArgs {
        command: "info".to_string(),
        server: None,
        tool: None,
        pattern: None,
        args: None,
        with_descriptions: false,
        config_path: None,
    };

    let mut positional = Vec::new();
    let mut i = 0;

    while i < args.len() {
        let arg = &args[i];
        match arg.as_str() {
            "-h" | "--help" => {
                result.command = "help".to_string();
                return result;
            }
            "-v" | "--version" => {
                result.command = "version".to_string();
                return result;
            }
            "-d" | "--with-descriptions" => {
                result.with_descriptions = true;
            }
            "-c" | "--config" => {
                i += 1;
                if i >= args.len() {
                    eprintln!("{}", format_cli_error(&missing_argument_error("-c/--config", "path")));
                    std::process::exit(ERROR_CODE_CLIENT_ERROR);
                }
                result.config_path = Some(args[i].clone());
            }
            _ => {
                if arg.starts_with('-') && arg != "-" {
                    eprintln!("{}", format_cli_error(&unknown_option_error(arg)));
                    std::process::exit(ERROR_CODE_CLIENT_ERROR);
                }
                positional.push(arg.clone());
            }
        }
        i += 1;
    }

    if positional.is_empty() {
        result.command = "list".to_string();
        return result;
    }

    let first_arg = &positional[0];

    // --- Explicit subcommand routing ---

    if first_arg == "info" {
        result.command = "info".to_string();
        let remaining = positional[1..].to_vec();
        let (server, tool) = parse_server_tool(&remaining);

        if server.is_empty() {
            let mut available_servers = Vec::new();
            if let Ok((cfg, _)) = config::load_config(result.config_path.as_deref()) {
                available_servers = config::list_server_names(&cfg);
            }
            let server_list = if !available_servers.is_empty() {
                available_servers.join(", ")
            } else {
                "(none found)".to_string()
            };
            eprintln!("Error [MISSING_ARGUMENT]: Missing required argument for info: server");
            eprintln!("  Available servers: {}", server_list);
            eprintln!("  Suggestion: Use 'mcp-cli info <server>' to see server details, or just 'mcp-cli' to list all");
            std::process::exit(ERROR_CODE_CLIENT_ERROR);
        }

        result.server = Some(server);
        result.tool = tool;
        return result;
    }

    if first_arg == "grep" {
        result.command = "grep".to_string();
        if positional.len() < 2 {
            eprintln!("{}", format_cli_error(&missing_argument_error("grep", "pattern")));
            std::process::exit(ERROR_CODE_CLIENT_ERROR);
        }
        result.pattern = Some(positional[1].clone());
        if positional.len() > 2 {
            eprintln!("{}", format_cli_error(&too_many_arguments_error("grep", positional.len() - 1, 1)));
            std::process::exit(ERROR_CODE_CLIENT_ERROR);
        }
        return result;
    }

    if first_arg == "call" {
        result.command = "call".to_string();
        let remaining = positional[1..].to_vec();

        if remaining.is_empty() {
            eprintln!("{}", format_cli_error(&missing_argument_error("call", "server and tool")));
            std::process::exit(ERROR_CODE_CLIENT_ERROR);
        }

        let (server, tool) = parse_server_tool(&remaining);
        result.server = Some(server);

        if tool.is_none() {
            if remaining.len() < 2 {
                eprintln!("{}", format_cli_error(&missing_argument_error("call", "tool")));
                std::process::exit(ERROR_CODE_CLIENT_ERROR);
            }
        }

        result.tool = tool;

        let args_start_index = if remaining[0].contains('/') { 1 } else { 2 };
        if args_start_index < remaining.len() {
            let json_args = remaining[args_start_index..].to_vec();
            let args_value = json_args.join(" ");
            if args_value != "-" {
                result.args = Some(args_value);
            }
        }

        return result;
    }

    // --- Unknown subcommand ---
    if is_possible_subcommand(first_arg) {
        eprintln!("{}", format_cli_error(&unknown_subcommand_error(first_arg)));
        std::process::exit(ERROR_CODE_CLIENT_ERROR);
    }

    // --- Slash format without subcommand ---
    if first_arg.contains('/') {
        let parts: Vec<&str> = first_arg.splitn(2, '/').collect();
        let server_name = parts[0];
        let tool_name = if parts.len() > 1 { parts[1] } else { "" };
        let has_args = positional.len() > 1;
        eprintln!("{}", format_cli_error(&ambiguous_command_error(server_name, tool_name, has_args)));
        std::process::exit(ERROR_CODE_CLIENT_ERROR);
    }

    // --- Ambiguous command detection ---
    if positional.len() >= 2 {
        let server_name = &positional[0];
        let possible_tool = &positional[1];

        let looks_like_json = possible_tool.starts_with('{') || possible_tool.starts_with('[');
        let looks_like_tool = Regex::new(r"^[a-zA-Z_][a-zA-Z0-9_-]*$").unwrap().is_match(possible_tool);

        if !looks_like_json && looks_like_tool {
            let has_args = positional.len() > 2;
            eprintln!("{}", format_cli_error(&ambiguous_command_error(server_name, possible_tool, has_args)));
            std::process::exit(ERROR_CODE_CLIENT_ERROR);
        }
    }

    // --- Default: single server name → info ---
    result.command = "info".to_string();
    result.server = Some(first_arg.clone());
    result
}

fn print_help() {
    println!(r#"
mcp-cli v{} - A lightweight CLI for MCP servers

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
  mcp-cli call server tool '{{}}'  Space-separated
  mcp-cli call server/tool '{{}}'  Slash-separated

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
  mcp-cli call filesystem read_file '{{}}'         # Call tool
  cat input.json | mcp-cli call server tool      # Read from stdin

Environment Variables:
  MCP_CONFIG_PATH   Path to config file
  MCP_DEBUG         Enable debug output (default: false)
  MCP_TIMEOUT       Request timeout in seconds (default: 1800)
  MCP_CONCURRENCY   Max parallel connections (default: 5)
  MCP_MAX_RETRIES   Max retry attempts (default: 3)
  MCP_RETRY_DELAY   Base retry delay in ms (default: 1000)
  MCP_STRICT_ENV    Error on missing env vars (default: true)
  MCP_NO_DAEMON     Disable daemon mode (default: false, set to 1)
  MCP_DAEMON_TIMEOUT  Daemon idle timeout in seconds (default: 60)
  NO_COLOR          Disable colored output
"#, VERSION);
}

#[tokio::main]
async fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();

    // Handle daemon mode
    if args.first().map(|a| a.as_str()) == Some("--daemon") {
        let server_name = args.get(1).cloned().unwrap_or_default();
        let config_json = args.get(2).cloned().unwrap_or_default();

        if server_name.is_empty() || config_json.is_empty() {
            eprintln!("Usage: mcp-cli --daemon <serverName> <configJson>");
            std::process::exit(1);
        }

        let server_config: config::ServerConfig = match serde_json::from_str(&config_json) {
            Ok(c) => c,
            Err(e) => {
                eprintln!("Invalid config JSON: {}", e);
                std::process::exit(1);
            }
        };

        daemon::run_daemon(&server_name, server_config).await;
        return;
    }

    let parsed = parse_args(args);

    let exit_code = match parsed.command.as_str() {
        "help" => {
            print_help();
            0
        }
        "version" => {
            println!("{}", VERSION);
            0
        }
        "list" => {
            match commands::list_command(parsed.config_path.as_deref(), parsed.with_descriptions).await {
                Ok(()) => 0,
                Err(e) => {
                    eprintln!("{}", format_cli_error(&e));
                    e.code
                }
            }
        }
        "info" => {
            let server = parsed.server.as_deref().unwrap_or("");
            match commands::info_command(
                server,
                parsed.tool.as_deref(),
                parsed.config_path.as_deref(),
                parsed.with_descriptions,
            ).await {
                Ok(()) => 0,
                Err(e) => {
                    eprintln!("{}", format_cli_error(&e));
                    e.code
                }
            }
        }
        "grep" => {
            let pattern = parsed.pattern.as_deref().unwrap_or("");
            match commands::grep_command(pattern, parsed.config_path.as_deref(), parsed.with_descriptions).await {
                Ok(()) => 0,
                Err(e) => {
                    eprintln!("{}", format_cli_error(&e));
                    e.code
                }
            }
        }
        "call" => {
            let server = parsed.server.as_deref().unwrap_or("");
            let tool = parsed.tool.as_deref().unwrap_or("");
            match commands::call_command(
                server,
                tool,
                parsed.args.as_deref(),
                parsed.config_path.as_deref(),
            ).await {
                Ok(()) => 0,
                Err(e) => {
                    eprintln!("{}", format_cli_error(&e));
                    e.code
                }
            }
        }
        _ => {
            print_help();
            ERROR_CODE_CLIENT_ERROR
        }
    };

    std::process::exit(exit_code);
}

#[cfg(test)]
mod tests {
    use super::*;

    fn args(input: &[&str]) -> Vec<String> {
        input.iter().map(|s| s.to_string()).collect()
    }

    #[test]
    fn test_parse_help() {
        let parsed = parse_args(args(&["-h"]));
        assert_eq!(parsed.command, "help");

        let parsed = parse_args(args(&["--help"]));
        assert_eq!(parsed.command, "help");
    }

    #[test]
    fn test_parse_version() {
        let parsed = parse_args(args(&["-v"]));
        assert_eq!(parsed.command, "version");

        let parsed = parse_args(args(&["--version"]));
        assert_eq!(parsed.command, "version");
    }

    #[test]
    fn test_parse_list() {
        let parsed = parse_args(args(&[]));
        assert_eq!(parsed.command, "list");
    }

    #[test]
    fn test_parse_list_with_descriptions() {
        let parsed = parse_args(args(&["-d"]));
        assert_eq!(parsed.command, "list");
        assert!(parsed.with_descriptions);
    }

    #[test]
    fn test_parse_grep() {
        let parsed = parse_args(args(&["grep", "*file*"]));
        assert_eq!(parsed.command, "grep");
        assert_eq!(parsed.pattern.as_deref(), Some("*file*"));
    }

    #[test]
    fn test_parse_call_space_format() {
        let parsed = parse_args(args(&["call", "server", "tool", r#"{"key":"val"}"#]));
        assert_eq!(parsed.command, "call");
        assert_eq!(parsed.server.as_deref(), Some("server"));
        assert_eq!(parsed.tool.as_deref(), Some("tool"));
        assert!(parsed.args.is_some());
    }

    #[test]
    fn test_parse_call_slash_format() {
        let parsed = parse_args(args(&["call", "server/tool", r#"{"key":"val"}"#]));
        assert_eq!(parsed.command, "call");
        assert_eq!(parsed.server.as_deref(), Some("server"));
        assert_eq!(parsed.tool.as_deref(), Some("tool"));
    }

    #[test]
    fn test_parse_call_stdin_dash() {
        let parsed = parse_args(args(&["call", "server", "tool", "-"]));
        assert_eq!(parsed.command, "call");
        assert!(parsed.args.is_none());
    }

    #[test]
    fn test_parse_config_option() {
        let parsed = parse_args(args(&["-c", "/path/to/config.json", "-d"]));
        assert_eq!(parsed.config_path.as_deref(), Some("/path/to/config.json"));
        assert!(parsed.with_descriptions);
        assert_eq!(parsed.command, "list");
    }

    #[test]
    fn test_parse_info_with_server() {
        let parsed = parse_args(args(&["info", "myserver"]));
        assert_eq!(parsed.command, "info");
        assert_eq!(parsed.server.as_deref(), Some("myserver"));
        assert!(parsed.tool.is_none());
    }

    #[test]
    fn test_parse_info_with_server_and_tool() {
        let parsed = parse_args(args(&["info", "myserver", "mytool"]));
        assert_eq!(parsed.command, "info");
        assert_eq!(parsed.server.as_deref(), Some("myserver"));
        assert_eq!(parsed.tool.as_deref(), Some("mytool"));
    }

    #[test]
    fn test_parse_info_slash_format() {
        let parsed = parse_args(args(&["info", "myserver/mytool"]));
        assert_eq!(parsed.command, "info");
        assert_eq!(parsed.server.as_deref(), Some("myserver"));
        assert_eq!(parsed.tool.as_deref(), Some("mytool"));
    }

    #[test]
    fn test_parse_server_tool_function() {
        let (server, tool) = parse_server_tool(&["server/tool".to_string()]);
        assert_eq!(server, "server");
        assert_eq!(tool.as_deref(), Some("tool"));

        let (server, tool) = parse_server_tool(&["server".to_string(), "tool".to_string()]);
        assert_eq!(server, "server");
        assert_eq!(tool.as_deref(), Some("tool"));

        let (server, tool) = parse_server_tool(&[]);
        assert_eq!(server, "");
        assert!(tool.is_none());
    }

    #[test]
    fn test_is_possible_subcommand() {
        assert!(is_possible_subcommand("run"));
        assert!(is_possible_subcommand("execute"));
        assert!(is_possible_subcommand("exec"));
        assert!(is_possible_subcommand("invoke"));
        assert!(is_possible_subcommand("list"));
        assert!(is_possible_subcommand("ls"));
        assert!(is_possible_subcommand("get"));
        assert!(is_possible_subcommand("show"));
        assert!(is_possible_subcommand("describe"));
        assert!(is_possible_subcommand("search"));
        assert!(is_possible_subcommand("find"));
        assert!(is_possible_subcommand("query"));

        assert!(!is_possible_subcommand("info"));
        assert!(!is_possible_subcommand("grep"));
        assert!(!is_possible_subcommand("call"));
        assert!(!is_possible_subcommand("random"));
    }
}
