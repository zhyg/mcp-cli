use std::fs;
use std::sync::Arc;

use serde::{Deserialize, Serialize};
use tokio::io::{AsyncBufReadExt, AsyncWriteExt, BufReader};
use tokio::net::UnixListener;
use tokio::sync::Mutex;

use crate::client::*;
use crate::config::*;

#[derive(Debug, Serialize, Deserialize)]
pub struct DaemonRequest {
    pub id: String,
    #[serde(rename = "type")]
    pub request_type: String,
    #[serde(rename = "toolName")]
    pub tool_name: Option<String>,
    pub args: Option<serde_json::Map<String, serde_json::Value>>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct DaemonResponse {
    pub id: String,
    pub success: bool,
    pub data: Option<serde_json::Value>,
    pub error: Option<DaemonError>,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct DaemonError {
    pub code: String,
    pub message: String,
}

#[derive(Debug, Serialize, Deserialize)]
pub struct PidFileContent {
    pub pid: u32,
    #[serde(rename = "configHash")]
    pub config_hash: String,
    #[serde(rename = "startedAt")]
    pub started_at: String,
}

pub fn write_pid_file(server_name: &str, config_hash: &str) {
    let pid_path = get_pid_path(server_name);
    let dir = pid_path.parent().unwrap();

    if !dir.exists() {
        let _ = fs::create_dir_all(dir);
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let _ = fs::set_permissions(dir, fs::Permissions::from_mode(0o700));
        }
    }

    let content = PidFileContent {
        pid: std::process::id(),
        config_hash: config_hash.to_string(),
        started_at: chrono_now(),
    };

    if let Ok(json) = serde_json::to_string(&content) {
        let _ = fs::write(&pid_path, json);
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let _ = fs::set_permissions(&pid_path, fs::Permissions::from_mode(0o600));
        }
    }
}

fn chrono_now() -> String {
    use std::time::SystemTime;
    let duration = SystemTime::now()
        .duration_since(SystemTime::UNIX_EPOCH)
        .unwrap_or_default();
    format!("{}s", duration.as_secs())
}

pub fn read_pid_file(server_name: &str) -> Option<PidFileContent> {
    let pid_path = get_pid_path(server_name);
    let data = fs::read_to_string(pid_path).ok()?;
    serde_json::from_str(&data).ok()
}

pub fn remove_pid_file(server_name: &str) {
    let _ = fs::remove_file(get_pid_path(server_name));
}

pub fn remove_socket_file(server_name: &str) {
    let _ = fs::remove_file(get_socket_path(server_name));
}

pub fn is_process_running(pid: u32) -> bool {
    unsafe { libc::kill(pid as i32, 0) == 0 }
}

pub fn kill_process(pid: u32) -> bool {
    unsafe { libc::kill(pid as i32, libc::SIGTERM) == 0 }
}

pub async fn run_daemon(server_name: &str, config: ServerConfig) {
    let socket_path = get_socket_path(server_name);
    let config_hash = get_config_hash(&config);
    let timeout_ms = get_daemon_timeout_ms();

    let socket_dir = get_socket_dir();
    if !socket_dir.exists() {
        let _ = fs::create_dir_all(&socket_dir);
        #[cfg(unix)]
        {
            use std::os::unix::fs::PermissionsExt;
            let _ = fs::set_permissions(&socket_dir, fs::Permissions::from_mode(0o700));
        }
    }

    // Remove stale socket
    remove_socket_file(server_name);

    // Write PID file
    write_pid_file(server_name, &config_hash);

    // Connect to MCP server
    debug_log(&format!("[daemon:{}] Connecting to MCP server...", server_name));
    let client = match connect_to_server(server_name, &config).await {
        Ok(c) => c,
        Err(e) => {
            eprintln!("[daemon:{}] Failed to connect: {}", server_name, e.message);
            cleanup_daemon_files(server_name);
            std::process::exit(1);
        }
    };
    debug_log(&format!("[daemon:{}] Connected to MCP server", server_name));

    let client = Arc::new(client);
    let server_name_owned = server_name.to_string();

    // Idle timer
    let idle_deadline = Arc::new(Mutex::new(tokio::time::Instant::now() + tokio::time::Duration::from_millis(timeout_ms)));

    // Start Unix socket listener
    let listener = match UnixListener::bind(&socket_path) {
        Ok(l) => l,
        Err(e) => {
            eprintln!("[daemon:{}] Failed to bind socket: {}", server_name, e);
            cleanup_daemon_files(server_name);
            std::process::exit(1);
        }
    };

    // Signal readiness
    println!("DAEMON_READY");

    let sn = server_name_owned.clone();
    let deadline = idle_deadline.clone();
    let client_ref = client.clone();

    // Spawn idle timeout checker
    let sn_timeout = sn.clone();
    let deadline_timeout = deadline.clone();
    tokio::spawn(async move {
        loop {
            tokio::time::sleep(tokio::time::Duration::from_secs(1)).await;
            let dl = *deadline_timeout.lock().await;
            if tokio::time::Instant::now() >= dl {
                debug_log(&format!("[daemon:{}] Idle timeout reached, shutting down", sn_timeout));
                cleanup_daemon_files(&sn_timeout);
                std::process::exit(0);
            }
        }
    });

    // Accept connections
    loop {
        match listener.accept().await {
            Ok((stream, _)) => {
                let client = client_ref.clone();
                let sn = sn.clone();
                let deadline = deadline.clone();
                let timeout_ms = timeout_ms;

                tokio::spawn(async move {
                    // Reset idle timer
                    {
                        let mut dl = deadline.lock().await;
                        *dl = tokio::time::Instant::now() + tokio::time::Duration::from_millis(timeout_ms);
                    }

                    let (reader, mut writer) = stream.into_split();
                    let mut buf_reader = BufReader::new(reader);
                    let mut line = String::new();

                    if buf_reader.read_line(&mut line).await.is_err() {
                        return;
                    }

                    let response = handle_request(&sn, &client, line.trim()).await;
                    if let Ok(json) = serde_json::to_string(&response) {
                        let _ = writer.write_all(format!("{}\n", json).as_bytes()).await;
                    }

                    // Handle close request
                    if response.data.as_ref().map(|d| d.as_str() == Some("closing")).unwrap_or(false) {
                        tokio::time::sleep(tokio::time::Duration::from_millis(100)).await;
                        cleanup_daemon_files(&sn);
                        std::process::exit(0);
                    }
                });
            }
            Err(e) => {
                debug_log(&format!("[daemon:{}] Accept error: {}", sn, e));
            }
        }
    }
}

async fn handle_request(
    server_name: &str,
    client: &rmcp::service::RunningService<rmcp::RoleClient, rmcp::model::ClientInfo>,
    data: &str,
) -> DaemonResponse {
    let request: DaemonRequest = match serde_json::from_str(data) {
        Ok(r) => r,
        Err(_) => {
            return DaemonResponse {
                id: "unknown".to_string(),
                success: false,
                data: None,
                error: Some(DaemonError {
                    code: "INVALID_REQUEST".to_string(),
                    message: "Invalid JSON".to_string(),
                }),
            };
        }
    };

    debug_log(&format!("[daemon:{}] Request: {} ({})", server_name, request.request_type, request.id));

    match request.request_type.as_str() {
        "ping" => DaemonResponse {
            id: request.id,
            success: true,
            data: Some(serde_json::Value::String("pong".to_string())),
            error: None,
        },

        "listTools" => match list_tools_from_client(client).await {
            Ok(tools) => DaemonResponse {
                id: request.id,
                success: true,
                data: Some(serde_json::to_value(&tools).unwrap_or_default()),
                error: None,
            },
            Err(e) => DaemonResponse {
                id: request.id,
                success: false,
                data: None,
                error: Some(DaemonError {
                    code: "EXECUTION_ERROR".to_string(),
                    message: e.message,
                }),
            },
        },

        "callTool" => {
            let tool_name = match &request.tool_name {
                Some(n) => n.clone(),
                None => {
                    return DaemonResponse {
                        id: request.id,
                        success: false,
                        data: None,
                        error: Some(DaemonError {
                            code: "MISSING_TOOL".to_string(),
                            message: "toolName required".to_string(),
                        }),
                    };
                }
            };
            let args = request.args.unwrap_or_default();
            match call_tool_on_client(client, &tool_name, args).await {
                Ok(result) => DaemonResponse {
                    id: request.id,
                    success: true,
                    data: Some(result),
                    error: None,
                },
                Err(e) => DaemonResponse {
                    id: request.id,
                    success: false,
                    data: None,
                    error: Some(DaemonError {
                        code: "EXECUTION_ERROR".to_string(),
                        message: e.message,
                    }),
                },
            }
        }

        "getInstructions" => {
            let instructions = get_instructions(client);
            DaemonResponse {
                id: request.id,
                success: true,
                data: instructions.map(serde_json::Value::String),
                error: None,
            }
        }

        "close" => DaemonResponse {
            id: request.id,
            success: true,
            data: Some(serde_json::Value::String("closing".to_string())),
            error: None,
        },

        _ => DaemonResponse {
            id: request.id,
            success: false,
            data: None,
            error: Some(DaemonError {
                code: "UNKNOWN_TYPE".to_string(),
                message: format!("Unknown request type: {}", request.request_type),
            }),
        },
    }
}

fn cleanup_daemon_files(server_name: &str) {
    remove_socket_file(server_name);
    remove_pid_file(server_name);
}

impl Serialize for ToolInfo {
    fn serialize<S>(&self, serializer: S) -> Result<S::Ok, S::Error>
    where
        S: serde::Serializer,
    {
        use serde::ser::SerializeStruct;
        let mut state = serializer.serialize_struct("ToolInfo", 3)?;
        state.serialize_field("name", &self.name)?;
        state.serialize_field("description", &self.description)?;
        state.serialize_field("inputSchema", &self.input_schema)?;
        state.end()
    }
}

impl<'de> Deserialize<'de> for ToolInfo {
    fn deserialize<D>(deserializer: D) -> Result<Self, D::Error>
    where
        D: serde::Deserializer<'de>,
    {
        #[derive(Deserialize)]
        struct ToolInfoHelper {
            name: String,
            description: Option<String>,
            #[serde(rename = "inputSchema")]
            input_schema: serde_json::Value,
        }
        let helper = ToolInfoHelper::deserialize(deserializer)?;
        Ok(ToolInfo {
            name: helper.name,
            description: helper.description,
            input_schema: helper.input_schema,
        })
    }
}
