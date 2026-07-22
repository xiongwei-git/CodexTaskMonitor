#!/usr/bin/env python3
"""Generate a read-only report of unarchived Codex tasks without a project mapping."""

from __future__ import annotations

import csv
import json
import sqlite3
import subprocess
from collections import Counter
from dataclasses import dataclass
from datetime import datetime, timezone
from pathlib import Path
from zoneinfo import ZoneInfo


HOME = Path.home()
PROJECT_ROOT = Path(__file__).resolve().parents[1]
CODEX_ROOT = HOME / "CodeX"
STATE_DB = HOME / ".codex" / "state_5.sqlite"
PROBE_BINARY = PROJECT_ROOT / "bin" / "agent-task-monitor"
REPORTS_DIR = PROJECT_ROOT / "reports"
LOCAL_TIMEZONE = ZoneInfo("Asia/Shanghai")


@dataclass
class TaskRow:
    thread_id: str
    name: str
    cwd: str
    created_at: str
    updated_at: str
    user_turns: int
    assistant_messages: int
    status: str
    category: str
    recommendation: str
    deep_link: str
    parse_errors: int


def local_time(timestamp: int) -> str:
    value = datetime.fromtimestamp(timestamp, tz=timezone.utc).astimezone(LOCAL_TIMEZONE)
    return value.strftime("%Y-%m-%d %H:%M")


def task_name(title: str) -> str:
    first_line = title.strip().splitlines()[0] if title.strip() else "未命名任务"
    return " ".join(first_line.split())


def category_for(title: str, cwd: str) -> str:
    if title.startswith("Automation:"):
        return "每日自动化运行"
    if cwd == str(CODEX_ROOT):
        return "CodeX 根目录对话"
    if cwd.startswith(str(HOME / ".codex" / ".chatgpt-projects")):
        return "ChatGPT 项目镜像"
    if cwd.startswith(str(HOME / "Documents" / "Codex")):
        return "Codex 临时目录"
    return "其他目录"


def recommendation_for(category: str, user_turns: int) -> str:
    if category == "每日自动化运行":
        return "日报确认后可归档历史运行"
    if category == "CodeX 根目录对话":
        return "确认归属到正式项目或归档"
    if category == "ChatGPT 项目镜像":
        return "绑定同名正式项目或归档"
    if category == "Codex 临时目录":
        if user_turns >= 5:
            return "交互较多，建议迁入正式项目"
        return "一次性任务完成后可归档"
    if user_turns >= 5:
        return "确认外部目录项目映射"
    return "确认项目归属或归档"


def scan_rollout(path: str) -> tuple[int, int, str, int]:
    user_turns = 0
    assistant_messages = 0
    status = "未知"
    parse_errors = 0
    try:
        with open(path, encoding="utf-8") as rollout:
            for line in rollout:
                try:
                    item = json.loads(line)
                except json.JSONDecodeError:
                    parse_errors += 1
                    continue
                payload = item.get("payload") or {}
                if item.get("type") == "event_msg":
                    event_type = payload.get("type")
                    if event_type == "user_message":
                        user_turns += 1
                    elif event_type == "task_started":
                        status = "最近一次可能未完成"
                    elif event_type == "task_complete":
                        status = "已完成"
                    elif event_type in {"turn_aborted", "task_aborted"}:
                        status = "已中断"
                if (
                    item.get("type") == "response_item"
                    and payload.get("type") == "message"
                    and payload.get("role") == "assistant"
                ):
                    assistant_messages += 1
    except OSError:
        parse_errors += 1
        status = "会话文件不可读"
    return user_turns, assistant_messages, status, parse_errors


def load_unmapped_tasks() -> tuple[list[TaskRow], dict]:
    probe = json.loads(subprocess.check_output([str(PROBE_BINARY), "probe"], text=True))
    mapped_ids = {
        thread["id"]
        for project in probe["projects"]
        for thread in project.get("threads", [])
    }

    database = sqlite3.connect(f"file:{STATE_DB}?mode=ro", uri=True)
    try:
        records = database.execute(
            """
            SELECT id, title, cwd, rollout_path, created_at, updated_at
            FROM threads
            WHERE archived = 0
            ORDER BY updated_at DESC, id DESC
            """
        ).fetchall()
    finally:
        database.close()

    tasks: list[TaskRow] = []
    for thread_id, title, cwd, rollout_path, created_at, updated_at in records:
        if thread_id in mapped_ids:
            continue
        user_turns, assistant_messages, status, parse_errors = scan_rollout(rollout_path)
        category = category_for(title, cwd)
        tasks.append(
            TaskRow(
                thread_id=thread_id,
                name=task_name(title),
                cwd=cwd,
                created_at=local_time(created_at),
                updated_at=local_time(updated_at),
                user_turns=user_turns,
                assistant_messages=assistant_messages,
                status=status,
                category=category,
                recommendation=recommendation_for(category, user_turns),
                deep_link=f"codex://threads/{thread_id}",
                parse_errors=parse_errors,
            )
        )

    expected = probe["summary"]["unmappedThreadCount"]
    if len(tasks) != expected:
        raise RuntimeError(f"unmapped task count changed during report: got {len(tasks)}, expected {expected}")
    return tasks, probe


def markdown_text(value: str) -> str:
    return value.replace("\\", "\\\\").replace("|", "\\|").replace("\n", " ")


def short_path(path: str) -> str:
    home = str(HOME)
    return "~" + path[len(home):] if path.startswith(home) else path


def write_csv(tasks: list[TaskRow], path: Path) -> None:
    with path.open("w", encoding="utf-8-sig", newline="") as output:
        writer = csv.writer(output)
        writer.writerow(
            [
                "任务名称",
                "用户发言次数",
                "助手消息数",
                "最近状态",
                "创建时间",
                "最后更新时间",
                "来源分类",
                "工作目录",
                "建议处理",
                "任务ID",
                "Codex链接",
            ]
        )
        for task in tasks:
            writer.writerow(
                [
                    task.name,
                    task.user_turns,
                    task.assistant_messages,
                    task.status,
                    task.created_at,
                    task.updated_at,
                    task.category,
                    task.cwd,
                    task.recommendation,
                    task.thread_id,
                    task.deep_link,
                ]
            )


def write_markdown(
    tasks: list[TaskRow], probe: dict, path: Path, *, include_csv_note: bool = True
) -> None:
    category_counts = Counter(task.category for task in tasks)
    total_turns = sum(task.user_turns for task in tasks)
    automation_count = category_counts["每日自动化运行"]
    manual_tasks = len(tasks) - automation_count
    parse_errors = sum(task.parse_errors for task in tasks)
    generated_at = datetime.now(LOCAL_TIMEZONE).strftime("%Y-%m-%d %H:%M")
    priority_tasks = sorted(
        (task for task in tasks if task.category != "每日自动化运行"),
        key=lambda task: (-task.user_turns, task.updated_at),
    )[:10]
    suggestions: list[str] = []
    if automation_count:
        suggestions.append(
            f"每日自动化的历史运行记录共 {automation_count} 个，日报确认后可以批量归档，不必建立独立项目。"
        )
    if category_counts["Codex 临时目录"]:
        suggestions.append(
            "`Documents/Codex` 临时目录中的高交互任务，优先判断是否已经发展成长期项目。"
        )
    if category_counts["ChatGPT 项目镜像"] or category_counts["CodeX 根目录对话"]:
        suggestions.append(
            "ChatGPT 项目镜像和 CodeX 根目录对话，需要补充正式项目映射或归档。"
        )
    if category_counts["其他目录"]:
        suggestions.append(
            "CodeX 之外的长期目录可以建立显式别名，避免以后持续进入待归类列表。"
        )

    lines = [
        "# Codex 未关联项目任务整理报表",
        "",
        f"> 生成时间：{generated_at}（Asia/Shanghai）",
        "",
        "## 统计口径",
        "",
        "- 一个独立 Codex 对话视为一个任务。",
        "- “用户发言次数”只统计会话事件中的真实用户消息，不包含系统提示、工具调用或助手过程更新。",
        "- 仅统计当前未归档、且无法归入 `~/CodeX` 正式项目的任务。",
        "",
        "## 总览",
        "",
        f"- 未归档任务记录：{probe['sources']['stateDb']['threadRecords']} 个",
        f"- 已归入 {probe['summary']['projectCount']} 个项目：{probe['summary']['threadCount']} 个",
        f"- 未关联项目：{len(tasks)} 个",
        f"- 未关联任务中的用户发言：{total_turns} 次",
        f"- 人工对话：{manual_tasks} 个；每日自动化运行记录：{automation_count} 个",
        f"- 会话文件解析异常：{parse_errors} 处",
        "",
        "### 来源分布",
        "",
        "| 来源 | 任务数 |",
        "|---|---:|",
    ]
    for category, count in category_counts.most_common():
        lines.append(f"| {markdown_text(category)} | {count} |")

    lines.extend(
        [
            "",
            "## 优先整理建议",
            "",
        ]
    )
    lines.extend(f"{index}. {suggestion}" for index, suggestion in enumerate(suggestions, 1))
    lines.extend(
        [
            "",
            "### 交互最多的 10 个非自动化任务",
            "",
            "| 任务名称 | 用户发言次数 | 最后更新 | 来源 | 打开 |",
            "|---|---:|---|---|---|",
        ]
    )
    for task in priority_tasks:
        lines.append(
            f"| {markdown_text(task.name)} | {task.user_turns} | {task.updated_at} | "
            f"{markdown_text(task.category)} | [打开任务]({task.deep_link}) |"
        )

    section_order = [
        "CodeX 根目录对话",
        "ChatGPT 项目镜像",
        "Codex 临时目录",
        "其他目录",
        "每日自动化运行",
    ]
    for category in section_order:
        category_tasks = [task for task in tasks if task.category == category]
        if not category_tasks:
            continue
        category_tasks.sort(key=lambda task: (-task.user_turns, task.updated_at))
        lines.extend(
            [
                "",
                f"## {category}（{len(category_tasks)}）",
                "",
                "| 任务名称 | 用户发言次数 | 最近状态 | 最后更新 | 工作目录 | 建议处理 | 打开 |",
                "|---|---:|---|---|---|---|---|",
            ]
        )
        for task in category_tasks:
            lines.append(
                f"| {markdown_text(task.name)} | {task.user_turns} | {task.status} | {task.updated_at} | "
                f"`{markdown_text(short_path(task.cwd))}` | {markdown_text(task.recommendation)} | "
                f"[打开]({task.deep_link}) |"
            )

    lines.extend(["", "## 使用说明", "", "- Markdown 版用于快速阅读和点击任务。"])
    if include_csv_note:
        lines.append("- CSV 版使用 UTF-8 BOM，可直接用 Numbers 或 Excel 打开、筛选和补充处理结果。")
    lines.extend(["- 本报表只读，不会归档、移动或修改任何 Codex 任务。", ""])
    path.write_text("\n".join(lines), encoding="utf-8")


def main() -> None:
    tasks, probe = load_unmapped_tasks()
    REPORTS_DIR.mkdir(parents=True, exist_ok=True)
    report_date = datetime.now(LOCAL_TIMEZONE).strftime("%Y-%m-%d")
    markdown_path = REPORTS_DIR / f"{report_date}-unmapped-tasks.md"
    csv_path = REPORTS_DIR / f"{report_date}-unmapped-tasks.csv"
    write_markdown(tasks, probe, markdown_path)
    write_csv(tasks, csv_path)
    print(json.dumps({"tasks": len(tasks), "markdown": str(markdown_path), "csv": str(csv_path)}, ensure_ascii=False))


if __name__ == "__main__":
    main()
