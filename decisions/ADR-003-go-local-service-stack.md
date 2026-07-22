# ADR-003: 本地服务采用 Go 单二进制技术栈

## Status

Accepted

## Date

2026-07-21

## Context

AgentTaskMonitor 本地服务需要：

- 长时间常驻且资源占用可控。
- 并发读取 SQLite、监听 JSONL 文件并检查本机进程。
- 提供 loopback HTTP API 和 SSE 实时事件流。
- 在 macOS 上易于启动、停止、升级和诊断。
- 尽量减少运行时和第三方依赖。
- 后续保留 Windows 支持的可能性。

当前开发机已经安装：

- Go `1.26.5 darwin/arm64`
- Swift `6.3.3`
- Node.js `24.14.1`
- Apple Command Line Tools

当前未安装 Rust 工具链。

## Decision

AgentTaskMonitor 本地服务采用 **Go 1.26.x** 开发，构建为单个原生 ARM64 可执行文件。

### 标准库优先

第一版优先使用 Go 标准库：

- `net/http`：REST、SSE、静态页面服务和安全中间件。
- `encoding/json`：API 编解码。
- `context`：取消、超时和优雅退出。
- `log/slog`：结构化日志和敏感字段过滤。
- `embed`：把原型 Web 静态资源嵌入可执行文件。
- `testing`、`net/http/httptest`：状态机、API 和安全边界测试。

Go 1.26 的标准 `http.ServeMux` 已支持 HTTP 方法、Host 和路径参数模式。第一版不引入 Gin、Echo 或 Chi，除非实现中出现标准库无法清晰解决的具体问题。

### SQLite

- 通过 `database/sql` 使用独立 SQLite 适配层。
- Codex 数据库始终以只读方式打开。
- 优先选择纯 Go SQLite 驱动，避免正式发布依赖 CGO 或用户本机动态库。
- 在第一轮技术验证中必须检查 WAL 并发读取、busy timeout 和 Codex 升级兼容性。
- 不使用 ORM；查询显式、参数化并限制到所需字段。
- AgentTaskMonitor 如需持久化项目映射和状态历史，使用自己的数据库，不向 Codex 数据库写入任何内容。

具体 SQLite 驱动在兼容性验证后锁定；驱动必须隐藏在适配层后，避免状态引擎依赖实现细节。

### 文件监听

- 使用一个小型文件监听适配层观察 `~/.codex/sessions` 的目录和文件变化。
- 优先评估 `fsnotify`；同时保留低频扫描作为事件丢失、休眠恢复和监听器重建后的校准机制。
- 状态机不能把单次文件事件当作最终事实，必须能够重新扫描恢复状态。

### 进程与服务生命周期

- 原型阶段以前台命令运行，方便观察和调试。
- 正式 macOS 版本使用用户级 LaunchAgent 管理后台服务。
- 服务响应 `SIGTERM`，停止新请求、关闭 SSE 连接并完成必要状态落盘后退出。
- 服务只绑定 loopback 地址。

### 项目模块

初步模块边界：

```text
cmd/agent-task-monitor       服务入口
internal/codexstate          Codex SQLite 只读适配器
internal/sessionwatcher      JSONL 文件监听与恢复扫描
internal/processprobe        Codex 进程探测
internal/statusengine        状态机与可信度判断
internal/projectresolver     项目、worktree、临时目录归一化
internal/api                 REST、SSE 和安全边界
internal/appstate            AgentTaskMonitor 自有配置与状态
web/                         Web 前端静态资源
```

## Alternatives Considered

### Swift

不作为本地服务首选。

优点：macOS 系统集成最好，适合未来菜单栏和桌面壳。

不足：会让状态引擎和 API 服务过早绑定 macOS，跨平台复用较弱。Swift 仍可用于未来原生桌面壳，但不承担核心观察服务。

### Rust

第一版不采用。

优点：资源控制和类型安全优秀。

不足：当前机器没有 Rust 工具链，开发和维护复杂度更高；本项目当前性能需求不足以抵消额外成本。

### Node.js / TypeScript

不作为后台服务首选。

优点：Web 前后端共享语言，原型速度快。

不足：需要管理 Node 运行时或额外打包，依赖树和常驻服务交付复杂度高于 Go 单二进制。TypeScript 仍适合 Web 前端。

### Python

不采用。

优点：数据验证脚本开发快。

不足：运行时、环境和打包管理不适合本项目的长期常驻本地服务。Python 可用于一次性研究，不进入产品运行时。

## Consequences

### Positive

- 单个二进制即可运行，不要求用户维护 Node、Python 或 Rust 环境。
- 标准库覆盖大部分需求，依赖面小。
- goroutine 和 channel 适合文件监听、状态更新和 SSE 广播。
- 服务层可以独立进行状态机、API、并发和恢复测试。
- Web 页面可嵌入同一二进制，也可在开发期独立运行。
- 核心状态服务未来可复用到 macOS 和 Windows 桌面壳。

### Negative

- 菜单栏、通知和桌面窗口仍需要单独的桌面壳技术。
- SQLite 和 macOS 进程探测需要少量第三方或平台适配代码。
- Go 嵌入 WebView 的桌面生态不作为核心能力，需要与桌面壳明确分层。
- 纯 Go SQLite 驱动必须经过真实 Codex 数据库并发读取验证。

## Verification Gates

进入 Web 看板开发前必须完成：

1. 能只读打开当前 `state_5.sqlite`，不阻塞 Codex App。
2. 能扫描并增量跟踪真实 JSONL 会话事件。
3. 能输出确定、推断和不确定三种质量等级。
4. 能在休眠恢复、文件轮转和服务重启后重建状态。
5. `go test ./...` 和 `go vet ./...` 通过。
6. 竞态检测覆盖状态引擎和 SSE 广播器。
7. loopback、Host、Origin 和错误脱敏测试通过。

## Revisit When

- 原生 macOS 菜单栏能力成为主要开发工作。
- 需要共享大量 Swift 数据模型，且跨平台不再重要。
- Go 在目标 Codex SQLite 或文件监听场景中出现无法规避的兼容问题。
- 服务需要远程多租户能力或显著更高的事件吞吐。
