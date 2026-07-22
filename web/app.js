"use strict";

const state = {
  projects: [],
  threads: [],
  overview: null,
  filter: "all",
  query: "",
  selectedProject: null,
  sortMode: "priority",
  draggedProjectId: null,
  preferenceEditing: false,
  preferenceInteractionVersion: 0,
  refreshPending: false,
};

const statusInfo = {
  working: { label: "工作中", tone: "working" },
  waiting: { label: "等待处理", tone: "attention" },
  completed: { label: "已完成", tone: "completed" },
  interrupted: { label: "已中断", tone: "attention" },
  failed: { label: "失败", tone: "failed" },
  suspected_abnormal: { label: "疑似异常", tone: "attention" },
  idle: { label: "空闲", tone: "neutral" },
  unknown: { label: "状态未知", tone: "neutral" },
};

const attentionStatuses = new Set(["waiting", "interrupted", "failed", "suspected_abnormal"]);
const priorityInfo = {
  p0: { label: "P0 必须推进", short: "P0" },
  p1: { label: "P1 近期重点", short: "P1" },
  p2: { label: "P2 正常", short: "P2" },
  p3: { label: "P3 低优先级", short: "P3" },
  unset: { label: "未设置", short: "未设置" },
};
const statusGroups = [
  { key: "working", label: "工作中", filter: "working", statuses: ["working"] },
  { key: "attention", label: "需关注", filter: "attention", statuses: [...attentionStatuses] },
  { key: "completed", label: "已完成", filter: "completed", statuses: ["completed"] },
  { key: "other", label: "其他", filter: "all", statuses: ["idle", "unknown"] },
];
const byId = (id) => document.getElementById(id);
const themeStorageKey = "agent-task-monitor-theme";

function applyTheme(theme, persist = false) {
  const normalized = theme === "dark" ? "dark" : "light";
  document.documentElement.dataset.theme = normalized;
  const isLight = normalized === "light";
  const toggle = byId("theme-toggle");
  toggle.setAttribute("aria-pressed", String(isLight));
  toggle.setAttribute("aria-label", isLight ? "切换到夜间模式" : "切换到日间模式");
  byId("theme-label").textContent = isLight ? "日间模式" : "夜间模式";
  document.querySelector('meta[name="theme-color"]').content = isLight ? "#f3f6fa" : "#0b1018";
  if (persist) {
    try {
      localStorage.setItem(themeStorageKey, normalized);
    } catch (_error) {
      // The theme still works when browser storage is unavailable.
    }
  }
}

function setupTheme() {
  let initialTheme = document.documentElement.dataset.theme || "light";
  try {
    const savedTheme = localStorage.getItem(themeStorageKey);
    if (savedTheme === "light" || savedTheme === "dark") initialTheme = savedTheme;
  } catch (_error) {
    // Keep the light default when browser storage is unavailable.
  }
  applyTheme(initialTheme);
  byId("theme-toggle").addEventListener("click", () => {
    const nextTheme = document.documentElement.dataset.theme === "light" ? "dark" : "light";
    applyTheme(nextTheme, true);
  });
}

function element(tag, className, text) {
  const node = document.createElement(tag);
  if (className) node.className = className;
  if (text !== undefined) node.textContent = text;
  return node;
}

function statusPill(status) {
  const info = statusInfo[status] || statusInfo.unknown;
  const pill = element("span", `status-pill status-${info.tone}`);
  pill.append(element("span", "status-dot"), element("span", "", info.label));
  return pill;
}

function statusDot(status) {
  const info = statusInfo[status] || statusInfo.unknown;
  return element("span", `status-dot status-${info.tone}`);
}

function taskLink(thread, projectName, compact = false) {
  const link = element("a", compact ? "mini-task" : "activity-item");
  link.href = thread.deepLink;
  link.setAttribute("aria-label", `在 Codex 中打开：${thread.title || "未命名任务"}`);
  if (compact) {
    link.append(
      element("span", "mini-task-title", cleanTitle(thread.title)),
      element("span", "mini-task-time", relativeTime(thread.lastActivityAt)),
    );
    return link;
  }

  const copy = element("span", "activity-copy");
  copy.append(element("span", "activity-title", cleanTitle(thread.title)));
  const meta = element("span", "activity-meta");
  meta.append(
    element("span", "activity-project", projectName || "未归类项目"),
    element("span", "", relativeTime(thread.lastActivityAt)),
  );
  copy.append(meta);
  link.append(statusDot(thread.status), copy);
  return link;
}

function cleanTitle(title) {
  const normalized = String(title || "未命名任务").replace(/\s+/g, " ").trim();
  return normalized || "未命名任务";
}

function relativeTime(value) {
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return "未知时间";
  const seconds = Math.round((date.getTime() - Date.now()) / 1000);
  const formatter = new Intl.RelativeTimeFormat("zh-CN", { numeric: "auto" });
  if (Math.abs(seconds) < 60) return formatter.format(seconds, "second");
  const minutes = Math.round(seconds / 60);
  if (Math.abs(minutes) < 60) return formatter.format(minutes, "minute");
  const hours = Math.round(minutes / 60);
  if (Math.abs(hours) < 24) return formatter.format(hours, "hour");
  const days = Math.round(hours / 24);
  if (Math.abs(days) < 30) return formatter.format(days, "day");
  return new Intl.DateTimeFormat("zh-CN", { month: "short", day: "numeric" }).format(date);
}

function projectThreads(projectId) {
  return state.threads.filter((thread) => thread.projectId === projectId);
}

function runtimeCompact(process) {
  const restarting = process.status === "restarting";
  const row = element("div", `runtime-compact ${restarting ? "runtime-restarting" : "runtime-running"}`);
  const pulse = element("span", `runtime-pulse ${restarting ? "pulse-attention" : ""}`);
  pulse.append(element("i"), element("i"), element("i"));
  const copy = element("span", "runtime-copy");
  copy.append(
    element("span", "runtime-title", process.label || "项目进程"),
    element("span", "runtime-label", restarting ? "频繁重启" : "后台运行"),
  );
  row.append(pulse, copy, element("span", "runtime-meta", process.runtime || "Process"));
  return row;
}

function runtimeAlert(project, process) {
  const row = element("div", "activity-item runtime-alert");
  const copy = element("span", "activity-copy");
  copy.append(element("span", "activity-title", `${project.name} · ${process.label || "项目进程"}`));
  const meta = element("span", "activity-meta");
  meta.append(element("span", "activity-project", "后台进程频繁重启"), element("span", "", process.runtime || "Process"));
  copy.append(meta);
  row.append(statusDot("suspected_abnormal"), copy);
  return row;
}

function matchesFilter(project) {
  if (state.filter === "working" && project.status !== "working") return false;
  if (state.filter === "attention" && !attentionStatuses.has(project.status)) return false;
  if (state.filter === "completed" && project.status !== "completed") return false;

  if (!state.query) return true;
  const query = state.query.toLocaleLowerCase("zh-CN");
  if (project.name.toLocaleLowerCase("zh-CN").includes(query)) return true;
  if (String(project.summary || "").toLocaleLowerCase("zh-CN").includes(query)) return true;
  return projectThreads(project.id).some((thread) => cleanTitle(thread.title).toLocaleLowerCase("zh-CN").includes(query));
}

function groupedStatusCounts(projects) {
  return statusGroups.map((group) => ({
    ...group,
    count: projects.reduce((total, project) => total + (group.statuses.includes(project.status) ? 1 : 0), 0),
  }));
}

function renderStatusDistribution() {
  const groups = groupedStatusCounts(state.projects);
  const total = Math.max(1, state.projects.length);
  const distribution = byId("project-distribution");
  const segments = groups.filter((group) => group.count > 0).map((group) => {
    const segment = element("button", `distribution-segment segment-${group.key}`);
    segment.type = "button";
    segment.style.flexGrow = String(group.count);
    segment.title = `${group.label}：${group.count} 个项目`;
    segment.setAttribute("aria-label", `${group.label} ${group.count} 个项目，点击筛选`);
    segment.addEventListener("click", () => setFilter(group.filter));
    return segment;
  });
  distribution.replaceChildren(...segments);
  distribution.setAttribute(
    "aria-label",
    `项目状态分布，共 ${state.projects.length} 个项目。${groups.map((group) => `${group.label} ${group.count}`).join("，")}`,
  );

  const legend = byId("distribution-legend");
  legend.replaceChildren(...groups.map((group) => {
    const item = element("span", "legend-item");
    item.append(element("i", `legend-swatch segment-${group.key}`), element("span", "", group.label), element("strong", "", String(group.count)));
    item.title = `${Math.round((group.count / total) * 100)}%`;
    return item;
  }));
}

function projectStatusRail(project) {
  const counts = project.threadCounts || {};
  const groups = statusGroups.map((group) => ({
    ...group,
    count: group.statuses.reduce((total, status) => total + (Number(counts[status]) || 0), 0),
  }));
  const rail = element("div", "project-status-rail");
  rail.setAttribute("role", "img");
  if (!project.threadTotal) {
    rail.append(element("span", "rail-segment segment-other"));
    rail.setAttribute("aria-label", "暂无 Codex 任务状态");
    return rail;
  }
  groups.filter((group) => group.count > 0).forEach((group) => {
    const segment = element("span", `rail-segment segment-${group.key}`);
    segment.style.flexGrow = String(group.count);
    segment.title = `${group.label} ${group.count}`;
    rail.append(segment);
  });
  rail.setAttribute("aria-label", `任务状态：${groups.map((group) => `${group.label} ${group.count}`).join("，")}`);
  return rail;
}

function projectPrioritySelect(project) {
  const select = element("select", `priority-select priority-${project.priority || "unset"}`);
  select.setAttribute("aria-label", `设置 ${project.name} 的优先级`);
  Object.entries(priorityInfo).forEach(([value, info]) => {
    const option = element("option", "", info.label);
    option.value = value;
    option.selected = value === (project.priority || "unset");
    select.append(option);
  });
  select.addEventListener("pointerdown", beginPreferenceInteraction);
  select.addEventListener("focus", beginPreferenceInteraction);
  select.addEventListener("keydown", beginPreferenceInteraction);
  select.addEventListener("change", () => updateProjectPriority(project, select));
  select.addEventListener("blur", () => {
    if (select.dataset.saving !== "true") endPreferenceInteraction();
  });
  return select;
}

function priorityPeers(project) {
  return state.projects.filter((candidate) => candidate.priority === project.priority);
}

function orderButton(project, direction) {
  const peers = priorityPeers(project);
  const index = peers.findIndex((candidate) => candidate.id === project.id);
  const label = direction < 0 ? "上移" : "下移";
  const button = element("button", "order-button", direction < 0 ? "↑" : "↓");
  button.type = "button";
  button.setAttribute("aria-label", `${label} ${project.name}`);
  button.title = label;
  button.disabled = index < 0 || (direction < 0 ? index === 0 : index === peers.length - 1);
  button.addEventListener("click", () => moveProject(project, direction));
  return button;
}

function renderProject(project) {
  const info = statusInfo[project.status] || statusInfo.unknown;
  const priority = project.priority || "unset";
  const card = element("article", `project-card status-card-${info.tone} priority-card-${priority}`);
  card.dataset.projectId = project.id;
  card.draggable = state.sortMode === "priority" && priority !== "unset";
  if (card.draggable) {
    card.addEventListener("dragstart", () => {
      state.draggedProjectId = project.id;
      card.classList.add("dragging");
    });
    card.addEventListener("dragover", (event) => {
      const dragged = state.projects.find((candidate) => candidate.id === state.draggedProjectId);
      if (dragged && dragged.priority === project.priority && dragged.id !== project.id) {
        event.preventDefault();
        card.classList.add("drag-target");
      }
    });
    card.addEventListener("dragleave", () => card.classList.remove("drag-target"));
    card.addEventListener("drop", (event) => {
      event.preventDefault();
      card.classList.remove("drag-target");
      reorderBefore(state.draggedProjectId, project.id);
    });
    card.addEventListener("dragend", () => {
      state.draggedProjectId = null;
      document.querySelectorAll(".project-card").forEach((item) => item.classList.remove("dragging", "drag-target"));
    });
  }
  const top = element("div", "project-top");
  const name = element("div", "project-name");
  const title = element("h3", "", project.name);
  title.title = project.canonicalPath;
  name.append(title);
  top.append(name, statusPill(project.status));
  const summaryText = project.summary || "待补充项目介绍。";
  const summary = element("p", `project-summary ${project.summaryQuality === "unknown" ? "summary-unknown" : ""}`, summaryText);
  summary.title = summaryText;
  card.append(top, summary, projectStatusRail(project));

  const tasks = element("div", "card-tasks");
  const runtimeProcesses = project.runtime?.processes || [];
  runtimeProcesses.slice(0, 1).forEach((process) => tasks.append(runtimeCompact(process)));
  const recent = projectThreads(project.id).slice(0, 1);
  recent.forEach((thread) => tasks.append(taskLink(thread, project.name, true)));
  if (!runtimeProcesses.length && !recent.length) {
    tasks.append(element("span", "quiet-card", "暂无最近活动"));
  } else {
    tasks.setAttribute("aria-label", "项目活动");
  }
  card.append(tasks);

  const footer = element("div", "project-footer");
  const runtimeCount = project.runtime?.processCount || 0;
  const meta = element("span", "project-meta", `${project.threadTotal} 任务 · ${relativeTime(project.lastActivityAt)}`);
  if (runtimeCount) meta.append(element("span", "process-count", `${runtimeCount} 进程`));
  footer.append(meta);
  const controls = element("div", "card-actions");
  controls.append(projectPrioritySelect(project));
  if (state.sortMode === "priority" && priority !== "unset") {
    const orderControls = element("span", "order-controls");
    orderControls.setAttribute("aria-label", `${priorityInfo[priority].short} 内排序`);
    orderControls.append(orderButton(project, -1), orderButton(project, 1));
    controls.append(orderControls);
  }
  const action = element("button", "project-action", "查看");
  action.type = "button";
  action.setAttribute("aria-label", `查看 ${project.name} 的任务`);
  action.addEventListener("click", () => selectProject(project));
  controls.append(action);
  footer.append(controls);
  card.append(footer);
  return card;
}

function showPreferenceStatus(message, isError = false) {
  const status = byId("preference-status");
  status.textContent = message;
  status.classList.toggle("error", isError);
}

function beginPreferenceInteraction() {
  if (state.preferenceEditing) return;
  state.preferenceEditing = true;
  state.preferenceInteractionVersion += 1;
}

function endPreferenceInteraction({ flush = true } = {}) {
  state.preferenceEditing = false;
  if (flush && state.refreshPending) {
    state.refreshPending = false;
    requestDashboardLoad();
  } else if (!flush) {
    state.refreshPending = false;
  }
}

function requestDashboardLoad({ force = false } = {}) {
  if (!force && state.preferenceEditing) {
    state.refreshPending = true;
    return Promise.resolve(false);
  }
  state.refreshPending = false;
  return loadDashboard();
}

async function updateProjectPriority(project, select) {
  const previous = project.priority || "unset";
  beginPreferenceInteraction();
  select.dataset.saving = "true";
  select.disabled = true;
  showPreferenceStatus(`正在保存 ${project.name}…`);
  try {
    const response = await requestJSON(`/api/v1/projects/${encodeURIComponent(project.id)}/preferences`, {
      method: "PATCH",
      body: JSON.stringify({ priority: select.value }),
    });
    project.priority = response.data.priority;
    project.priorityRank = response.data.rank;
    project.preferenceUpdatedAt = response.data.updatedAt;
    sortProjectsLocally();
    endPreferenceInteraction({ flush: false });
    renderProjects();
    showPreferenceStatus(`${project.name} 已设为 ${priorityInfo[project.priority].short}`);
  } catch (error) {
    project.priority = previous;
    select.value = previous;
    showPreferenceStatus(error instanceof Error ? error.message : "优先级保存失败", true);
    endPreferenceInteraction();
  } finally {
    delete select.dataset.saving;
    select.disabled = false;
  }
}

function sortProjectsLocally() {
  const priorityWeight = { p0: 0, p1: 1, p2: 2, p3: 3, unset: 4 };
  state.projects.sort((left, right) => {
    if (state.sortMode === "name") return left.name.localeCompare(right.name, "zh-CN");
    if (state.sortMode === "last_activity") return new Date(right.lastActivityAt) - new Date(left.lastActivityAt);
    if (state.sortMode === "priority") {
      const priorityDifference = priorityWeight[left.priority || "unset"] - priorityWeight[right.priority || "unset"];
      if (priorityDifference) return priorityDifference;
      if ((left.priority || "unset") !== "unset" && left.priorityRank !== right.priorityRank) {
        return (left.priorityRank || Number.MAX_SAFE_INTEGER) - (right.priorityRank || Number.MAX_SAFE_INTEGER);
      }
    }
    const statusWeight = (project) => project.status === "working" ? 0 : attentionStatuses.has(project.status) ? 1 : project.status === "completed" ? 2 : 3;
    return statusWeight(left) - statusWeight(right) || new Date(right.lastActivityAt) - new Date(left.lastActivityAt);
  });
}

async function saveProjectOrder(priority, orderedProjects) {
  orderedProjects.forEach((project, index) => { project.priorityRank = (index + 1) * 1000; });
  sortProjectsLocally();
  renderProjects();
  showPreferenceStatus("正在保存项目顺序…");
  try {
    await requestJSON("/api/v1/project-order", {
      method: "PUT",
      body: JSON.stringify({ priority, projectIds: orderedProjects.map((project) => project.id) }),
    });
    showPreferenceStatus(`${priorityInfo[priority].short} 顺序已保存`);
  } catch (error) {
    showPreferenceStatus(error instanceof Error ? error.message : "顺序保存失败", true);
    await requestDashboardLoad({ force: true });
  }
}

function moveProject(project, direction) {
  const peers = priorityPeers(project);
  const index = peers.findIndex((candidate) => candidate.id === project.id);
  const target = index + direction;
  if (index < 0 || target < 0 || target >= peers.length) return;
  [peers[index], peers[target]] = [peers[target], peers[index]];
  saveProjectOrder(project.priority, peers);
}

function reorderBefore(draggedID, targetID) {
  const dragged = state.projects.find((project) => project.id === draggedID);
  const target = state.projects.find((project) => project.id === targetID);
  if (!dragged || !target || dragged.priority !== target.priority || dragged.id === target.id) return;
  const peers = priorityPeers(dragged).filter((project) => project.id !== dragged.id);
  const targetIndex = peers.findIndex((project) => project.id === target.id);
  peers.splice(targetIndex, 0, dragged);
  saveProjectOrder(dragged.priority, peers);
}

function renderProjects() {
  const visible = state.projects.filter(matchesFilter);
  const container = byId("projects");
  container.replaceChildren(...visible.map(renderProject));
  byId("visible-count").textContent = `显示 ${visible.length} / ${state.projects.length}`;
  byId("empty").hidden = visible.length !== 0;
}

function selectProject(project) {
  state.selectedProject = project.id;
  const panel = document.querySelector(".activity-panel");
  panel.hidden = false;
  panel.classList.add("attention-empty");
  byId("recent-title").textContent = project.name;
  renderActivityList(byId("recent-list"), projectThreads(project.id).slice(0, 12), project.name);
  byId("activity-title").textContent = "项目任务";
  byId("recent-list").scrollIntoView({ behavior: "smooth", block: "nearest" });
}

function renderActivityList(container, threads, fixedProjectName) {
  if (!threads.length) {
    container.replaceChildren(element("div", "quiet-empty", "当前没有需要显示的任务。"));
    return;
  }
  const projectNames = new Map(state.projects.map((project) => [project.id, project.name]));
  container.replaceChildren(...threads.map((thread) => taskLink(thread, fixedProjectName || projectNames.get(thread.projectId))));
}

function renderAttention() {
  const container = byId("attention-list");
  const panel = document.querySelector(".activity-panel");
  const section = byId("attention-section");
  const divider = panel.querySelector(".divider");
  const runtimeAlerts = state.projects.flatMap((project) =>
    (project.runtime?.processes || [])
      .filter((process) => process.status === "restarting")
      .map((process) => runtimeAlert(project, process)),
  );
  const projectNames = new Map(state.projects.map((project) => [project.id, project.name]));
  const threadAlerts = (state.overview.attention || []).map((thread) => taskLink(thread, projectNames.get(thread.projectId)));
  const alerts = [...runtimeAlerts, ...threadAlerts];
  if (!alerts.length) {
    section.hidden = true;
    divider.hidden = true;
    panel.classList.add("attention-empty");
    panel.hidden = !state.selectedProject;
    container.replaceChildren();
    return;
  }
  section.hidden = false;
  divider.hidden = false;
  panel.classList.remove("attention-empty");
  panel.hidden = false;
  container.replaceChildren(...alerts);
}

function renderOverview() {
  const summary = state.overview.summary;
  const counts = summary.statusCounts || {};
  const attention = ["waiting", "interrupted", "failed", "suspected_abnormal"]
    .reduce((total, status) => total + (counts[status] || 0), 0);
  byId("metric-working").textContent = summary.activeProjectCount ?? (counts.working || 0);
  byId("metric-attention").textContent = summary.attentionProjectCount ?? attention;
  byId("metric-projects").textContent = summary.projectCount || 0;
  byId("metric-threads").textContent = summary.threadCount || 0;
  byId("metric-unmapped").textContent = summary.unmappedThreadCount || 0;

  const sources = state.overview.sources || {};
  const sourceValues = Object.values(sources);
  const healthy = sourceValues.length > 0 && sourceValues.every((source) => source.status === "healthy");
  byId("source-dot").className = `source-dot ${healthy ? "healthy" : "degraded"}`;
  byId("source-label").textContent = healthy ? "本地数据源正常" : "部分数据源需要检查";
  byId("updated-at").textContent = `更新于 ${relativeTime(state.generatedAt)}`;

  renderStatusDistribution();
  renderAttention();
  if (!state.selectedProject) {
    renderActivityList(byId("recent-list"), state.overview.recent || []);
  }
}

async function requestJSON(path, options = {}) {
  const headers = { Accept: "application/json", ...(options.headers || {}) };
  if (options.body !== undefined) headers["Content-Type"] = "application/json";
  const response = await fetch(path, { ...options, headers, cache: "no-store" });
  if (!response.ok) throw new Error(`本地服务返回 ${response.status}`);
  return response.json();
}

const fetchJSON = (path) => requestJSON(path);

async function loadDashboard() {
  const interactionVersion = state.preferenceInteractionVersion;
  byId("refresh").disabled = true;
  try {
    const [overview, projects, threads] = await Promise.all([
      fetchJSON("/api/v1/overview"),
      fetchJSON(`/api/v1/projects?limit=100&sort=${encodeURIComponent(state.sortMode)}`),
      fetchJSON("/api/v1/threads?limit=100"),
    ]);
    // Project rendering replaces native selects, so discard any response that
    // overlapped a priority interaction instead of interrupting the user.
    if (state.preferenceEditing || interactionVersion !== state.preferenceInteractionVersion) {
      state.refreshPending = true;
      return false;
    }
    state.overview = overview.data;
    state.generatedAt = overview.meta.generatedAt;
    state.projects = projects.data;
    state.threads = threads.data;
    byId("loading").hidden = true;
    byId("error").hidden = true;
    renderOverview();
    renderProjects();
    return true;
  } catch (error) {
    byId("loading").hidden = true;
    byId("error").hidden = false;
    byId("error-message").textContent = error instanceof Error ? error.message : "无法连接本地服务";
    return false;
  } finally {
    byId("refresh").disabled = false;
  }
}

function setupFilters() {
  document.querySelectorAll(".filter").forEach((button) => {
    button.addEventListener("click", () => setFilter(button.dataset.filter));
  });
  byId("search").addEventListener("input", (event) => {
    state.query = event.target.value.trim();
    renderProjects();
  });
  byId("sort").addEventListener("change", (event) => {
    state.sortMode = event.target.value;
    showPreferenceStatus("");
    requestDashboardLoad({ force: true });
  });
}

function setFilter(filter) {
  state.filter = filter;
  document.querySelectorAll(".filter").forEach((item) => {
    const active = item.dataset.filter === filter;
    item.classList.toggle("active", active);
    item.setAttribute("aria-pressed", String(active));
  });
  renderProjects();
}

function setupLiveUpdates() {
  const indicator = byId("connection");
  const label = byId("connection-text");
  const events = new EventSource("/api/v1/events");
  events.addEventListener("open", () => {
    indicator.className = "connection live";
    label.textContent = "实时连接";
  });
  events.addEventListener("snapshot.updated", () => requestDashboardLoad());
  events.addEventListener("error", () => {
    indicator.className = "connection offline";
    label.textContent = "正在重连";
  });
}

byId("refresh").addEventListener("click", () => requestDashboardLoad({ force: true }));
byId("retry").addEventListener("click", () => requestDashboardLoad({ force: true }));
setupTheme();
setupFilters();
setupLiveUpdates();
requestDashboardLoad();
setInterval(requestDashboardLoad, 30_000);
