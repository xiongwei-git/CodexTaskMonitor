# Read-only Probe v0

日期：2026-07-21
状态：Implemented and verified

## 目标

在启动完整本地 HTTP 服务和 Web 看板之前，验证 Go 能否安全读取当前 Mac 上正在使用的 Codex 状态，并输出稳定、脱敏的产品对象。

## 使用方式

构建：

```bash
go build -o bin/agent-task-monitor ./cmd/agent-task-monitor
```

扫描默认 `~/.codex` 和 `~/CodeX`：

```bash
./bin/agent-task-monitor probe
```

调整疑似异常阈值：

```bash
./bin/agent-task-monitor probe --stale-after 15m
```

验证 ChatGPT Project 镜像目录：

```bash
./bin/agent-task-monitor probe \
  --project-root /Users/example/.codex/.chatgpt-projects
```

## 当前能力

- 以 SQLite `mode=ro` 和 `query_only` 打开 `state_5.sqlite`。
- 读取未归档 Codex 线程元数据。
- 扫描 JSONL，但只解析时间、记录类型和生命周期事件类型。
- 不保留提示词、对话正文、推理文本、命令输出或原始日志。
- 识别：
  - `working`
  - `completed`
  - `interrupted`
  - `suspected_abnormal`
  - `unknown`
- 为每个判断输出：
  - `statusQuality`
  - `statusReason`
  - `lastActivityAt`
- 检查 Codex 与 ChatGPT 进程是否存在。
- 将 `~/CodeX/<project>/...` 归一化为项目。
- 生成 `codex://threads/<thread-id>` 深链接。
- 项目 ID 使用路径哈希，不把路径结构编码进 ID。

## 状态判断

| 信号 | 状态 | 质量 |
|---|---|---|
| 收到 `task_complete` | `completed` | `exact` |
| 收到 `turn_aborted` | `interrupted` | `exact` |
| 已开始且仍有近期活动 | `working` | `inferred` |
| 已开始、无终止事件且超过阈值 | `suspected_abnormal` | `uncertain` |
| 缺少生命周期证据 | `unknown` | `uncertain` |

`suspected_abnormal` 不是“已确认崩溃”，页面必须保留不确定性表达。

## 真实数据验证

2026-07-21 在 Codex App 正在运行并写入本地状态时验证：

### `~/CodeX`

- 状态数据库：healthy
- 未归档线程记录：127
- 归入 `~/CodeX`：82 个线程、46 个项目路径
- 未归入该根目录：45 个线程
- 扫描会话文件：82
- 缺失文件：0
- JSONL 解析错误：0
- 状态结果：81 completed、1 interrupted
- 首次完整探针耗时：约 1.2 秒

### ChatGPT Project 镜像

- 成功识别当前 AgentTaskMonitor 对话为 `working`
- 判断质量：`inferred`
- 判断依据：`recent_session_activity`
- 成功生成当前对话深链接

## 已验证边界

- 真实 SQLite 并发读取未阻塞 Codex App。
- 当前增长中的 JSONL 可被安全扫描。
- 截断或格式错误行只计数，不输出行内容。
- 报告不包含 JSONL 路径或会话正文。
- 报告可以在服务重启后通过全量扫描重建。

## 已知限制

1. ChatGPT Project 镜像当前显示内部 `g-p-...` 名称，尚未归并到 `AgentTaskMonitor` 等长期项目。
2. `waiting` 和明确的 `failed` 状态尚未实现。
3. 当前是全量扫描探针，还没有增量文件监听。
4. 进程探测只能确认 Codex 是否运行，不能把进程精确映射到某条线程。
5. worktree、临时 Documents/Codex 目录和长期项目的归并尚未实现。
6. 还没有 HTTP API、SSE 或 Web 页面。

## 质量门禁

已通过：

- `go test ./...`
- `go test -race ./...`
- `go vet ./...`
- `git diff --check`
- macOS ARM64 构建

当前二进制约 9.8 MiB。

## 下一步

先完成项目身份归一化，让 ChatGPT Project、worktree 和临时执行目录能映射回长期项目；随后再把探针封装为 `/api/v1` 快照接口和 SSE 增量事件流。
