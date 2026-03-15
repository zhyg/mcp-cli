# AGENTS.md

## Cursor Cloud specific instructions

This is a monorepo with three CLI implementations of `mcp-cli` (a lightweight CLI for MCP servers): **Go** (`golang/`), **Rust** (`rust/`) and **TypeScript** (`typescript/`)。TypeScript 版本通过 git submodule 关联原版 [philschmid/mcp-cli](https://github.com/philschmid/mcp-cli)，需先执行 `git submodule update --init --recursive typescript` 初始化。

### Toolchain requirements

- **Go 1.25+** -- the system default may be older; the VM snapshot has Go 1.25.8 installed at `/usr/local/go/bin/go`.
- **Rust stable 1.85+** -- `edition = "2024"` in `Cargo.toml` requires it。Run `rustup default stable` if `cargo` reports the old version.
- **Bun 1.x** -- TypeScript 版本使用 Bun 编译为独立二进制。VM snapshot 已安装到 `~/.bun/bin/bun`。

### Build

Standard commands are in the top-level `Makefile`. Quick reference:

| Target | Command | 备注 |
|--------|---------|------|
| Go | `make golang` (or `cd golang && go build -o mcp-cli .`) | ~12 MB |
| Rust | `make rust` (or `cd rust && cargo build --release`) | ~14 MB |
| TypeScript | `make typescript` (or `cd typescript && bun install && bun build --compile --minify src/index.ts --outfile ../bin/typescript/mcp-cli`) | ~100 MB，首次需初始化 submodule |
| All | `make all` | 构建全部三个版本 |

Binaries go to `bin/{golang,rust,typescript}/mcp-cli`.

### Lint

- Go: `cd golang && go vet ./...`
- Rust: `cd rust && cargo clippy --release` -- note: the codebase has one pre-existing clippy error ("this loop never actually loops" in `commands.rs`) that causes clippy to return non-zero。`cargo build` still succeeds.
- TypeScript: `cd typescript && bunx biome check src/`

### Test

- Go: `cd golang && go test ./...` (111 tests)
- Rust: `cd rust && cargo test -- --skip test_parse_call_args_empty` (77 tests)。The `test_parse_call_args_empty` test hangs because it reads from stdin; skip it in non-interactive environments.
- TypeScript: 原版仓库无自动化测试。

### Running the CLI

三个版本的 CLI 功能等价（TypeScript 额外支持 Daemon 连接池），均需要一个 MCP server config（JSON）。详细性能对比见 `docs/performance-comparison.md`。示例：

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
