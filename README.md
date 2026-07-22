# AgentTaskMonitor

一个用于持续探讨、验证并孵化产品创意的项目空间。

## 当前状态

- 阶段：技术原型
- 创建日期：2026-07-20
- 产品名称：AgentTaskMonitor
- 产品定义：本地 Codex 项目与任务实时看板
- 推荐形态：macOS 菜单栏桌面伴侣应用

首位目标用户是同时维护多个项目、并行运行多个 Codex 任务的个人重度用户。产品希望统一呈现项目与任务状态、提示可能的异常，并让用户一键回到对应 Codex 对话。

## 当前目标

当前优先验证：

1. 能否从现有 Codex 本地状态准确识别正在工作和正常完成。
2. 能否用多信号识别疑似异常终止，并控制误报。
3. 按项目聚合是否比 Codex App 的线程列表更容易理解。
4. 一键返回对应 Codex 对话是否能明显降低查找成本。

## 当前原型

本地实时看板已经实现。构建并启动：

```bash
go build -o bin/agent-task-monitor ./cmd/agent-task-monitor
./bin/agent-task-monitor serve
```

然后访问 [http://127.0.0.1:4747](http://127.0.0.1:4747)。

看板提供：

- 正在工作、需要关注、项目和任务数量总览。
- 可点击的项目状态分布条，以及卡片内的任务状态轨道。
- 三列紧凑项目卡片、状态筛选和项目／任务搜索。
- 每张卡片显示固定两行的一句话项目介绍，并支持按介绍内容搜索。
- 可直接设置 P0–P3 项目优先级；同级项目支持拖拽和键盘上移／下移，并持久保存到本机。
- 支持按“我的优先级、状态优先、最近活动、项目名称”切换排序。
- 默认日间模式，并支持持久化的日间／夜间切换。
- 需要关注与最近活动列表。
- 没有异常时自动收起关注区域，减少重复信息。
- SSE 实时刷新和 30 秒校准刷新。
- 点击任务返回对应 Codex 对话。
- 识别路径位于项目目录中的长时后台进程，并单独显示“后台运行”或“频繁重启”。
- ChatGPT Project 镜像按声明名称归入同名 `~/CodeX` 项目。

服务只监听 loopback、只读访问 Codex 状态，并拒绝非本机 Host 与跨源请求。唯一可写数据是 ProjectNavigator 中的项目偏好文件；页面、API 和静态资源都包含在一个 Go 二进制中。

命令行探针仍可单独使用：

```bash
./bin/agent-task-monitor probe
```

它输出按项目归类的 JSON 状态报告；不会修改 Codex 数据，也不会输出对话正文。

真实数据验证结果和已知限制见 `docs/probe-v0.md`。

## 项目结构

- `PROJECT_MEMORY.md`：项目当前事实、决策、状态和下一步。
- `docs/product-discovery.md`：产品问题与假设的持续讨论稿。
- `docs/feasibility-assessment.md`：基于本机 Codex 状态和官方能力的初步可行性评估。
- `decisions/ADR-001-product-form.md`：产品为何采用本地桌面伴侣应用，而不是云端 Web 服务。
- `decisions/ADR-002-local-service-web-ui.md`：本地观察服务、API、SSE 与 Web 前端的架构边界。
- `decisions/ADR-003-go-local-service-stack.md`：本地服务为何采用 Go 单二进制技术栈。
- `decisions/ADR-004-dashboard-information-architecture.md`：看板为何采用状态优先的本地任务指挥台。
- `decisions/ADR-005-project-runtime-observation.md`：为何分离项目、Codex 任务和后台进程状态，以及隐私边界。
- `decisions/ADR-006-project-summary-metadata.md`：为何由 ProjectNavigator 生成项目介绍、看板只读取登记表。
- `decisions/ADR-007-user-project-preferences.md`：项目优先级的持久化、写入安全边界和排序规则。
- `docs/api-contract-v1.md`：第一版本地 API 合同。
- `docs/probe-v0.md`：首个只读状态探针的能力、真实验证和限制。
- `decisions/`：重要产品和技术决策记录。
- `research/`：竞品、用户、市场和技术研究。
- `prototypes/`：后续低保真或可运行原型。

## 协作原则

- 明确区分事实、用户反馈、假设和推测。
- 先理解问题，再讨论功能和技术方案。
- 每轮讨论都尽量沉淀一项结论或待验证假设。
- 重要决策写入项目文件，避免只留在聊天记录中。

## 下一步

复核资料不足项目的一句话介绍；继续完善 worktree、临时执行目录与长期项目的身份归一化，随后增加显式项目进度上报、增量文件监听和 macOS 自动启动／菜单栏外壳。
