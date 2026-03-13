use regex::Regex;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::env;
use std::fs;
use std::path::PathBuf;

use crate::errors::*;

pub const VERSION: &str = "0.3.0";
pub const DEFAULT_TIMEOUT_SECONDS: u64 = 1800;
pub const DEFAULT_CONCURRENCY: usize = 5;
pub const DEFAULT_MAX_RETRIES: usize = 3;
pub const DEFAULT_RETRY_DELAY_MS: u64 = 1000;
pub const DEFAULT_DAEMON_TIMEOUT_SECONDS: u64 = 60;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ServerConfig {
    #[serde(default)]
    pub command: Option<String>,
    #[serde(default)]
    pub args: Option<Vec<String>>,
    #[serde(default)]
    pub env: Option<HashMap<String, String>>,
    #[serde(default)]
    pub cwd: Option<String>,
    #[serde(default)]
    pub url: Option<String>,
    #[serde(default)]
    pub headers: Option<HashMap<String, String>>,
    #[serde(default)]
    #[allow(dead_code)]
    pub timeout: Option<u64>,
    #[serde(default, rename = "allowedTools")]
    pub allowed_tools: Option<Vec<String>>,
    #[serde(default, rename = "disabledTools")]
    pub disabled_tools: Option<Vec<String>>,
}

impl ServerConfig {
    pub fn is_http(&self) -> bool {
        self.url.is_some()
    }

    pub fn is_stdio(&self) -> bool {
        self.command.is_some()
    }
}

#[derive(Debug, Clone, Deserialize)]
pub struct McpServersConfig {
    #[serde(rename = "mcpServers")]
    pub mcp_servers: HashMap<String, Option<ServerConfig>>,
}

#[derive(Debug, Clone)]
pub struct ToolInfo {
    pub name: String,
    pub description: Option<String>,
    pub input_schema: serde_json::Value,
}

pub fn debug_log(message: &str) {
    if env::var("MCP_DEBUG").is_ok() {
        eprintln!("[mcp-cli] {}", message);
    }
}

pub fn get_timeout_ms() -> u64 {
    if let Ok(v) = env::var("MCP_TIMEOUT") {
        if let Ok(secs) = v.parse::<u64>() {
            if secs > 0 {
                return secs * 1000;
            }
        }
    }
    DEFAULT_TIMEOUT_SECONDS * 1000
}

pub fn get_concurrency_limit() -> usize {
    if let Ok(v) = env::var("MCP_CONCURRENCY") {
        if let Ok(limit) = v.parse::<usize>() {
            if limit > 0 {
                return limit;
            }
        }
    }
    DEFAULT_CONCURRENCY
}

pub fn get_max_retries() -> usize {
    if let Ok(v) = env::var("MCP_MAX_RETRIES") {
        if let Ok(retries) = v.parse::<usize>() {
            return retries;
        }
    }
    DEFAULT_MAX_RETRIES
}

pub fn get_retry_delay_ms() -> u64 {
    if let Ok(v) = env::var("MCP_RETRY_DELAY") {
        if let Ok(delay) = v.parse::<u64>() {
            if delay > 0 {
                return delay;
            }
        }
    }
    DEFAULT_RETRY_DELAY_MS
}

fn is_strict_env_mode() -> bool {
    match env::var("MCP_STRICT_ENV") {
        Ok(v) => {
            let v = v.to_lowercase();
            v != "false" && v != "0"
        }
        Err(_) => true,
    }
}

fn substitute_env_vars(value: &str) -> Result<String, CliError> {
    let re = Regex::new(r"\$\{([^}]+)\}").unwrap();
    let mut missing_vars = Vec::new();

    let result = re.replace_all(value, |caps: &regex::Captures| {
        let var_name = &caps[1];
        match env::var(var_name) {
            Ok(v) => v,
            Err(_) => {
                missing_vars.push(var_name.to_string());
                String::new()
            }
        }
    });

    if !missing_vars.is_empty() {
        let var_list = missing_vars.join(", ");
        let msg = format!("Missing environment variable(s): {}", var_list);
        if is_strict_env_mode() {
            return Err(CliError {
                code: ERROR_CODE_CLIENT_ERROR,
                error_type: "MISSING_ENV_VAR".to_string(),
                message: msg,
                details: Some("Referenced in config but not set in environment".to_string()),
                suggestion: Some(format!(
                    "Set the variable(s) before running: export {}=\"value\" or set MCP_STRICT_ENV=false to use empty values",
                    missing_vars[0]
                )),
            });
        }
        eprintln!("Warning: {} (using empty values)", msg);
    }

    Ok(result.into_owned())
}

fn substitute_env_vars_in_config(config: &mut McpServersConfig) -> Result<(), CliError> {
    for (name, server_opt) in config.mcp_servers.iter_mut() {
        let server = match server_opt {
            Some(s) => s,
            None => continue,
        };
        if let Some(ref mut cmd) = server.command {
            *cmd = substitute_env_vars(cmd)
                .map_err(|e| CliError { message: format!("server {:?} command: {}", name, e.message), ..e })?;
        }
        if let Some(ref mut args) = server.args {
            for (i, arg) in args.iter_mut().enumerate() {
                *arg = substitute_env_vars(arg)
                    .map_err(|e| CliError { message: format!("server {:?} args[{}]: {}", name, i, e.message), ..e })?;
            }
        }
        if let Some(ref mut env_map) = server.env {
            for (key, val) in env_map.iter_mut() {
                *val = substitute_env_vars(val)
                    .map_err(|e| CliError { message: format!("server {:?} env[{}]: {}", name, key, e.message), ..e })?;
            }
        }
        if let Some(ref mut url) = server.url {
            *url = substitute_env_vars(url)
                .map_err(|e| CliError { message: format!("server {:?} url: {}", name, e.message), ..e })?;
        }
        if let Some(ref mut headers) = server.headers {
            for (key, val) in headers.iter_mut() {
                *val = substitute_env_vars(val)
                    .map_err(|e| CliError { message: format!("server {:?} headers[{}]: {}", name, key, e.message), ..e })?;
            }
        }
        if let Some(ref mut cwd) = server.cwd {
            *cwd = substitute_env_vars(cwd)
                .map_err(|e| CliError { message: format!("server {:?} cwd: {}", name, e.message), ..e })?;
        }
    }
    Ok(())
}

fn resolve_config_path(config_path: Option<&str>) -> Result<PathBuf, CliError> {
    let mut candidates = Vec::new();

    if let Some(p) = config_path {
        candidates.push(p.to_string());
    }
    if let Ok(v) = env::var("MCP_CONFIG_PATH") {
        candidates.push(v);
    }

    for p in &candidates {
        let abs = fs::canonicalize(p).unwrap_or_else(|_| PathBuf::from(p));
        if abs.exists() {
            return Ok(abs);
        }
        return Err(config_not_found_error(&abs.to_string_lossy()));
    }

    let home = dirs_home();
    let search_paths: Vec<PathBuf> = vec![
        PathBuf::from("./mcp_servers.json"),
        home.join(".mcp_servers.json"),
        home.join(".config/mcp/mcp_servers.json"),
    ];

    for p in &search_paths {
        let abs = fs::canonicalize(p).unwrap_or_else(|_| p.clone());
        if abs.exists() {
            return Ok(abs);
        }
    }

    Err(config_search_error())
}

fn dirs_home() -> PathBuf {
    env::var("HOME")
        .map(PathBuf::from)
        .unwrap_or_else(|_| PathBuf::from("/"))
}

pub fn load_config(config_path: Option<&str>) -> Result<(McpServersConfig, String), CliError> {
    let path = resolve_config_path(config_path)?;
    let path_str = path.to_string_lossy().to_string();

    let data = fs::read_to_string(&path)
        .map_err(|_| config_not_found_error(&path_str))?;

    let mut config: McpServersConfig = serde_json::from_str(&data)
        .map_err(|e| config_invalid_json_error(&path_str, &e.to_string()))?;

    if config.mcp_servers.is_empty() {
        return Err(config_missing_field_error(&path_str));
    }

    for (name, server_opt) in &config.mcp_servers {
        match server_opt {
            None => {
                return Err(CliError {
                    code: ERROR_CODE_CLIENT_ERROR,
                    error_type: "CONFIG_INVALID_SERVER".to_string(),
                    message: format!("Invalid server configuration for {:?}: value is null", name),
                    details: Some(format!("File: {}", path_str)),
                    suggestion: Some(r#"Each server must have either "command" (stdio) or "url" (HTTP) field"#.to_string()),
                });
            }
            Some(server) => {
                let has_command = server.command.is_some();
                let has_url = server.url.is_some();
                if !has_command && !has_url {
                    return Err(CliError {
                        code: ERROR_CODE_CLIENT_ERROR,
                        error_type: "CONFIG_INVALID_SERVER".to_string(),
                        message: format!("Server {:?} missing required field: must have either \"command\" or \"url\"", name),
                        details: Some(format!("File: {}", path_str)),
                        suggestion: Some(r#"Add "command" for stdio servers or "url" for HTTP servers"#.to_string()),
                    });
                }
                if has_command && has_url {
                    return Err(CliError {
                        code: ERROR_CODE_CLIENT_ERROR,
                        error_type: "CONFIG_INVALID_SERVER".to_string(),
                        message: format!("Server {:?} has both \"command\" and \"url\" - pick one", name),
                        details: Some(format!("File: {}", path_str)),
                        suggestion: Some(r#"Use "command" for local stdio servers or "url" for remote HTTP servers, not both"#.to_string()),
                    });
                }
            }
        }
    }

    substitute_env_vars_in_config(&mut config)?;

    Ok((config, path_str))
}

pub fn get_server_config<'a>(config: &'a McpServersConfig, server_name: &str) -> Result<&'a ServerConfig, CliError> {
    match config.mcp_servers.get(server_name) {
        Some(Some(server)) => Ok(server),
        _ => {
            let available = list_server_names(config);
            Err(server_not_found_error(server_name, &available))
        }
    }
}

pub fn list_server_names(config: &McpServersConfig) -> Vec<String> {
    config.mcp_servers.keys().cloned().collect()
}

// --- Tool Filtering ---

fn matches_glob_pattern(name: &str, pattern: &str) -> bool {
    let mut regex_pattern = regex::escape(pattern);
    regex_pattern = regex_pattern.replace(r"\*", ".*");
    regex_pattern = regex_pattern.replace(r"\?", ".");
    if let Ok(re) = Regex::new(&format!("(?i)^{}$", regex_pattern)) {
        re.is_match(name)
    } else {
        false
    }
}

fn matches_any_pattern(name: &str, patterns: &[String]) -> bool {
    patterns.iter().any(|p| matches_glob_pattern(name, p))
}

pub fn filter_tools(tools: Vec<ToolInfo>, config: &ServerConfig) -> Vec<ToolInfo> {
    let allowed = config.allowed_tools.as_deref().unwrap_or(&[]);
    let disabled = config.disabled_tools.as_deref().unwrap_or(&[]);

    if allowed.is_empty() && disabled.is_empty() {
        return tools;
    }

    tools
        .into_iter()
        .filter(|tool| {
            if !disabled.is_empty() && matches_any_pattern(&tool.name, disabled) {
                return false;
            }
            if !allowed.is_empty() && !matches_any_pattern(&tool.name, allowed) {
                return false;
            }
            true
        })
        .collect()
}

pub fn is_tool_allowed(tool_name: &str, config: &ServerConfig) -> bool {
    let disabled = config.disabled_tools.as_deref().unwrap_or(&[]);
    let allowed = config.allowed_tools.as_deref().unwrap_or(&[]);

    if !disabled.is_empty() && matches_any_pattern(tool_name, disabled) {
        return false;
    }
    if !allowed.is_empty() && !matches_any_pattern(tool_name, allowed) {
        return false;
    }
    true
}

// --- Daemon Configuration ---

pub fn is_daemon_enabled() -> bool {
    env::var("MCP_NO_DAEMON").map(|v| v != "1").unwrap_or(true)
}

pub fn get_daemon_timeout_ms() -> u64 {
    if let Ok(v) = env::var("MCP_DAEMON_TIMEOUT") {
        if let Ok(secs) = v.parse::<u64>() {
            if secs > 0 {
                return secs * 1000;
            }
        }
    }
    DEFAULT_DAEMON_TIMEOUT_SECONDS * 1000
}

pub fn get_socket_dir() -> PathBuf {
    let uid = unsafe { libc::getuid() };
    PathBuf::from(format!("/tmp/mcp-cli-{}", uid))
}

pub fn get_socket_path(server_name: &str) -> PathBuf {
    get_socket_dir().join(format!("{}.sock", server_name))
}

pub fn get_pid_path(server_name: &str) -> PathBuf {
    get_socket_dir().join(format!("{}.pid", server_name))
}

pub fn get_config_hash(config: &ServerConfig) -> String {
    let value = serde_json::to_value(config).unwrap_or_default();
    let s = value.to_string();
    use std::hash::{Hash, Hasher};
    let mut hasher = std::collections::hash_map::DefaultHasher::new();
    s.hash(&mut hasher);
    format!("{:016x}", hasher.finish())
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::io::Write;

    fn make_tool(name: &str) -> ToolInfo {
        ToolInfo {
            name: name.to_string(),
            description: None,
            input_schema: serde_json::json!({}),
        }
    }

    fn make_config(allowed: Option<Vec<&str>>, disabled: Option<Vec<&str>>) -> ServerConfig {
        ServerConfig {
            command: Some("test".to_string()),
            args: None,
            env: None,
            cwd: None,
            url: None,
            headers: None,
            timeout: None,
            allowed_tools: allowed.map(|v| v.into_iter().map(String::from).collect()),
            disabled_tools: disabled.map(|v| v.into_iter().map(String::from).collect()),
        }
    }

    #[test]
    fn test_filter_no_filtering() {
        let tools = vec![make_tool("a"), make_tool("b")];
        let config = make_config(None, None);
        let result = filter_tools(tools, &config);
        assert_eq!(result.len(), 2);
    }

    #[test]
    fn test_filter_allowed_exact() {
        let tools = vec![make_tool("read_file"), make_tool("write_file"), make_tool("delete_file")];
        let config = make_config(Some(vec!["read_file"]), None);
        let result = filter_tools(tools, &config);
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].name, "read_file");
    }

    #[test]
    fn test_filter_allowed_wildcard() {
        let tools = vec![
            make_tool("read_file"),
            make_tool("write_file"),
            make_tool("list_directory"),
            make_tool("search_files"),
            make_tool("delete_file"),
        ];
        let config = make_config(Some(vec!["*file*"]), None);
        let result = filter_tools(tools, &config);
        assert_eq!(result.len(), 4);
    }

    #[test]
    fn test_filter_allowed_prefix() {
        let tools = vec![make_tool("read_file"), make_tool("write_file")];
        let config = make_config(Some(vec!["read_*"]), None);
        let result = filter_tools(tools, &config);
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].name, "read_file");
    }

    #[test]
    fn test_filter_disabled_exact() {
        let tools = vec![make_tool("read_file"), make_tool("delete_file")];
        let config = make_config(None, Some(vec!["delete_file"]));
        let result = filter_tools(tools, &config);
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].name, "read_file");
    }

    #[test]
    fn test_filter_disabled_wildcard() {
        let tools = vec![make_tool("read_file"), make_tool("write_file"), make_tool("list_dir")];
        let config = make_config(None, Some(vec!["*file"]));
        let result = filter_tools(tools, &config);
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].name, "list_dir");
    }

    #[test]
    fn test_filter_disabled_takes_precedence() {
        let tools = vec![make_tool("read_file"), make_tool("delete_file")];
        let config = make_config(Some(vec!["*file"]), Some(vec!["delete_file"]));
        let result = filter_tools(tools, &config);
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].name, "read_file");
    }

    #[test]
    fn test_filter_case_insensitive() {
        let tools = vec![make_tool("READ_FILE"), make_tool("write_file")];
        let config = make_config(Some(vec!["read_file"]), None);
        let result = filter_tools(tools, &config);
        assert_eq!(result.len(), 1);
        assert_eq!(result[0].name, "READ_FILE");
    }

    #[test]
    fn test_filter_question_mark() {
        let tools = vec![make_tool("file1"), make_tool("file2"), make_tool("file10")];
        let config = make_config(Some(vec!["file?"]), None);
        let result = filter_tools(tools, &config);
        assert_eq!(result.len(), 2);
    }

    #[test]
    fn test_is_tool_allowed_basic() {
        let config = make_config(Some(vec!["read_*"]), Some(vec!["read_secret"]));
        assert!(is_tool_allowed("read_file", &config));
        assert!(!is_tool_allowed("write_file", &config));
        assert!(!is_tool_allowed("read_secret", &config));
    }

    #[test]
    fn test_server_config_type_detection() {
        let stdio = ServerConfig {
            command: Some("node".to_string()),
            args: None, env: None, cwd: None,
            url: None, headers: None, timeout: None,
            allowed_tools: None, disabled_tools: None,
        };
        assert!(stdio.is_stdio());
        assert!(!stdio.is_http());

        let http = ServerConfig {
            command: None, args: None, env: None, cwd: None,
            url: Some("https://example.com".to_string()),
            headers: None, timeout: None,
            allowed_tools: None, disabled_tools: None,
        };
        assert!(http.is_http());
        assert!(!http.is_stdio());
    }

    #[test]
    fn test_load_config_valid() {
        let dir = tempfile::tempdir().unwrap();
        let cfg_path = dir.path().join("mcp_servers.json");
        let mut f = fs::File::create(&cfg_path).unwrap();
        writeln!(f, r#"{{"mcpServers": {{"test": {{"command": "echo", "args": ["hello"]}}}}}}"#).unwrap();

        let (config, _) = load_config(Some(cfg_path.to_str().unwrap())).unwrap();
        assert!(config.mcp_servers.contains_key("test"));
        let server = config.mcp_servers.get("test").unwrap().as_ref().unwrap();
        assert_eq!(server.command.as_deref(), Some("echo"));
    }

    #[test]
    fn test_load_config_not_found() {
        let err = load_config(Some("/nonexistent/path.json")).unwrap_err();
        assert!(err.message.contains("not found") || err.error_type == "CONFIG_NOT_FOUND");
    }

    #[test]
    fn test_load_config_invalid_json() {
        let dir = tempfile::tempdir().unwrap();
        let cfg_path = dir.path().join("bad.json");
        fs::write(&cfg_path, "not json").unwrap();

        let err = load_config(Some(cfg_path.to_str().unwrap())).unwrap_err();
        assert_eq!(err.error_type, "CONFIG_INVALID_JSON");
    }

    #[test]
    fn test_load_config_missing_mcp_servers() {
        let dir = tempfile::tempdir().unwrap();
        let cfg_path = dir.path().join("empty.json");
        fs::write(&cfg_path, r#"{"mcpServers": {}}"#).unwrap();

        let err = load_config(Some(cfg_path.to_str().unwrap())).unwrap_err();
        assert!(err.message.contains("mcpServers") || err.error_type == "CONFIG_MISSING_FIELD");
    }

    #[test]
    fn test_load_config_null_server() {
        let dir = tempfile::tempdir().unwrap();
        let cfg_path = dir.path().join("null.json");
        fs::write(&cfg_path, r#"{"mcpServers": {"s": null}}"#).unwrap();

        let err = load_config(Some(cfg_path.to_str().unwrap())).unwrap_err();
        assert!(err.message.contains("null") || err.error_type == "CONFIG_INVALID_SERVER");
    }

    #[test]
    fn test_load_config_missing_command_and_url() {
        let dir = tempfile::tempdir().unwrap();
        let cfg_path = dir.path().join("no_cmd.json");
        fs::write(&cfg_path, r#"{"mcpServers": {"s": {}}}"#).unwrap();

        let err = load_config(Some(cfg_path.to_str().unwrap())).unwrap_err();
        assert!(err.message.contains("missing required field") || err.error_type == "CONFIG_INVALID_SERVER");
    }

    #[test]
    fn test_load_config_both_command_and_url() {
        let dir = tempfile::tempdir().unwrap();
        let cfg_path = dir.path().join("both.json");
        fs::write(&cfg_path, r#"{"mcpServers": {"s": {"command": "echo", "url": "http://x"}}}"#).unwrap();

        let err = load_config(Some(cfg_path.to_str().unwrap())).unwrap_err();
        assert!(err.message.contains("both") || err.error_type == "CONFIG_INVALID_SERVER");
    }

    #[test]
    fn test_env_var_substitution() {
        unsafe { env::set_var("MCP_TEST_VAR_XYZ", "hello"); }
        let result = substitute_env_vars("prefix_${MCP_TEST_VAR_XYZ}_suffix").unwrap();
        assert_eq!(result, "prefix_hello_suffix");
        unsafe { env::remove_var("MCP_TEST_VAR_XYZ"); }
    }

    #[test]
    fn test_env_var_strict_and_non_strict_mode() {
        // Test strict mode (default) then non-strict in same test to avoid race
        unsafe { env::remove_var("MCP_STRICT_ENV"); }
        unsafe { env::remove_var("NONEXISTENT_VAR_12345"); }
        let err = substitute_env_vars("${NONEXISTENT_VAR_12345}").unwrap_err();
        assert_eq!(err.error_type, "MISSING_ENV_VAR");

        // Test non-strict mode
        unsafe { env::set_var("MCP_STRICT_ENV", "false"); }
        unsafe { env::remove_var("NONEXISTENT_VAR_67890"); }
        let result = substitute_env_vars("${NONEXISTENT_VAR_67890}").unwrap();
        assert_eq!(result, "");
        unsafe { env::remove_var("MCP_STRICT_ENV"); }
    }

    #[test]
    fn test_get_server_config_found() {
        let dir = tempfile::tempdir().unwrap();
        let cfg_path = dir.path().join("cfg.json");
        fs::write(&cfg_path, r#"{"mcpServers": {"myserver": {"command": "echo"}}}"#).unwrap();

        let (config, _) = load_config(Some(cfg_path.to_str().unwrap())).unwrap();
        let server = get_server_config(&config, "myserver").unwrap();
        assert_eq!(server.command.as_deref(), Some("echo"));
    }

    #[test]
    fn test_get_server_config_not_found() {
        let dir = tempfile::tempdir().unwrap();
        let cfg_path = dir.path().join("cfg.json");
        fs::write(&cfg_path, r#"{"mcpServers": {"myserver": {"command": "echo"}}}"#).unwrap();

        let (config, _) = load_config(Some(cfg_path.to_str().unwrap())).unwrap();
        let err = get_server_config(&config, "unknown").unwrap_err();
        assert_eq!(err.error_type, "SERVER_NOT_FOUND");
    }

    #[test]
    fn test_list_server_names() {
        let dir = tempfile::tempdir().unwrap();
        let cfg_path = dir.path().join("cfg.json");
        fs::write(&cfg_path, r#"{"mcpServers": {"a": {"command": "x"}, "b": {"url": "http://y"}}}"#).unwrap();

        let (config, _) = load_config(Some(cfg_path.to_str().unwrap())).unwrap();
        let names = list_server_names(&config);
        assert_eq!(names.len(), 2);
        assert!(names.contains(&"a".to_string()));
        assert!(names.contains(&"b".to_string()));
    }

    #[test]
    fn test_env_config_functions() {
        // Timeout
        unsafe { env::remove_var("MCP_TIMEOUT"); }
        assert_eq!(get_timeout_ms(), DEFAULT_TIMEOUT_SECONDS * 1000);
        unsafe { env::set_var("MCP_TIMEOUT", "60"); }
        assert_eq!(get_timeout_ms(), 60000);
        unsafe { env::set_var("MCP_TIMEOUT", "abc"); }
        assert_eq!(get_timeout_ms(), DEFAULT_TIMEOUT_SECONDS * 1000);
        unsafe { env::remove_var("MCP_TIMEOUT"); }

        // Concurrency
        unsafe { env::remove_var("MCP_CONCURRENCY"); }
        assert_eq!(get_concurrency_limit(), DEFAULT_CONCURRENCY);
        unsafe { env::set_var("MCP_CONCURRENCY", "10"); }
        assert_eq!(get_concurrency_limit(), 10);
        unsafe { env::remove_var("MCP_CONCURRENCY"); }

        // Max retries
        unsafe { env::remove_var("MCP_MAX_RETRIES"); }
        assert_eq!(get_max_retries(), DEFAULT_MAX_RETRIES);
        unsafe { env::set_var("MCP_MAX_RETRIES", "0"); }
        assert_eq!(get_max_retries(), 0);
        unsafe { env::remove_var("MCP_MAX_RETRIES"); }

        // Retry delay
        unsafe { env::remove_var("MCP_RETRY_DELAY"); }
        assert_eq!(get_retry_delay_ms(), DEFAULT_RETRY_DELAY_MS);
        unsafe { env::set_var("MCP_RETRY_DELAY", "2000"); }
        assert_eq!(get_retry_delay_ms(), 2000);
        unsafe { env::remove_var("MCP_RETRY_DELAY"); }
    }

    #[test]
    fn test_is_http_and_is_stdio() {
        let dir = tempfile::tempdir().unwrap();
        let cfg_path = dir.path().join("cfg.json");
        fs::write(&cfg_path, r#"{"mcpServers": {"http_s": {"url": "http://x"}, "stdio_s": {"command": "echo"}}}"#).unwrap();

        let (config, _) = load_config(Some(cfg_path.to_str().unwrap())).unwrap();
        let http_s = get_server_config(&config, "http_s").unwrap();
        assert!(http_s.is_http());
        assert!(!http_s.is_stdio());

        let stdio_s = get_server_config(&config, "stdio_s").unwrap();
        assert!(stdio_s.is_stdio());
        assert!(!stdio_s.is_http());
    }
}
