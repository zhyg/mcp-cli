use crate::config::{ServerConfig, ToolInfo};

const COLOR_RESET: &str = "\x1b[0m";
const COLOR_BOLD: &str = "\x1b[1m";
const COLOR_DIM: &str = "\x1b[2m";
const COLOR_GREEN: &str = "\x1b[32m";
const COLOR_YELLOW: &str = "\x1b[33m";
const COLOR_CYAN: &str = "\x1b[36m";

fn should_colorize() -> bool {
    if std::env::var("NO_COLOR").is_ok() {
        return false;
    }
    atty::is(atty::Stream::Stdout)
}

fn colorize(text: &str, code: &str) -> String {
    if !should_colorize() {
        return text.to_string();
    }
    format!("{}{}{}", code, text, COLOR_RESET)
}

#[allow(dead_code)]
pub struct ServerWithTools {
    pub name: String,
    pub tools: Vec<ToolInfo>,
    pub instructions: Option<String>,
    pub error: Option<String>,
}

pub fn format_server_list(servers: &[ServerWithTools], with_descriptions: bool) -> String {
    let mut lines = Vec::new();

    for server in servers {
        lines.push(colorize(&server.name, &format!("{}{}", COLOR_BOLD, COLOR_CYAN)));

        if let Some(ref instructions) = server.instructions {
            let instr_lines: Vec<&str> = instructions.split('\n').collect();
            let mut first_line = instr_lines[0].to_string();
            if first_line.len() > 100 {
                first_line = first_line[..100].to_string();
            }
            let suffix = if instr_lines.len() > 1 || instr_lines[0].len() > 100 { "..." } else { "" };
            lines.push(format!("  {}", colorize(&format!("Instructions: {}{}", first_line, suffix), COLOR_DIM)));
        }

        for tool in &server.tools {
            if with_descriptions {
                if let Some(ref desc) = tool.description {
                    lines.push(format!("  • {} - {}", tool.name, colorize(desc, COLOR_DIM)));
                } else {
                    lines.push(format!("  • {}", tool.name));
                }
            } else {
                lines.push(format!("  • {}", tool.name));
            }
        }

        lines.push(String::new());
    }

    lines.join("\n").trim_end().to_string()
}

pub struct SearchResult {
    pub server: String,
    pub tool: ToolInfo,
}

pub fn format_search_results(results: &[SearchResult], with_descriptions: bool) -> String {
    let mut lines = Vec::new();

    for r in results {
        let server = colorize(&r.server, COLOR_CYAN);
        let tool = colorize(&r.tool.name, COLOR_GREEN);
        if let Some(ref desc) = r.tool.description {
            lines.push(format!("{}/{} - {}", server, tool, colorize(desc, COLOR_DIM)));
        } else {
            lines.push(format!("{}/{}", server, tool));
        }
    }

    let _ = with_descriptions;
    lines.join("\n")
}

pub fn format_server_details(
    server_name: &str,
    config: &ServerConfig,
    tools: &[ToolInfo],
    with_descriptions: bool,
    instructions: Option<&str>,
) -> String {
    let mut lines = Vec::new();

    lines.push(format!("{} {}", colorize("Server:", COLOR_BOLD), colorize(server_name, COLOR_CYAN)));

    if config.is_http() {
        lines.push(format!("{} HTTP", colorize("Transport:", COLOR_BOLD)));
        lines.push(format!("{} {}", colorize("URL:", COLOR_BOLD), config.url.as_deref().unwrap_or("")));
    } else {
        lines.push(format!("{} stdio", colorize("Transport:", COLOR_BOLD)));
        let args = config.args.as_deref().unwrap_or(&[]).join(" ");
        lines.push(format!("{} {} {}", colorize("Command:", COLOR_BOLD), config.command.as_deref().unwrap_or(""), args));
    }

    if let Some(instr) = instructions {
        lines.push(String::new());
        lines.push(colorize("Instructions:", COLOR_BOLD));
        for line in instr.split('\n') {
            lines.push(format!("  {}", line));
        }
    }

    lines.push(String::new());
    lines.push(colorize(&format!("Tools ({}):", tools.len()), COLOR_BOLD));

    for tool in tools {
        lines.push(format!("  {}", colorize(&tool.name, COLOR_GREEN)));
        if with_descriptions {
            if let Some(ref desc) = tool.description {
                lines.push(format!("    {}", colorize(desc, COLOR_DIM)));
            }
        }

        if let Some(props) = tool.input_schema.get("properties").and_then(|v| v.as_object()) {
            lines.push(format!("    {}", colorize("Parameters:", COLOR_YELLOW)));

            let required: Vec<String> = tool.input_schema.get("required")
                .and_then(|v| v.as_array())
                .map(|arr| arr.iter().filter_map(|v| v.as_str().map(String::from)).collect())
                .unwrap_or_default();

            for (name, prop_raw) in props {
                let prop = match prop_raw.as_object() {
                    Some(p) => p,
                    None => continue,
                };
                let req_str = if required.contains(name) { "required" } else { "optional" };
                let type_str = prop.get("type").and_then(|v| v.as_str()).unwrap_or("any");
                let desc = if with_descriptions {
                    prop.get("description").and_then(|v| v.as_str()).map(|d| format!(" - {}", d)).unwrap_or_default()
                } else {
                    String::new()
                };
                lines.push(format!("      • {} ({}, {}){}", name, type_str, req_str, desc));
            }
        }

        lines.push(String::new());
    }

    lines.join("\n").trim_end().to_string()
}

pub fn format_tool_schema(server_name: &str, tool: &ToolInfo) -> String {
    let mut lines = Vec::new();

    lines.push(format!("{} {}", colorize("Tool:", COLOR_BOLD), colorize(&tool.name, COLOR_GREEN)));
    lines.push(format!("{} {}", colorize("Server:", COLOR_BOLD), colorize(server_name, COLOR_CYAN)));
    lines.push(String::new());

    if let Some(ref desc) = tool.description {
        lines.push(colorize("Description:", COLOR_BOLD));
        lines.push(format!("  {}", desc));
        lines.push(String::new());
    }

    lines.push(colorize("Input Schema:", COLOR_BOLD));
    match serde_json::to_string_pretty(&tool.input_schema) {
        Ok(json) => lines.push(json),
        Err(_) => lines.push("  (error formatting schema)".to_string()),
    }

    lines.join("\n")
}

#[allow(dead_code)]
pub fn format_tool_result(result: &serde_json::Value) -> String {
    if let Some(content) = result.get("content").and_then(|v| v.as_array()) {
        let text_parts: Vec<&str> = content
            .iter()
            .filter(|c| c.get("type").and_then(|v| v.as_str()) == Some("text"))
            .filter_map(|c| c.get("text").and_then(|v| v.as_str()))
            .collect();
        if !text_parts.is_empty() {
            return text_parts.join("\n");
        }
    }
    serde_json::to_string_pretty(result).unwrap_or_else(|_| format!("{:?}", result))
}

#[cfg(test)]
mod tests {
    use super::*;

    fn make_tool(name: &str, desc: Option<&str>) -> ToolInfo {
        ToolInfo {
            name: name.to_string(),
            description: desc.map(String::from),
            input_schema: serde_json::json!({}),
        }
    }

    #[test]
    fn test_format_server_list_basic() {
        unsafe { std::env::set_var("NO_COLOR", "1"); }
        let servers = vec![ServerWithTools {
            name: "myserver".to_string(),
            tools: vec![make_tool("tool1", None), make_tool("tool2", None)],
            instructions: None,
            error: None,
        }];
        let output = format_server_list(&servers, false);
        assert!(output.contains("myserver"));
        assert!(output.contains("tool1"));
        assert!(output.contains("tool2"));
        unsafe { std::env::remove_var("NO_COLOR"); }
    }

    #[test]
    fn test_format_server_list_with_descriptions() {
        unsafe { std::env::set_var("NO_COLOR", "1"); }
        let servers = vec![ServerWithTools {
            name: "s".to_string(),
            tools: vec![make_tool("t", Some("a description"))],
            instructions: None,
            error: None,
        }];
        let output = format_server_list(&servers, true);
        assert!(output.contains("a description"));

        let output = format_server_list(&servers, false);
        assert!(!output.contains("a description"));
        unsafe { std::env::remove_var("NO_COLOR"); }
    }

    #[test]
    fn test_format_search_results() {
        unsafe { std::env::set_var("NO_COLOR", "1"); }
        let results = vec![
            SearchResult { server: "s1".to_string(), tool: make_tool("t1", Some("desc1")) },
            SearchResult { server: "s2".to_string(), tool: make_tool("t2", None) },
        ];
        let output = format_search_results(&results, false);
        assert!(output.contains("s1/t1"));
        assert!(output.contains("desc1"));
        assert!(output.contains("s2/t2"));
        unsafe { std::env::remove_var("NO_COLOR"); }
    }

    #[test]
    fn test_format_tool_schema() {
        unsafe { std::env::set_var("NO_COLOR", "1"); }
        let tool = ToolInfo {
            name: "my_tool".to_string(),
            description: Some("does stuff".to_string()),
            input_schema: serde_json::json!({"type": "object", "properties": {"path": {"type": "string"}}}),
        };
        let output = format_tool_schema("server1", &tool);
        assert!(output.contains("my_tool"));
        assert!(output.contains("server1"));
        assert!(output.contains("does stuff"));
        assert!(output.contains("path"));
        unsafe { std::env::remove_var("NO_COLOR"); }
    }

    #[test]
    fn test_format_tool_result_text_content() {
        let result = serde_json::json!({
            "content": [
                {"type": "text", "text": "hello"},
                {"type": "text", "text": "world"}
            ]
        });
        let output = format_tool_result(&result);
        assert_eq!(output, "hello\nworld");
    }

    #[test]
    fn test_format_tool_result_json_fallback() {
        let result = serde_json::json!({"key": "value"});
        let output = format_tool_result(&result);
        assert!(output.contains("key"));
        assert!(output.contains("value"));
    }

    #[test]
    fn test_format_server_details() {
        unsafe { std::env::set_var("NO_COLOR", "1"); }
        let config = ServerConfig {
            command: Some("node".to_string()),
            args: Some(vec!["server.js".to_string()]),
            env: None, cwd: None, url: None, headers: None,
            timeout: None, allowed_tools: None, disabled_tools: None,
        };
        let tools = vec![ToolInfo {
            name: "read_file".to_string(),
            description: Some("Read a file".to_string()),
            input_schema: serde_json::json!({
                "type": "object",
                "properties": {"path": {"type": "string", "description": "File path"}},
                "required": ["path"]
            }),
        }];
        let output = format_server_details("myserver", &config, &tools, true, Some("Be careful"));
        assert!(output.contains("myserver"));
        assert!(output.contains("stdio"));
        assert!(output.contains("node server.js"));
        assert!(output.contains("Be careful"));
        assert!(output.contains("read_file"));
        assert!(output.contains("Read a file"));
        assert!(output.contains("path"));
        assert!(output.contains("required"));
        unsafe { std::env::remove_var("NO_COLOR"); }
    }
}
