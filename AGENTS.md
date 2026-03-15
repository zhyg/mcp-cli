# AGENTS.md

## Cursor Cloud specific instructions

This is a monorepo with two active CLI implementations of `mcp-cli` (a lightweight CLI for MCP servers): **Go** (`golang/`) and **Rust** (`rust/`). The TypeScript version (`typescript/`) is a git submodule reference only (not initialized by default).

### Toolchain requirements

- **Go 1.25+** -- the system default may be older; the VM snapshot has Go 1.25.8 installed at `/usr/local/go/bin/go`.
- **Rust stable 1.85+** -- `edition = "2024"` in `Cargo.toml` requires it. Run `rustup default stable` if `cargo` reports the old version.

### Build

Standard commands are in the top-level `Makefile`. Quick reference:

| Target | Command |
|--------|---------|
| Go binary | `make golang` (or `cd golang && go build -o mcp-cli .`) |
| Rust binary | `make rust` (or `cd rust && cargo build --release`) |
| Both | `make all` (also tries TypeScript submodule) |

Binaries go to `bin/{golang,rust}/mcp-cli`.

### Lint

- Go: `cd golang && go vet ./...`
- Rust: `cd rust && cargo clippy --release` -- note: the codebase has one pre-existing clippy error ("this loop never actually loops" in `commands.rs`) that causes clippy to return non-zero. `cargo build` still succeeds.

### Test

- Go: `cd golang && go test ./...` (111 tests)
- Rust: `cd rust && cargo test -- --skip test_parse_call_args_empty` (77 tests). The `test_parse_call_args_empty` test hangs because it reads from stdin; skip it in non-interactive environments.

### Running the CLI

Both CLIs need a valid MCP server config (JSON). Example:

```bash
mcp-cli -c /path/to/mcp_servers.json           # list servers
mcp-cli -c /path/to/mcp_servers.json info srv   # show server details
mcp-cli -c /path/to/mcp_servers.json call srv tool '{"key":"val"}'
```

A quick smoke-test config using the filesystem MCP server:

```json
{
  "mcpServers": {
    "filesystem": {
      "command": "npx",
      "args": ["-y", "@modelcontextprotocol/server-filesystem", "/tmp"]
    }
  }
}
```

### PATH note

The update script ensures `/usr/local/go/bin` is prepended to PATH in `~/.bashrc`. If Go is not found, run `export PATH=/usr/local/go/bin:$PATH`.
