use std::fs;
use std::time::Duration;

use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::UnixStream;

use crate::config::*;
use crate::daemon::*;

pub struct DaemonConnection {
    pub server_name: String,
    socket_path: std::path::PathBuf,
}

impl DaemonConnection {
    pub async fn list_tools(&self) -> Result<Vec<ToolInfo>, crate::errors::CliError> {
        let response = self.send_request(DaemonRequest {
            id: generate_request_id(),
            request_type: "listTools".to_string(),
            tool_name: None,
            args: None,
        }).await?;

        if !response.success {
            let msg = response.error.map(|e| e.message).unwrap_or_else(|| "listTools failed".to_string());
            return Err(crate::errors::CliError {
                code: crate::errors::ERROR_CODE_SERVER_ERROR,
                error_type: "DAEMON_ERROR".to_string(),
                message: msg,
                details: None,
                suggestion: None,
            });
        }

        let data = response.data.unwrap_or(serde_json::Value::Array(vec![]));
        serde_json::from_value(data).map_err(|e| crate::errors::CliError {
            code: crate::errors::ERROR_CODE_SERVER_ERROR,
            error_type: "DAEMON_ERROR".to_string(),
            message: format!("Failed to parse daemon response: {}", e),
            details: None,
            suggestion: None,
        })
    }

    pub async fn call_tool(
        &self,
        tool_name: &str,
        args: serde_json::Map<String, serde_json::Value>,
    ) -> Result<serde_json::Value, crate::errors::CliError> {
        let response = self.send_request(DaemonRequest {
            id: generate_request_id(),
            request_type: "callTool".to_string(),
            tool_name: Some(tool_name.to_string()),
            args: Some(args),
        }).await?;

        if !response.success {
            let msg = response.error.map(|e| e.message).unwrap_or_else(|| "callTool failed".to_string());
            return Err(crate::errors::CliError {
                code: crate::errors::ERROR_CODE_SERVER_ERROR,
                error_type: "DAEMON_ERROR".to_string(),
                message: msg,
                details: None,
                suggestion: None,
            });
        }

        Ok(response.data.unwrap_or(serde_json::Value::Null))
    }

    pub async fn get_instructions(&self) -> Result<Option<String>, crate::errors::CliError> {
        let response = self.send_request(DaemonRequest {
            id: generate_request_id(),
            request_type: "getInstructions".to_string(),
            tool_name: None,
            args: None,
        }).await?;

        if !response.success {
            return Ok(None);
        }

        Ok(response.data.and_then(|v| v.as_str().map(String::from)))
    }

    pub async fn close(&self) {
        debug_log(&format!("[daemon-client] Disconnecting from {} daemon", self.server_name));
    }

    async fn send_request(&self, request: DaemonRequest) -> Result<DaemonResponse, crate::errors::CliError> {
        let result = tokio::time::timeout(
            Duration::from_secs(5),
            self.send_request_inner(&request),
        ).await;

        match result {
            Ok(Ok(resp)) => Ok(resp),
            Ok(Err(e)) => Err(e),
            Err(_) => Err(crate::errors::CliError {
                code: crate::errors::ERROR_CODE_NETWORK_ERROR,
                error_type: "DAEMON_TIMEOUT".to_string(),
                message: "Daemon request timeout".to_string(),
                details: None,
                suggestion: None,
            }),
        }
    }

    async fn send_request_inner(&self, request: &DaemonRequest) -> Result<DaemonResponse, crate::errors::CliError> {
        let stream = UnixStream::connect(&self.socket_path).await.map_err(|e| crate::errors::CliError {
            code: crate::errors::ERROR_CODE_NETWORK_ERROR,
            error_type: "DAEMON_CONNECTION_FAILED".to_string(),
            message: format!("Failed to connect to daemon: {}", e),
            details: None,
            suggestion: None,
        })?;

        let (reader, mut writer) = stream.into_split();

        let json = serde_json::to_string(request).unwrap_or_default();
        writer.write_all(format!("{}\n", json).as_bytes()).await.map_err(|e| crate::errors::CliError {
            code: crate::errors::ERROR_CODE_NETWORK_ERROR,
            error_type: "DAEMON_WRITE_ERROR".to_string(),
            message: format!("Failed to write to daemon: {}", e),
            details: None,
            suggestion: None,
        })?;

        let mut buf_reader = BufReader::new(reader);
        let mut line = String::new();
        buf_reader.read_line(&mut line).await.map_err(|e| crate::errors::CliError {
            code: crate::errors::ERROR_CODE_NETWORK_ERROR,
            error_type: "DAEMON_READ_ERROR".to_string(),
            message: format!("Failed to read from daemon: {}", e),
            details: None,
            suggestion: None,
        })?;

        serde_json::from_str(line.trim()).map_err(|e| crate::errors::CliError {
            code: crate::errors::ERROR_CODE_NETWORK_ERROR,
            error_type: "DAEMON_PARSE_ERROR".to_string(),
            message: format!("Invalid response from daemon: {}", e),
            details: None,
            suggestion: None,
        })
    }
}

fn generate_request_id() -> String {
    use std::time::SystemTime;
    let ts = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis();
    format!("{}-{:x}", ts, std::process::id())
}

fn is_daemon_valid(server_name: &str, config: &ServerConfig) -> bool {
    let pid_info = match read_pid_file(server_name) {
        Some(p) => p,
        None => {
            debug_log(&format!("[daemon-client] No PID file for {}", server_name));
            return false;
        }
    };

    if !is_process_running(pid_info.pid) {
        debug_log(&format!("[daemon-client] Process {} not running, cleaning up", pid_info.pid));
        remove_pid_file(server_name);
        remove_socket_file(server_name);
        return false;
    }

    let current_hash = get_config_hash(config);
    if pid_info.config_hash != current_hash {
        debug_log(&format!("[daemon-client] Config hash mismatch for {}, killing old daemon", server_name));
        kill_process(pid_info.pid);
        remove_pid_file(server_name);
        remove_socket_file(server_name);
        return false;
    }

    let socket_path = get_socket_path(server_name);
    if !socket_path.exists() {
        debug_log(&format!("[daemon-client] Socket missing for {}, cleaning up", server_name));
        kill_process(pid_info.pid);
        remove_pid_file(server_name);
        return false;
    }

    true
}

async fn spawn_daemon(server_name: &str, config: &ServerConfig) -> bool {
    debug_log(&format!("[daemon-client] Spawning daemon for {}", server_name));

    let exe = match std::env::current_exe() {
        Ok(e) => e,
        Err(_) => return false,
    };

    let config_json = match serde_json::to_string(config) {
        Ok(j) => j,
        Err(_) => return false,
    };

    let mut child = match tokio::process::Command::new(&exe)
        .arg("--daemon")
        .arg(server_name)
        .arg(&config_json)
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .spawn()
    {
        Ok(c) => c,
        Err(e) => {
            debug_log(&format!("[daemon-client] Failed to spawn daemon: {}", e));
            return false;
        }
    };

    let stdout = match child.stdout.take() {
        Some(s) => s,
        None => return false,
    };

    let result = tokio::time::timeout(Duration::from_secs(5), async {
        let mut reader = BufReader::new(stdout);
        let mut line = String::new();
        loop {
            line.clear();
            match reader.read_line(&mut line).await {
                Ok(0) => return false,
                Ok(_) => {
                    if line.contains("DAEMON_READY") {
                        return true;
                    }
                }
                Err(_) => return false,
            }
        }
    }).await;

    match result {
        Ok(true) => {
            // Daemon is ready, detach it
            drop(child);
            true
        }
        _ => {
            debug_log(&format!("[daemon-client] Daemon spawn timeout for {}", server_name));
            let _ = child.kill().await;
            false
        }
    }
}

pub async fn get_daemon_connection(
    server_name: &str,
    config: &ServerConfig,
) -> Option<DaemonConnection> {
    let socket_path = get_socket_path(server_name);

    if !is_daemon_valid(server_name, config) {
        if !spawn_daemon(server_name, config).await {
            debug_log(&format!("[daemon-client] Failed to spawn daemon for {}", server_name));
            return None;
        }
        tokio::time::sleep(Duration::from_millis(100)).await;
    }

    if !socket_path.exists() {
        debug_log(&format!("[daemon-client] Socket not found after spawn for {}", server_name));
        return None;
    }

    let conn = DaemonConnection {
        server_name: server_name.to_string(),
        socket_path: socket_path.clone(),
    };

    // Ping test
    match conn.send_request(DaemonRequest {
        id: generate_request_id(),
        request_type: "ping".to_string(),
        tool_name: None,
        args: None,
    }).await {
        Ok(resp) if resp.success => {}
        _ => {
            debug_log(&format!("[daemon-client] Ping failed for {}", server_name));
            return None;
        }
    }

    debug_log(&format!("[daemon-client] Connected to daemon for {}", server_name));
    Some(conn)
}

pub async fn cleanup_orphaned_daemons() {
    let socket_dir = get_socket_dir();
    if !socket_dir.exists() {
        return;
    }

    let entries = match fs::read_dir(&socket_dir) {
        Ok(e) => e,
        Err(_) => return,
    };

    for entry in entries.flatten() {
        let path = entry.path();
        if path.extension().map(|e| e == "pid").unwrap_or(false) {
            let server_name = path
                .file_stem()
                .and_then(|s| s.to_str())
                .unwrap_or("")
                .to_string();

            if server_name.is_empty() {
                continue;
            }

            if let Some(pid_info) = read_pid_file(&server_name) {
                if !is_process_running(pid_info.pid) {
                    debug_log(&format!("[daemon-client] Cleaning up orphaned daemon: {}", server_name));
                    remove_pid_file(&server_name);
                    remove_socket_file(&server_name);
                }
            }
        }
    }
}
