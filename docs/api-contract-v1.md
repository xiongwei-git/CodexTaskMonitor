# AgentTaskMonitor Local API v1

状态：Implemented
日期：2026-07-21

## 目标

为 AgentTaskMonitor Web 前端提供稳定、只读、版本化的本地产品数据接口。

API 不镜像 Codex 内部数据库，也不暴露原始 JSONL。底层数据变化由本地服务吸收，前端只依赖本文定义的产品对象。

## Transport

- Base URL：`http://127.0.0.1:<port>`
- API prefix：`/api/v1`
- Snapshot：JSON over HTTP
- Live updates：Server-Sent Events
- 时间：RFC 3339 UTC 字符串
- ID：不透明字符串，客户端不能解析其结构

## Core Status

### ThreadStatus

- `working`
- `waiting`
- `completed`
- `interrupted`
- `failed`
- `suspected_abnormal`
- `idle`
- `unknown`

### StatusQuality

- `exact`：有明确的开始、完成、中断或失败事件
- `inferred`：由多个本地活动信号推断
- `uncertain`：证据不足，页面必须明确提示不确定性

## Resources

### Project

```json
{
  "id": "project_opaque_id",
  "name": "AgentTaskMonitor",
  "canonicalPath": "/Users/example/CodeX/AgentTaskMonitor",
  "summary": "聚合展示本机 Codex 项目与任务状态，帮助快速发现异常的实时看板。",
  "summarySource": "manual_override",
  "summaryQuality": "confirmed",
  "summaryUpdatedAt": "2026-07-21",
  "priority": "p0",
  "priorityRank": 1000,
  "preferenceUpdatedAt": "2026-07-21T08:30:00Z",
  "status": "working",
  "statusQuality": "inferred",
  "threadCounts": {
    "working": 1,
    "completed": 3
  },
  "threadTotal": 4,
  "runtime": {
    "status": "running",
    "statusQuality": "inferred",
    "processCount": 1,
    "processes": [
      {
        "id": "process_opaque_id",
        "runtime": "Python",
        "label": "resume_tree_by_node.py",
        "status": "running",
        "statusQuality": "inferred",
        "startedAt": "2026-07-21T02:20:00Z",
        "restartCount": 0
      }
    ]
  },
  "lastActivityAt": "2026-07-21T02:30:00Z"
}
```

`runtime.status` 为 `none`、`running` 或 `restarting`。后台进程运行时，项目可以是 `working`，同时其最近 Codex 任务仍是 `completed`。进程对象不返回 PID、完整命令、命令参数或日志正文。

`summaryQuality` 为 `confirmed`、`extracted` 或 `unknown`。简介来自 ProjectNavigator v3 登记表；登记表无法读取时这些字段为空，但项目实时状态仍可用。

`priority` 为 `p0`、`p1`、`p2`、`p3` 或 `unset`，P0 最高。`priorityRank` 只用于同一优先级内排序；未设置时为 0。

### Thread

```json
{
  "id": "thread_opaque_id",
  "projectId": "project_opaque_id",
  "title": "评估产品形态",
  "status": "working",
  "statusQuality": "inferred",
  "statusReason": "recent_session_activity",
  "startedAt": "2026-07-21T02:28:00Z",
  "lastActivityAt": "2026-07-21T02:30:00Z",
  "deepLink": "codex://threads/thread_opaque_id",
  "source": "codex_app"
}
```

`statusReason` 使用稳定的机器可读代码；面向用户的中文解释由前端映射，不直接依赖服务日志文本。

### Sources

```json
{
  "stateDb": {"status": "healthy", "threadRecords": 127},
  "sessions": {"status": "healthy", "filesScanned": 83, "filesMissing": 0, "parseErrors": 0},
  "processes": {
    "status": "healthy",
    "codexRunning": true,
    "codexProcessCount": 4,
    "chatgptAppRunning": true,
    "projectProcessesStatus": "healthy",
    "projectProcessCount": 1
  },
  "projectMetadata": {"status": "healthy", "recordCount": 64},
  "projectPreferences": {"status": "healthy", "recordCount": 3}
}
```

## Endpoints

### `GET /healthz`

只返回本地服务是否可响应，不包含 Codex 数据。

```json
{"status":"ok"}
```

### `GET /api/v1/overview`

返回菜单栏和看板首页所需的聚合快照。

包括：

- 项目总数
- 正在工作的项目数量（`activeProjectCount`）
- 需要关注的项目数量（`attentionProjectCount`）
- 后台进程数量（`backgroundProcessCount`）
- 等待处理数量
- 疑似异常数量
- 最近需要关注的项目和任务
- 数据源健康状态
- 最近需要关注的任务（最多 6 条）
- 最近活动任务（最多 12 条）

### `GET /api/v1/projects`

Query：

- `status`：可重复的状态筛选
- `search`：项目名称和一句话介绍搜索
- `cursor`：分页游标
- `limit`：默认 50，最大 100
- `sort`：`priority`、`attention`、`last_activity`、`name`；默认 `priority`

### `GET /api/v1/projects/{projectId}`

返回一个项目及其状态摘要。

不存在时返回 `404 project_not_found`。

### `PATCH /api/v1/projects/{projectId}/preferences`

设置项目优先级。只接受 JSON，未知字段会被拒绝。

```json
{"priority":"p0"}
```

传入 `unset` 会删除该项目的人工偏好。项目不存在返回 `404 project_not_found`，优先级无效返回 `422 invalid_priority`。

### `PUT /api/v1/project-order`

保存同一优先级内当前可见项目的顺序：

```json
{
  "priority": "p1",
  "projectIds": ["project_beta", "project_alpha"]
}
```

顺序与当前偏好冲突时返回 `409 project_order_conflict`，客户端应重新加载快照。

### `GET /api/v1/threads`

Query：

- `projectId`
- `status`
- `search`
- `cursor`
- `limit`：默认 50，最大 100
- `sort`：`attention`、`last_activity`、`started_at`

### `GET /api/v1/threads/{threadId}`

返回一个任务的归一化状态、状态依据和 Codex 深链接。

不返回对话正文、命令输出或原始会话事件。

### `GET /api/v1/sources`

返回 Codex 数据库、会话事件、进程探测器、ProjectNavigator 项目元数据和人工偏好的健康状态及兼容性信息。

### `GET /api/v1/events`

返回 SSE 事件流。

事件类型：

- `snapshot.updated`
- `project.status_changed`
- `thread.status_changed`
- `source.health_changed`

事件必须包含：

- `eventId`
- `occurredAt`
- `type`
- `data`

客户端使用 `Last-Event-ID` 恢复连接。服务无法补发时发送 `snapshot.updated`，要求客户端重新拉取快照。

## Response Shape

单资源：

```json
{
  "data": {},
  "meta": {
    "generatedAt": "2026-07-21T02:30:00Z",
    "apiVersion": "v1"
  }
}
```

列表：

```json
{
  "data": [],
  "meta": {
    "generatedAt": "2026-07-21T02:30:00Z",
    "apiVersion": "v1",
    "nextCursor": null
  }
}
```

错误：

```json
{
  "error": {
    "code": "invalid_status_filter",
    "message": "The status filter is not supported.",
    "requestId": "request_opaque_id"
  }
}
```

## Error Semantics

- `400`：格式错误
- `401`：需要本地会话认证时尚未认证
- `403`：Origin、Host 或权限被拒绝
- `404`：资源不存在
- `422`：参数格式正确但语义无效
- `429`：请求频率过高
- `500`：服务内部错误
- `503`：Codex 数据源暂不可用或版本不兼容

错误响应不得包含堆栈、本机敏感路径、SQL 或原始日志内容。

## Compatibility

- v1 内新增字段必须保持向后兼容。
- 现有枚举增加新值时，前端必须回退为 `unknown`，不能崩溃。
- 字段删除、重命名或类型改变必须进入新的 API 主版本。
- 前后端必须有合同测试覆盖状态枚举、分页、错误和 SSE 重连。

## 当前实现说明

- `agent-task-monitor serve` 默认监听 `127.0.0.1:4747`。
- 服务每 2 秒重建一次只读快照；内容发生变化时发送 `snapshot.updated`。
- SSE 当前不保存可回放事件历史。客户端连接或重连后立即收到 `snapshot.updated`，然后重新获取 REST 快照。
- 列表游标为不透明字符串；客户端不能依赖其内部表示。
- 当前服务拒绝非 loopback Host 和非同源 Origin，不返回 CORS 通配符。
- 偏好写接口只接受 `application/json`，请求体上限 16 KiB；项目路径和任意文件路径不能由请求指定。
- 项目进程观察只匹配 `~/CodeX/<project>` 下持续至少 10 秒的进程；完整命令和参数不会进入 API。
- 项目介绍从 `~/CodeX/ProjectNavigator/project-registry.json` v3 读取；不会在实时刷新中调用模型或扫描项目正文。
- 项目优先级从 `~/CodeX/ProjectNavigator/project-preferences.json` v1 读取；写入采用 `0600` 原子替换并保留上一版 `.bak`。
