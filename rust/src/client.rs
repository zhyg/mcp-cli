use std::time::{Duration, Instant};

use rmcp::model::{CallToolRequestParams, ClientCapabilities, ClientInfo, Implementation};
use rmcp::service::RunningService;
use rmcp::transport::{TokioChildProcess, StreamableHttpClientTransport};
use rmcp::{RoleClient, ServiceExt};
use tokio::process::Command;

use crate::config::*;
use crate::errors::*;

pub async fn connect_to_server(
    server_name: &str,
    config: &ServerConfig,
) -> Result<RunningService<RoleClient, ClientInfo>, CliError> {
    let client_info = ClientInfo::new(
        ClientCapabilities::default(),
        Implementation::new("mcp-cli", VERSION),
    );

    if config.is_http() {
        let url = config.url.as_deref().unwrap();
        debug_log(&format!("{}: connecting via HTTP to {}", server_name, url));

        let transport = StreamableHttpClientTransport::from_uri(url);

        let service = client_info
            .serve(transport)
            .await
            .map_err(|e| server_connection_error(server_name, &format!("{}", e)))?;

        Ok(service)
    } else {
        let cmd = config.command.as_deref().unwrap();
        let cmd_args = config.args.as_deref().unwrap_or(&[]);
        debug_log(&format!("{}: connecting via stdio: {} {}", server_name, cmd, cmd_args.join(" ")));

        let mut command = Command::new(cmd);
        command.args(cmd_args);

        if let Some(ref env_map) = config.env {
            for (k, v) in env_map {
                command.env(k, v);
            }
        }
        if let Some(ref cwd) = config.cwd {
            command.current_dir(cwd);
        }

        let transport = TokioChildProcess::new(command)
            .map_err(|e| server_connection_error(server_name, &e.to_string()))?;

        let service = client_info
            .serve(transport)
            .await
            .map_err(|e| server_connection_error(server_name, &format!("{}", e)))?;

        Ok(service)
    }
}

pub async fn list_tools_from_client(
    client: &RunningService<RoleClient, ClientInfo>,
) -> Result<Vec<ToolInfo>, CliError> {
    let tools = client
        .list_all_tools()
        .await
        .map_err(|e| CliError {
            code: ERROR_CODE_SERVER_ERROR,
            error_type: "LIST_TOOLS_FAILED".to_string(),
            message: format!("Failed to list tools: {}", e),
            details: None,
            suggestion: None,
        })?;

    Ok(tools
        .into_iter()
        .map(|t| {
            let schema = serde_json::to_value(&t.input_schema).unwrap_or(serde_json::json!({}));
            ToolInfo {
                name: t.name.to_string(),
                description: t.description.map(|d| d.to_string()),
                input_schema: schema,
            }
        })
        .collect())
}

pub async fn call_tool_on_client(
    client: &RunningService<RoleClient, ClientInfo>,
    tool_name: &str,
    args: serde_json::Map<String, serde_json::Value>,
) -> Result<serde_json::Value, CliError> {
    let params = CallToolRequestParams::new(tool_name.to_string()).with_arguments(args);
    let result = client
        .call_tool(params)
        .await
        .map_err(|e| CliError {
            code: ERROR_CODE_SERVER_ERROR,
            error_type: "CALL_TOOL_FAILED".to_string(),
            message: format!("Call tool failed: {}", e),
            details: None,
            suggestion: None,
        })?;

    if result.is_error == Some(true) {
        let err_texts: Vec<String> = result
            .content
            .iter()
            .filter_map(|c| {
                let raw = &c.raw;
                if let rmcp::model::RawContent::Text(tc) = raw {
                    Some(tc.text.clone())
                } else {
                    None
                }
            })
            .collect();
        if !err_texts.is_empty() {
            return Err(CliError {
                code: ERROR_CODE_SERVER_ERROR,
                error_type: "TOOL_EXECUTION_FAILED".to_string(),
                message: err_texts.join("\n"),
                details: None,
                suggestion: None,
            });
        }
        return Err(CliError {
            code: ERROR_CODE_SERVER_ERROR,
            error_type: "TOOL_EXECUTION_FAILED".to_string(),
            message: "Tool execution failed".to_string(),
            details: None,
            suggestion: None,
        });
    }

    serde_json::to_value(&result)
        .map_err(|e| CliError {
            code: ERROR_CODE_SERVER_ERROR,
            error_type: "SERIALIZATION_ERROR".to_string(),
            message: format!("Failed to serialize result: {}", e),
            details: None,
            suggestion: None,
        })
}

pub fn get_instructions(
    client: &RunningService<RoleClient, ClientInfo>,
) -> Option<String> {
    client.peer_info().and_then(|info| info.instructions.clone())
}

pub async fn safe_close(client: RunningService<RoleClient, ClientInfo>) {
    if let Err(e) = client.cancel().await {
        debug_log(&format!("Failed to close connection: {}", e));
    }
}

pub fn is_transient_error(msg: &str) -> bool {
    let msg_lower = msg.to_lowercase();

    for code in &["502", "503", "504", "429"] {
        if msg.starts_with(code) {
            return true;
        }
    }

    let patterns = [
        "connection refused",
        "connection reset",
        "timeout",
        "econnrefused",
        "econnreset",
        "etimedout",
    ];
    for pattern in &patterns {
        if msg_lower.contains(pattern) {
            return true;
        }
    }

    false
}

pub async fn connect_with_retry(
    server_name: &str,
    config: &ServerConfig,
) -> Result<RunningService<RoleClient, ClientInfo>, CliError> {
    let max_retries = get_max_retries();
    let base_delay = get_retry_delay_ms();
    let total_budget_ms = get_timeout_ms();
    let start = Instant::now();

    let mut last_err = None;

    for attempt in 0..=max_retries {
        let elapsed = start.elapsed().as_millis() as u64;
        if elapsed >= total_budget_ms {
            debug_log(&format!("{}: timeout budget exhausted after {}ms", server_name, elapsed));
            break;
        }

        match connect_to_server(server_name, config).await {
            Ok(client) => return Ok(client),
            Err(e) => {
                let remaining = total_budget_ms.saturating_sub(start.elapsed().as_millis() as u64);
                let should_retry = attempt < max_retries
                    && is_transient_error(&e.message)
                    && remaining > 1000;

                if should_retry {
                    let delay = calculate_backoff_delay(attempt, base_delay).min(remaining - 1000);
                    debug_log(&format!(
                        "{} failed (attempt {}/{}): {}. Retrying in {}ms...",
                        server_name, attempt + 1, max_retries + 1, e.message, delay
                    ));
                    tokio::time::sleep(Duration::from_millis(delay)).await;
                }

                last_err = Some(e);
                if !should_retry && attempt > 0 {
                    break;
                }
            }
        }
    }

    Err(last_err.unwrap_or_else(|| server_connection_error(server_name, "connection failed")))
}

fn calculate_backoff_delay(attempt: usize, base_delay: u64) -> u64 {
    let exponential = base_delay * 2u64.pow(attempt as u32);
    let capped = exponential.min(10000);
    let jitter = (capped as f64 * 0.25 * (rand_f64() * 2.0 - 1.0)) as i64;
    (capped as i64 + jitter).max(0) as u64
}

fn rand_f64() -> f64 {
    use std::time::SystemTime;
    let nanos = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .unwrap_or_default()
        .subsec_nanos();
    (nanos as f64) / (u32::MAX as f64)
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_is_transient_error() {
        assert!(is_transient_error("502 Bad Gateway"));
        assert!(is_transient_error("503 Service Unavailable"));
        assert!(is_transient_error("504 Gateway Timeout"));
        assert!(is_transient_error("429 Too Many Requests"));
        assert!(is_transient_error("connection refused"));
        assert!(is_transient_error("Connection Reset"));
        assert!(is_transient_error("request timeout"));
        assert!(is_transient_error("ECONNREFUSED"));
        assert!(is_transient_error("ETIMEDOUT"));

        assert!(!is_transient_error("404 Not Found"));
        assert!(!is_transient_error("authentication failed"));
        assert!(!is_transient_error("invalid JSON"));
    }

    #[test]
    fn test_calculate_backoff_delay() {
        let delay = calculate_backoff_delay(0, 1000);
        assert!(delay >= 750 && delay <= 1250);

        let delay = calculate_backoff_delay(1, 1000);
        assert!(delay >= 1500 && delay <= 2500);
    }
}
