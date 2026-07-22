package webui

import (
	"io/fs"
	"strings"
	"testing"
)

func TestEmbeddedDashboardContainsRequiredAssetsAndAvoidsUnsafeHTMLRendering(t *testing.T) {
	for _, name := range []string{"index.html", "styles.css", "app.js"} {
		if _, err := fs.ReadFile(Assets, name); err != nil {
			t.Fatalf("read embedded %s: %v", name, err)
		}
	}

	page, _ := fs.ReadFile(Assets, "index.html")
	for _, required := range []string{`id="projects"`, `id="connection"`, `id="search"`, `id="project-distribution"`, `class="overview-strip"`} {
		if !strings.Contains(string(page), required) {
			t.Fatalf("index.html missing %s", required)
		}
	}
	for _, required := range []string{`data-theme="light"`, `id="theme-toggle"`, `content="light dark"`} {
		if !strings.Contains(string(page), required) {
			t.Fatalf("index.html missing theme support %s", required)
		}
	}

	script, _ := fs.ReadFile(Assets, "app.js")
	if strings.Contains(string(script), "innerHTML") {
		t.Fatal("app.js must render Codex-sourced strings through textContent, not innerHTML")
	}
	for _, required := range []string{"agent-task-monitor-theme", "document.documentElement.dataset.theme", "localStorage.setItem"} {
		if !strings.Contains(string(script), required) {
			t.Fatalf("app.js missing persistent theme behavior %q", required)
		}
	}
	for _, required := range []string{"project.runtime", "后台运行", "频繁重启"} {
		if !strings.Contains(string(script), required) {
			t.Fatalf("app.js missing project runtime presentation %q", required)
		}
	}
	for _, required := range []string{"renderStatusDistribution", "project-status-rail", "runtime-pulse", "项目状态分布"} {
		if !strings.Contains(string(script), required) {
			t.Fatalf("app.js missing compact visualization behavior %q", required)
		}
	}
	for _, required := range []string{"project.summary", "project-summary", "待补充项目介绍"} {
		if !strings.Contains(string(script), required) {
			t.Fatalf("app.js missing project summary behavior %q", required)
		}
	}
	for _, required := range []string{"priority-select", "project-order", "dragstart", "上移", "下移"} {
		if !strings.Contains(string(script), required) && !strings.Contains(string(page), required) {
			t.Fatalf("dashboard missing priority interaction %q", required)
		}
	}
	for _, required := range []string{
		"preferenceInteractionVersion",
		"refreshPending",
		"beginPreferenceInteraction",
		"requestDashboardLoad",
		"interactionVersion !== state.preferenceInteractionVersion",
	} {
		if !strings.Contains(string(script), required) {
			t.Fatalf("app.js missing priority refresh protection %q", required)
		}
	}
	if strings.Contains(string(script), `events.addEventListener("snapshot.updated", () => loadDashboard())`) {
		t.Fatal("live updates must not replace project controls during a priority interaction")
	}

	styles, _ := fs.ReadFile(Assets, "styles.css")
	stylesText := string(styles)
	if !strings.Contains(stylesText, "--body-size: 16px") {
		t.Fatal("styles.css must keep the dashboard body type at a readable 16px baseline")
	}
	if !strings.Contains(stylesText, `:root[data-theme="light"]`) {
		t.Fatal("styles.css must define a first-class light color system")
	}
	if strings.Contains(stylesText, ".activity-panel { position: sticky") {
		t.Fatal("activity panel must stay in document flow and never cover project content")
	}
	if !strings.Contains(stylesText, ".project-card::before") || !strings.Contains(stylesText, ".status-card-working") {
		t.Fatal("project cards must have a visible surface and status accent")
	}
	gridRule := cssRule(stylesText, ".project-grid, .loading-grid")
	if !strings.Contains(gridRule, "grid-template-columns: repeat(3") {
		t.Fatalf("desktop project grid must use compact three-column cards; rule = %q", gridRule)
	}
	cardRule := cssRule(stylesText, ".project-card")
	if !strings.Contains(cardRule, "padding: 18px") {
		t.Fatalf("project cards must use compact spacing; rule = %q", cardRule)
	}
	projectGridRule := cssRule(stylesText, ".project-grid")
	if !strings.Contains(projectGridRule, "grid-auto-rows: 1fr") || !strings.Contains(projectGridRule, "align-items: stretch") {
		t.Fatalf("project grid must enforce equal card heights; rule = %q", projectGridRule)
	}
	if !strings.Contains(cardRule, "height: 100%") {
		t.Fatalf("project cards must fill the shared grid row height; rule = %q", cardRule)
	}
	for _, selector := range []string{".status-distribution", ".project-status-rail", ".runtime-pulse"} {
		if cssRule(stylesText, selector) == "" {
			t.Fatalf("styles.css missing visualization selector %s", selector)
		}
	}
	summaryRule := cssRule(stylesText, ".project-summary")
	if !strings.Contains(summaryRule, "-webkit-line-clamp: 2") || !strings.Contains(summaryRule, "overflow-wrap: anywhere") {
		t.Fatalf("project summaries must use a fixed wrapping two-line block; rule = %q", summaryRule)
	}
	for _, selector := range []string{".project-name h3", ".mini-task-title", ".activity-title"} {
		rule := cssRule(stylesText, selector)
		if !strings.Contains(rule, "overflow-wrap: anywhere") || strings.Contains(rule, "white-space: nowrap") {
			t.Fatalf("%s must wrap long content inside its card; rule = %q", selector, rule)
		}
	}

	pageText := string(page)
	activityIndex := strings.Index(pageText, `class="activity-panel"`)
	projectsIndex := strings.Index(pageText, `class="project-area"`)
	if activityIndex < 0 || projectsIndex < 0 || activityIndex > projectsIndex {
		t.Fatal("attention and activity must appear before the full project grid")
	}
}

func cssRule(styles, selector string) string {
	start := strings.Index(styles, selector+" {")
	if start < 0 {
		return ""
	}
	remaining := styles[start:]
	end := strings.Index(remaining, "}")
	if end < 0 {
		return remaining
	}
	return remaining[:end+1]
}
