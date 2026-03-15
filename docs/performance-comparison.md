# mcp-cli 性能对比报告: TypeScript (Bun) vs Golang vs Rust

> 对比版本: TypeScript v0.3.0 (philschmid/mcp-cli) / Golang v0.3.0 / Rust v0.3.0
>
> 测试日期: 2026-03-15
>
> 测试环境: Linux 6.1.147 x86_64, Intel Xeon 4 cores, 15 GB RAM
>
> 工具链: Go 1.25.8 / Rust 1.94.0 (edition 2024) / Bun 1.3.10

---

## 1. 总览

| 维度 | TypeScript (Bun) | Golang | Rust | 优势方 |
|------|-------------------|--------|------|--------|
| 二进制大小 (linux-x64) | **99.4 MB** | **11.3 MB** | **14.6 MB** | **Go/Rust (~7-9x 更小)** |
| 冷启动 (--version) | **50 ms** | **3 ms** | **3 ms** | **Go/Rust (~17x 更快)** |
| MCP 操作延迟 | ~610 ms | ~560 ms | ~560 ms | **Go/Rust (快 ~10%)** |
| 峰值内存 (list) | **65.9 MB** | **8.9 MB** | **7.3 MB** | **Rust (最低, ~1/9)** |
| 源码行数 (不含测试) | 3,513 行 / 12 文件 | 2,637 行 / 8 文件 | 2,718 行 / 8 文件 | **Go (~25% 更少)** |
| 测试代码行数 | 无自动化测试 | 1,729 行 (111 tests) | 884 行 (79 tests) | **Go (覆盖最全)** |
| 运行时依赖 | Bun runtime + SDK | 无 (静态编译) | 无 (静态编译) | **Go/Rust** |
| 连接池/Daemon | 有 | 有 | 有 | 持平 |

---

## 2. 测试方法

### 测试配置

- **MCP Server**: `@modelcontextprotocol/server-filesystem` (via npx)
- **服务目录**: `/tmp`
- **Daemon**: 禁用 (`MCP_NO_DAEMON=1`), 确保每次都是冷连接
- **每项测试**: 2 次预热 + 10 次采样, 取中位数
- **启动测试**: 50 次采样取中位数
- **内存测试**: 通过 `/proc/$pid/status` 的 VmHWM 字段采样, 3 次取中位数
- **计时工具**: bash `date +%s%N` (纳秒精度)

### 测试项目

| 测试项 | 命令 | 测量内容 |
|--------|------|----------|
| 冷启动 | `mcp-cli --version` | 进程启动到退出的总耗时 |
| list | `mcp-cli -c config.json` | 连接 MCP server + 列出所有工具 |
| info | `mcp-cli -c config.json info filesystem` | 连接 + 查询服务器详情 |
| info tool | `mcp-cli -c config.json info filesystem read_file` | 连接 + 查询工具 schema |
| call | `mcp-cli -c config.json call filesystem read_file '{"path":"..."}'` | 连接 + 调用工具 |
| grep | `mcp-cli -c config.json grep '*file*'` | 连接 + 搜索工具 |
| 内存 | 上述 list 命令期间采样 VmHWM | 进程生命周期内的峰值 RSS |

---

## 3. 实测数据

### 3.1 二进制大小

| 版本 | 大小 | 相对 TypeScript |
|------|------|----------------|
| TypeScript (Bun compile) | 99.4 MB | 1x |
| Golang | 11.3 MB | **8.8x 更小** |
| Rust (release) | 14.6 MB | **6.8x 更小** |

TypeScript 的二进制包含完整 Bun 运行时 (~90+ MB); Go 和 Rust 均为静态编译的原生二进制。

### 3.2 冷启动性能 (--version, 50 次采样)

| 版本 | 中位数 | 最小值 | 最大值 | p95 |
|------|--------|--------|--------|-----|
| Golang | **3 ms** | 3 ms | 4 ms | 4 ms |
| Rust | **3 ms** | 2 ms | 4 ms | 3 ms |
| TypeScript | **50 ms** | 47 ms | 72 ms | 64 ms |

Go 和 Rust 启动速度相当, 均为原生机器码无运行时开销; TypeScript 需加载 Bun runtime + JIT, 慢约 17 倍。

### 3.3 MCP 操作延迟 (中位数, ms)

| 操作 | TypeScript | Golang | Rust |
|------|-----------|--------|------|
| list | 628 | 583 | 593 |
| info server | 639 | 545 | 604 |
| info tool | 618 | 566 | 549 |
| call | 609 | 543 | 539 |
| grep | 638 | 564 | 571 |
| **平均** | **626** | **560** | **571** |

三个版本的 MCP 操作延迟差异较小 (500-650 ms 量级), 因为总耗时主要受 MCP server 启动和通信 (npx spawn + stdio JSON-RPC) 支配, CLI 自身开销占比不大。Go 和 Rust 比 TypeScript 快约 10%, 主要来自更低的进程启动开销。

### 3.4 峰值内存 (list 命令, VmHWM)

| 版本 | 峰值 RSS | 相对 TypeScript |
|------|----------|----------------|
| TypeScript | 65.9 MB | 1x |
| Golang | 8.9 MB | **7.4x 更低** |
| Rust | 7.3 MB | **9.0x 更低** |

TypeScript 进程需要加载完整的 V8/Bun 运行时, 内存基线较高。Rust 内存最低, 得益于零运行时开销和无 GC 设计。

---

## 4. 代码复杂度与规模

### 4.1 源码规模

| 模块 | TypeScript | Golang | Rust |
|------|------------|--------|------|
| 入口 + CLI 解析 | index.ts (474) | main.go (324) | main.rs (504) |
| MCP 客户端 | client.ts (466) | client.go (312) | client.rs (466) |
| 配置管理 | config.ts (525) | config.go (432) | config.rs (714) |
| 命令实现 | 4 文件 (661) | commands.go (422) | commands.rs (448) |
| 错误处理 | errors.ts (386) | errors.go (320) | errors.rs (450) |
| 输出格式化 | output.ts (230) | output.go (215) | output.rs (318) |
| Daemon 系统 | 2 文件 (766) | daemon.go (604) | 2 文件 (702) |
| 版本 | version.ts (5) | 内联 config.go | 内联 config.rs |
| **源码合计** | **3,513 行 / 12 文件** | **2,637 行 / 8 文件** | **2,718 行 / 8 文件** |
| **测试合计** | **无** | **1,729 行 (111 tests)** | **884 行 (79 tests)** |
| **总计** | **3,513 行** | **4,366 行** | **3,602 行** |

### 4.2 分析

- Go 和 Rust 源码行数接近, 均比 TypeScript 少 ~20-25%
- Rust 的 config.rs 较长, 因为包含了大量配置验证和环境变量替换逻辑及其测试
- Go 测试最全面 (111 个测试函数, 1,729 行测试代码)
- Rust 测试亦相当充实 (79 个测试函数, 884 行测试代码)
- TypeScript 原版仓库无自动化测试

---

## 5. 依赖链

### TypeScript

```
@modelcontextprotocol/sdk (运行时)
@biomejs/biome (开发)
@types/bun (开发)
typescript (开发)
Bun runtime (编译进二进制, ~90+ MB)
```

### Golang

```
github.com/modelcontextprotocol/go-sdk (MCP 协议)
golang.org/x/term (终端检测)
---------- 以下为间接依赖 ----------
github.com/google/jsonschema-go
github.com/segmentio/encoding / asm
github.com/yosida95/uritemplate/v3
golang.org/x/oauth2, golang.org/x/sys
```

### Rust

```
rmcp (MCP 协议, 含 reqwest HTTP 客户端)
tokio (异步运行时)
serde / serde_json (序列化)
regex (glob 模式匹配)
http (HTTP header 类型)
atty (终端检测)
libc (Unix 系统调用)
---------- 以下为间接依赖 ----------
共 250 个 crate (含 reqwest, hyper, rustls 等)
```

Go 的依赖链最轻; Rust 因 tokio + reqwest + rustls 间接依赖较多, 但均静态编译入二进制, 无运行时依赖。

---

## 6. 并发模型

| 指标 | TypeScript | Golang | Rust |
|------|------------|--------|------|
| 并发原语 | Promise + 手动信号量 | goroutine + channel | tokio task + Semaphore |
| 调度器 | 单线程事件循环 | 多线程 M:N 调度 (GMP) | 多线程 work-stealing (tokio) |
| 单个协程内存 | ~数 KB (V8 Promise) | ~2-8 KB (goroutine) | ~数百 B - 数 KB (Future) |
| CPU 密集型 | 受限于单线程 | 多核并行 | 多核并行 |
| I/O 密集型 | 优秀 (非阻塞 I/O) | 优秀 (netpoller) | 优秀 (epoll/io_uring) |

对于 mcp-cli 的主要场景 (网络 I/O 为主), 三者性能差异不大。Go 和 Rust 在多 server 并发连接时有多核优势。

---

## 7. 功能对等性

| 功能 | TypeScript | Golang | Rust |
|------|------------|--------|------|
| list / info / grep / call 命令 | Y | Y | Y |
| stdio 传输 | Y | Y | Y |
| HTTP (StreamableHTTP) 传输 | Y | Y | Y |
| 自定义 HTTP headers | Y | Y | Y |
| 环境变量替换 (`${VAR}`) | Y (递归) | Y (递归) | Y (递归) |
| 工具过滤 (allowedTools/disabledTools) | Y | Y | Y |
| 重试 + 指数退避 (连接 + 操作) | Y | Y | Y |
| 并发控制 (MCP_CONCURRENCY) | Y | Y | Y |
| ANSI 颜色输出 + NO_COLOR | Y | Y | Y |
| 结构化错误消息 | Y | Y | Y |
| Daemon 连接池 | Y | Y | Y |
| stderr 转发 (带 server 前缀) | Y | Y | Y |
| Server instructions 显示 | Y | Y | Y |

三个版本功能完全对等。

---

## 8. 构建与分发

| 维度 | TypeScript | Golang | Rust |
|------|------------|--------|------|
| 构建工具 | `bun build --compile` | `go build` | `cargo build --release` |
| 构建速度 (增量) | ~1s | ~1-2s | ~10s |
| 交叉编译 | `--target=bun-linux-x64` | `GOOS/GOARCH` | `--target` + rustup |
| CI/CD 复杂度 | 需安装 Bun | Go 工具链内置 | 需安装 Rust 工具链 |
| 产物分发大小 | 99.4 MB | 11.3 MB | 14.6 MB |
| 运行时依赖 | 无 (Bun 打包) | 无 (静态链接) | 无 (静态链接) |

---

## 9. 综合评价

### 性能排名 (按维度)

| 维度 | 第一 | 第二 | 第三 |
|------|------|------|------|
| 二进制大小 | Go (11.3 MB) | Rust (14.6 MB) | TS (99.4 MB) |
| 冷启动速度 | Go/Rust (3 ms) | - | TS (50 ms) |
| MCP 操作延迟 | Go (560 ms) | Rust (571 ms) | TS (626 ms) |
| 内存占用 | Rust (7.3 MB) | Go (8.9 MB) | TS (65.9 MB) |
| 代码简洁度 | Go (2,637 行) | Rust (2,718 行) | TS (3,513 行) |
| 测试覆盖 | Go (111 tests) | Rust (79 tests) | TS (无) |
| 构建速度 | TS (~1s) | Go (~1-2s) | Rust (~10s) |

### 各版本优势

**Golang:**
1. 二进制最小 (11.3 MB), 分发最快
2. 冷启动极快 (3 ms), 与 Rust 并列
3. MCP 操作平均最快 (560 ms)
4. 代码最简洁 (2,637 行), 测试最全面 (111 tests)
5. 交叉编译最简单 (`GOOS/GOARCH`)
6. 构建速度快 (~1-2s)

**Rust:**
1. 内存占用最低 (7.3 MB), 适合资源受限环境
2. 冷启动极快 (3 ms), 与 Go 并列
3. call 操作最快 (539 ms)
4. 无 GC 停顿, 延迟最可预测
5. 类型系统最严格, 编译期捕获更多错误

**TypeScript:**
1. 生态最成熟, Bun/Node.js 生态庞大
2. 开发效率最高, 类型推导和工具链完善
3. 构建最快 (~1s)
4. 原版仓库, 功能迭代最活跃

### 结论

对于 **CLI 工具** 这一特定场景, **Go 和 Rust 版本在性能维度全面胜出**:

- **Go** 在综合表现上略优: 最小体积、最简洁代码、最全测试、最快操作延迟
- **Rust** 在内存效率上最优: 最低峰值 RSS, 无 GC, 延迟抖动最小
- **TypeScript** 的性能短板主要来自 Bun 运行时开销 (二进制体积、启动延迟、内存基线), 但在实际 MCP 操作中差距仅约 10%, 因为瓶颈在 MCP server 通信本身

三个版本功能完全对等, 均已实现 Daemon 连接池。在 MCP 操作层面, 三者延迟差距不大 (560-626 ms), 因为主要耗时在 server 端; 真正体现差异的是启动速度和资源占用。
