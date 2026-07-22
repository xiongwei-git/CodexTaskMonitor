package probe

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/xiongwei-git/agent-task-monitor/internal/processprobe"
	"github.com/xiongwei-git/agent-task-monitor/internal/projectprocess"
	"github.com/xiongwei-git/agent-task-monitor/internal/statusengine"
	_ "modernc.org/sqlite"
)

func TestRunBuildsAProjectReportFromReadOnlyCodexState(t *testing.T) {
	tempDir := t.TempDir()
	projectRoot := filepath.Join(tempDir, "CodeX")
	alphaPath := filepath.Join(projectRoot, "Alpha")
	if err := os.MkdirAll(filepath.Join(alphaPath, "internal"), 0o755); err != nil {
		t.Fatalf("create project directories: %v", err)
	}

	sessionsDir := filepath.Join(tempDir, "sessions")
	if err := os.MkdirAll(sessionsDir, 0o755); err != nil {
		t.Fatalf("create sessions directory: %v", err)
	}

	now := time.Date(2026, 7, 21, 4, 0, 0, 0, time.UTC)
	workingSession := filepath.Join(sessionsDir, "working.jsonl")
	completedSession := filepath.Join(sessionsDir, "completed.jsonl")
	writeSession(t, workingSession, []string{
		`{"timestamp":"2026-07-21T03:58:00Z","type":"event_msg","payload":{"type":"task_started","secret":"secret-message"}}`,
		`{"timestamp":"2026-07-21T03:59:55Z","type":"event_msg","payload":{"type":"agent_reasoning","text":"secret-message"}}`,
		`{"timestamp":"2026-07-21T03:59:56Z"`,
	})
	writeSession(t, completedSession, []string{
		`{"timestamp":"2026-07-21T03:40:00Z","type":"event_msg","payload":{"type":"task_started"}}`,
		`{"timestamp":"2026-07-21T03:41:00Z","type":"event_msg","payload":{"type":"task_complete"}}`,
	})

	dbPath := filepath.Join(tempDir, "state_5.sqlite")
	seedProbeDatabase(t, dbPath, []seedThread{
		{
			id:          "thread-working",
			rolloutPath: workingSession,
			cwd:         alphaPath,
			title:       "Working task",
			updatedAt:   now.Add(-5 * time.Second),
		},
		{
			id:          "thread-completed",
			rolloutPath: completedSession,
			cwd:         filepath.Join(alphaPath, "internal"),
			title:       "Completed task",
			updatedAt:   now.Add(-19 * time.Minute),
		},
		{
			id:          "thread-outside",
			rolloutPath: completedSession,
			cwd:         filepath.Join(tempDir, "Other", "Gamma"),
			title:       "Outside task",
			updatedAt:   now.Add(-time.Minute),
		},
	})

	options := Options{
		StateDBPath: dbPath,
		ProjectRoot: projectRoot,
		StaleAfter:  10 * time.Minute,
		Now:         func() time.Time { return now },
		DetectProcesses: func(context.Context) (processprobe.Snapshot, error) {
			return processprobe.Snapshot{CodexRunning: true, CodexProcessCount: 2, ChatGPTAppRunning: true}, nil
		},
	}
	report, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	if report.Summary.ProjectCount != 1 || report.Summary.ThreadCount != 2 {
		t.Fatalf("summary = %#v, want 1 project and 2 threads", report.Summary)
	}
	if report.Summary.UnmappedThreadCount != 1 {
		t.Fatalf("UnmappedThreadCount = %d, want 1", report.Summary.UnmappedThreadCount)
	}
	if len(report.Projects) != 1 || report.Projects[0].Name != "Alpha" {
		t.Fatalf("projects = %#v, want Alpha", report.Projects)
	}
	if report.Projects[0].Status != statusengine.StatusWorking {
		t.Fatalf("project status = %q, want working", report.Projects[0].Status)
	}
	if len(report.Projects[0].Threads) != 2 {
		t.Fatalf("len(project threads) = %d, want 2", len(report.Projects[0].Threads))
	}
	if report.Projects[0].Threads[0].ID != "thread-working" {
		t.Fatalf("first thread = %q, want working thread first", report.Projects[0].Threads[0].ID)
	}
	if report.Projects[0].Threads[0].DeepLink != "codex://threads/thread-working" {
		t.Fatalf("deep link = %q", report.Projects[0].Threads[0].DeepLink)
	}
	if report.Sources.Sessions.FilesScanned != 2 {
		t.Fatalf("FilesScanned = %d, want 2 unique files", report.Sources.Sessions.FilesScanned)
	}
	if report.Sources.Sessions.Status != "degraded" || report.Sources.Sessions.ParseErrors != 1 {
		t.Fatalf("sessions source = %#v, want one tolerated parse error and degraded status", report.Sources.Sessions)
	}

	encoded, err := json.Marshal(report)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	output := string(encoded)
	if strings.Contains(output, "secret-message") {
		t.Fatal("report leaked session content")
	}
	if strings.Contains(output, sessionsDir) {
		t.Fatal("report leaked raw session file paths")
	}
}

func TestRunPromotesACompletedProjectWhileItsBackgroundProcessIsRunning(t *testing.T) {
	tempDir := t.TempDir()
	projectRoot := filepath.Join(tempDir, "CodeX")
	projectPath := filepath.Join(projectRoot, "FeishuKnowNexus")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("create project: %v", err)
	}

	now := time.Date(2026, 7, 21, 5, 0, 0, 0, time.UTC)
	sessionPath := filepath.Join(tempDir, "completed.jsonl")
	writeSession(t, sessionPath, []string{
		`{"timestamp":"2026-07-21T04:40:00Z","type":"event_msg","payload":{"type":"task_started"}}`,
		`{"timestamp":"2026-07-21T04:41:00Z","type":"event_msg","payload":{"type":"task_complete"}}`,
	})
	dbPath := filepath.Join(tempDir, "state_5.sqlite")
	seedProbeDatabase(t, dbPath, []seedThread{{
		id: "thread-completed", rolloutPath: sessionPath, cwd: projectPath,
		title: "Start migration", updatedAt: now.Add(-19 * time.Minute),
	}})

	options := Options{
		StateDBPath: dbPath,
		ProjectRoot: projectRoot,
		StaleAfter:  10 * time.Minute,
		Now:         func() time.Time { return now },
		DetectProcesses: func(context.Context) (processprobe.Snapshot, error) {
			return processprobe.Snapshot{CodexRunning: true}, nil
		},
		DetectProjectProcesses: func(context.Context, string) ([]projectprocess.Activity, error) {
			return []projectprocess.Activity{{
				ID: "process_migration", ProjectName: "FeishuKnowNexus", ProjectPath: projectPath,
				Runtime: "Python", Label: "resume_tree_by_node.py", Status: projectprocess.StatusRunning,
				Quality: projectprocess.QualityInferred, StartedAt: now.Add(-10 * time.Minute),
			}}, nil
		},
	}
	report, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if len(report.Projects) != 1 {
		t.Fatalf("projects = %#v, want one", report.Projects)
	}
	project := report.Projects[0]
	if project.Status != statusengine.StatusWorking || project.StatusQuality != statusengine.QualityInferred {
		t.Fatalf("project status = %q/%q, want working/inferred", project.Status, project.StatusQuality)
	}
	if project.Threads[0].Status != statusengine.StatusCompleted {
		t.Fatalf("thread status = %q, want completed", project.Threads[0].Status)
	}
	if project.Runtime.Status != projectprocess.StatusRunning || project.Runtime.ProcessCount != 1 {
		t.Fatalf("project runtime = %#v", project.Runtime)
	}
	if report.Summary.ActiveProjectCount != 1 || report.Summary.BackgroundProcessCount != 1 {
		t.Fatalf("summary = %#v", report.Summary)
	}

	options.DetectProjectProcesses = func(context.Context, string) ([]projectprocess.Activity, error) {
		return []projectprocess.Activity{{
			ID: "process_migration", ProjectName: "FeishuKnowNexus", ProjectPath: projectPath,
			Runtime: "Python", Label: "resume_tree_by_node.py", Status: projectprocess.StatusRestarting,
			Quality: projectprocess.QualityUncertain, StartedAt: now.Add(-10 * time.Minute), RestartCount: 2,
		}}, nil
	}
	restartingReport, err := Run(context.Background(), options)
	if err != nil {
		t.Fatalf("Run() restarting error = %v", err)
	}
	if restartingReport.Projects[0].Status != statusengine.StatusSuspectedAbnormal ||
		restartingReport.Summary.AttentionProjectCount != 1 || restartingReport.Summary.ActiveProjectCount != 0 {
		t.Fatalf("restarting project report = %#v", restartingReport)
	}
}

func TestRunAddsProjectSummaryFromNavigatorRegistry(t *testing.T) {
	tempDir := t.TempDir()
	projectRoot := filepath.Join(tempDir, "CodeX")
	projectPath := filepath.Join(projectRoot, "Alpha")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("create project: %v", err)
	}
	sessionPath := filepath.Join(tempDir, "completed.jsonl")
	writeSession(t, sessionPath, []string{
		`{"timestamp":"2026-07-21T04:40:00Z","type":"event_msg","payload":{"type":"task_complete"}}`,
	})
	dbPath := filepath.Join(tempDir, "state_5.sqlite")
	seedProbeDatabase(t, dbPath, []seedThread{{
		id: "thread-alpha", rolloutPath: sessionPath, cwd: projectPath,
		title: "Alpha task", updatedAt: time.Date(2026, 7, 21, 5, 0, 0, 0, time.UTC),
	}})
	registryPath := filepath.Join(tempDir, "project-registry.json")
	registry := `{"version":3,"projects":[{"path":"` + projectPath + `","summary":"项目一句话介绍。","summary_source":"readme","summary_quality":"extracted","summary_updated_at":"2026-07-21"}]}`
	if err := os.WriteFile(registryPath, []byte(registry), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	report, err := Run(context.Background(), Options{
		StateDBPath: dbPath, ProjectRoot: projectRoot, ProjectRegistryPath: registryPath,
		StaleAfter: 10 * time.Minute,
		Now:        func() time.Time { return time.Date(2026, 7, 21, 5, 0, 0, 0, time.UTC) },
		DetectProcesses: func(context.Context) (processprobe.Snapshot, error) {
			return processprobe.Snapshot{}, nil
		},
		DetectProjectProcesses: func(context.Context, string) ([]projectprocess.Activity, error) {
			return nil, nil
		},
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	project := report.Projects[0]
	if project.Summary != "项目一句话介绍。" || project.SummarySource != "readme" || project.SummaryQuality != "extracted" {
		t.Fatalf("project summary = %#v", project)
	}
	if report.Sources.ProjectMetadata.Status != "healthy" || report.Sources.ProjectMetadata.RecordCount != 1 {
		t.Fatalf("metadata source = %#v", report.Sources.ProjectMetadata)
	}
}

func TestRunAddsProjectPreferenceFromNavigatorPreferences(t *testing.T) {
	tempDir := t.TempDir()
	projectRoot := filepath.Join(tempDir, "CodeX")
	projectPath := filepath.Join(projectRoot, "Alpha")
	if err := os.MkdirAll(projectPath, 0o755); err != nil {
		t.Fatalf("create project: %v", err)
	}
	sessionPath := filepath.Join(tempDir, "completed.jsonl")
	writeSession(t, sessionPath, []string{`{"timestamp":"2026-07-21T04:40:00Z","type":"event_msg","payload":{"type":"task_complete"}}`})
	dbPath := filepath.Join(tempDir, "state_5.sqlite")
	seedProbeDatabase(t, dbPath, []seedThread{{
		id: "thread-alpha", rolloutPath: sessionPath, cwd: projectPath,
		title: "Alpha task", updatedAt: time.Date(2026, 7, 21, 5, 0, 0, 0, time.UTC),
	}})
	preferencesPath := filepath.Join(tempDir, "project-preferences.json")
	preferences := `{"version":1,"projects":{"Alpha":{"priority":"p0","rank":1000,"updated_at":"2026-07-21T08:00:00Z"}}}`
	if err := os.WriteFile(preferencesPath, []byte(preferences), 0o600); err != nil {
		t.Fatalf("write preferences: %v", err)
	}

	report, err := Run(context.Background(), Options{
		StateDBPath: dbPath, ProjectRoot: projectRoot, ProjectPreferencesPath: preferencesPath,
		StaleAfter:             10 * time.Minute,
		Now:                    func() time.Time { return time.Date(2026, 7, 21, 5, 0, 0, 0, time.UTC) },
		DetectProcesses:        func(context.Context) (processprobe.Snapshot, error) { return processprobe.Snapshot{}, nil },
		DetectProjectProcesses: func(context.Context, string) ([]projectprocess.Activity, error) { return nil, nil },
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	project := report.Projects[0]
	if project.Priority != "p0" || project.PriorityRank != 1000 {
		t.Fatalf("project preference = %#v", project)
	}
	if report.Sources.ProjectPreferences.Status != "healthy" || report.Sources.ProjectPreferences.RecordCount != 1 {
		t.Fatalf("preferences source = %#v", report.Sources.ProjectPreferences)
	}
}

type seedThread struct {
	id          string
	rolloutPath string
	cwd         string
	title       string
	updatedAt   time.Time
}

func seedProbeDatabase(t *testing.T, path string, threads []seedThread) {
	t.Helper()

	db, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open seed database: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE threads (
			id TEXT PRIMARY KEY,
			rollout_path TEXT NOT NULL,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			cwd TEXT NOT NULL,
			title TEXT NOT NULL,
			archived INTEGER NOT NULL,
			cli_version TEXT NOT NULL
		)
	`)
	if err != nil {
		t.Fatalf("create threads table: %v", err)
	}

	for _, thread := range threads {
		_, err := db.Exec(
			`INSERT INTO threads VALUES (?, ?, ?, ?, ?, ?, 0, '0.145.0')`,
			thread.id,
			thread.rolloutPath,
			thread.updatedAt.Add(-time.Hour).Unix(),
			thread.updatedAt.Unix(),
			thread.cwd,
			thread.title,
		)
		if err != nil {
			t.Fatalf("insert thread %q: %v", thread.id, err)
		}
	}
}

func writeSession(t *testing.T, path string, lines []string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write session fixture: %v", err)
	}
}
