# mcp-cli agent 重构实验

本仓库用于验证 AI agent 的代码重构能力：以 [philschmid/mcp-cli](https://github.com/philschmid/mcp-cli) 为原版，由 agent 将其重构为其他编程语言的等价实现。

## 目录结构

```
.
├── typescript/   # 原版 TypeScript 实现（git submodule → philschmid/mcp-cli）
├── golang/       # Agent 重构的 Go 语言版本
└── docs/         # 性能对比等分析报告
```

## 关于 mcp-cli

轻量级 CLI 工具，用于与 [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) 服务器交互。支持列出工具、查看 schema、搜索工具、调用工具等操作。

## 重构结果

| 版本 | 语言 | 二进制大小 | 冷启动 |
|------|------|-----------|--------|
| 原版 | TypeScript (Bun) | ~96 MB | ~50-150 ms |
| 重构版 | Golang | ~11 MB | ~1-5 ms |

详见 [docs/performance-comparison.md](docs/performance-comparison.md)。

## 初始化子模块

```bash
git submodule update --init
```
