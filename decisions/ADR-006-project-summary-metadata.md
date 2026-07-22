# ADR-006: 由 ProjectNavigator 生成项目简介，看板只消费登记表

## Status

Accepted

## Date

2026-07-21

## Context

只有项目名和实时状态时，用户仍需要回忆每个项目在做什么。看板需要稳定的一句话介绍，但不能在每次两秒刷新时重复扫描项目或调用模型，也不能把任意项目文档正文直接暴露给浏览器。

## Decision

1. ProjectNavigator 负责从人工覆盖、README、PROJECT_MEMORY 和项目元数据中生成一句话介绍，并写入 `project-registry.json`。
2. 人工覆盖文件 `project-summary-overrides.json` 优先级最高，用于保存已经确认的介绍。
3. 自动生成的 PROJECT_BRIEF 和 ProjectNavigator 生成的 PROJECT_MEMORY 不作为介绍来源，避免生成内容循环污染。
4. 介绍携带 `summary_source`、`summary_quality` 和 `summary_updated_at`；质量为 `confirmed`、`extracted` 或 `unknown`。
5. AgentTaskMonitor 每次快照只读取登记表，按规范化项目路径合并介绍；登记表缺失或版本不兼容时数据源降级，但实时状态服务继续运行。
6. API 仅返回最终介绍和来源元数据，不返回被读取的文档内容或介绍指纹。
7. 看板用固定两行区域展示介绍，并允许通过介绍文本搜索项目。

## Consequences

- 实时刷新成本稳定，不依赖模型或全项目文档扫描。
- 人工确认内容与自动提取内容的可信度可以区分。
- 资料不足的项目明确显示“待补充项目介绍”，不会根据名称编造用途。
- 项目内容发生变化后，需要重新运行 ProjectNavigator 才能刷新介绍。
- AgentTaskMonitor 依赖 ProjectNavigator v3 登记表；读取失败时会在数据源健康状态中显式反映。
