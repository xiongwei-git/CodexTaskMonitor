package probe

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/url"
	"path/filepath"
	"sort"
	"time"

	"github.com/xiongwei-git/agent-task-monitor/internal/codexstate"
	"github.com/xiongwei-git/agent-task-monitor/internal/processprobe"
	"github.com/xiongwei-git/agent-task-monitor/internal/projectmeta"
	"github.com/xiongwei-git/agent-task-monitor/internal/projectprefs"
	"github.com/xiongwei-git/agent-task-monitor/internal/projectprocess"
	"github.com/xiongwei-git/agent-task-monitor/internal/projectresolver"
	"github.com/xiongwei-git/agent-task-monitor/internal/sessionevents"
	"github.com/xiongwei-git/agent-task-monitor/internal/statusengine"
)

type Options struct {
	StateDBPath            string
	ProjectRoot            string
	ProjectRegistryPath    string
	ProjectPreferencesPath string
	StaleAfter             time.Duration
	Now                    func() time.Time
	DetectProcesses        func(context.Context) (processprobe.Snapshot, error)
	DetectProjectProcesses func(context.Context, string) ([]projectprocess.Activity, error)
}

type Report struct {
	GeneratedAt time.Time `json:"generatedAt"`
	StaleAfter  string    `json:"staleAfter"`
	Sources     Sources   `json:"sources"`
	Summary     Summary   `json:"summary"`
	Projects    []Project `json:"projects"`
}

type Sources struct {
	StateDB            StateDBSource            `json:"stateDb"`
	Sessions           SessionsSource           `json:"sessions"`
	Processes          ProcessSource            `json:"processes"`
	ProjectMetadata    ProjectMetadataSource    `json:"projectMetadata"`
	ProjectPreferences ProjectPreferencesSource `json:"projectPreferences"`
}

type ProjectMetadataSource struct {
	Status      string `json:"status"`
	RecordCount int    `json:"recordCount"`
}

type ProjectPreferencesSource struct {
	Status      string `json:"status"`
	RecordCount int    `json:"recordCount"`
}

type StateDBSource struct {
	Status        string `json:"status"`
	ThreadRecords int    `json:"threadRecords"`
}

type SessionsSource struct {
	Status       string `json:"status"`
	FilesScanned int    `json:"filesScanned"`
	FilesMissing int    `json:"filesMissing"`
	ParseErrors  int    `json:"parseErrors"`
}

type ProcessSource struct {
	Status                 string `json:"status"`
	CodexRunning           bool   `json:"codexRunning"`
	CodexProcessCount      int    `json:"codexProcessCount"`
	ChatGPTAppRunning      bool   `json:"chatgptAppRunning"`
	ProjectProcessesStatus string `json:"projectProcessesStatus"`
	ProjectProcessCount    int    `json:"projectProcessCount"`
}

type Summary struct {
	ProjectCount           int                         `json:"projectCount"`
	ActiveProjectCount     int                         `json:"activeProjectCount"`
	AttentionProjectCount  int                         `json:"attentionProjectCount"`
	BackgroundProcessCount int                         `json:"backgroundProcessCount"`
	ThreadCount            int                         `json:"threadCount"`
	UnmappedThreadCount    int                         `json:"unmappedThreadCount"`
	StatusCounts           map[statusengine.Status]int `json:"statusCounts"`
}

type Project struct {
	ID                  string                      `json:"id"`
	Name                string                      `json:"name"`
	CanonicalPath       string                      `json:"canonicalPath"`
	Summary             string                      `json:"summary"`
	SummarySource       string                      `json:"summarySource"`
	SummaryQuality      string                      `json:"summaryQuality"`
	SummaryUpdatedAt    string                      `json:"summaryUpdatedAt,omitempty"`
	Priority            projectprefs.Priority       `json:"priority"`
	PriorityRank        int                         `json:"priorityRank"`
	PreferenceUpdatedAt *time.Time                  `json:"preferenceUpdatedAt,omitempty"`
	Status              statusengine.Status         `json:"status"`
	StatusQuality       statusengine.Quality        `json:"statusQuality"`
	LastActivityAt      time.Time                   `json:"lastActivityAt"`
	ThreadCounts        map[statusengine.Status]int `json:"threadCounts"`
	Runtime             ProjectRuntime              `json:"runtime"`
	Threads             []Thread                    `json:"threads"`
}

type ProjectRuntime struct {
	Status        projectprocess.Status  `json:"status"`
	StatusQuality projectprocess.Quality `json:"statusQuality,omitempty"`
	ProcessCount  int                    `json:"processCount"`
	Processes     []BackgroundProcess    `json:"processes"`
}

type BackgroundProcess struct {
	ID            string                 `json:"id"`
	Runtime       string                 `json:"runtime"`
	Label         string                 `json:"label"`
	Status        projectprocess.Status  `json:"status"`
	StatusQuality projectprocess.Quality `json:"statusQuality"`
	StartedAt     time.Time              `json:"startedAt"`
	RestartCount  int                    `json:"restartCount"`
}

type Thread struct {
	ID             string               `json:"id"`
	ProjectID      string               `json:"projectId"`
	Title          string               `json:"title"`
	Status         statusengine.Status  `json:"status"`
	StatusQuality  statusengine.Quality `json:"statusQuality"`
	StatusReason   statusengine.Reason  `json:"statusReason"`
	CreatedAt      time.Time            `json:"createdAt"`
	UpdatedAt      time.Time            `json:"updatedAt"`
	StartedAt      *time.Time           `json:"startedAt,omitempty"`
	LastActivityAt time.Time            `json:"lastActivityAt"`
	DeepLink       string               `json:"deepLink"`
	Source         string               `json:"source"`
	CLIVersion     string               `json:"cliVersion"`
}

type sessionScan struct {
	summary sessionevents.Summary
	err     error
}

func Run(ctx context.Context, options Options) (Report, error) {
	if options.StateDBPath == "" {
		return Report{}, fmt.Errorf("state database path is required")
	}
	if options.ProjectRoot == "" {
		return Report{}, fmt.Errorf("project root is required")
	}
	if options.StaleAfter <= 0 {
		return Report{}, fmt.Errorf("stale duration must be positive")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.DetectProcesses == nil {
		options.DetectProcesses = processprobe.Detect
	}
	if options.DetectProjectProcesses == nil {
		observer := projectprocess.NewObserver(projectprocess.Options{Now: options.Now})
		options.DetectProjectProcesses = observer.Detect
	}

	projectRoot, err := filepath.Abs(options.ProjectRoot)
	if err != nil {
		return Report{}, fmt.Errorf("resolve project root: %w", err)
	}
	now := options.Now().UTC()
	if options.ProjectRegistryPath == "" {
		options.ProjectRegistryPath = filepath.Join(projectRoot, "ProjectNavigator", "project-registry.json")
	}
	projectSummaries, metadataErr := projectmeta.Load(options.ProjectRegistryPath)
	if options.ProjectPreferencesPath == "" {
		options.ProjectPreferencesPath = filepath.Join(projectRoot, "ProjectNavigator", "project-preferences.json")
	}
	projectPreferences, preferencesErr := projectprefs.Load(options.ProjectPreferencesPath)

	reader, err := codexstate.Open(options.StateDBPath)
	if err != nil {
		return Report{}, err
	}
	defer reader.Close()

	threadRecords, err := reader.ListThreads(ctx, codexstate.ListOptions{Archived: false})
	if err != nil {
		return Report{}, err
	}

	report := Report{
		GeneratedAt: now,
		StaleAfter:  options.StaleAfter.String(),
		Sources: Sources{
			StateDB:            StateDBSource{Status: "healthy", ThreadRecords: len(threadRecords)},
			Sessions:           SessionsSource{Status: "healthy"},
			Processes:          ProcessSource{Status: "healthy", ProjectProcessesStatus: "healthy"},
			ProjectMetadata:    ProjectMetadataSource{Status: "healthy", RecordCount: len(projectSummaries)},
			ProjectPreferences: ProjectPreferencesSource{Status: "healthy", RecordCount: len(projectPreferences)},
		},
		Summary: Summary{StatusCounts: make(map[statusengine.Status]int)},
	}
	if metadataErr != nil {
		report.Sources.ProjectMetadata.Status = "degraded"
	}
	if preferencesErr != nil {
		report.Sources.ProjectPreferences.Status = "degraded"
	}

	processes, processErr := options.DetectProcesses(ctx)
	if processErr != nil {
		report.Sources.Processes.Status = "degraded"
	} else {
		report.Sources.Processes.CodexRunning = processes.CodexRunning
		report.Sources.Processes.CodexProcessCount = processes.CodexProcessCount
		report.Sources.Processes.ChatGPTAppRunning = processes.ChatGPTAppRunning
	}

	projectsByPath := make(map[string]*Project)
	sessionCache := make(map[string]sessionScan)
	projectCache := make(map[string]projectresolver.Project)
	unresolvedProjects := make(map[string]struct{})

	for _, record := range threadRecords {
		resolved, ok := projectCache[record.CWD]
		if !ok {
			if _, knownUnresolved := unresolvedProjects[record.CWD]; !knownUnresolved {
				resolved, ok = projectresolver.ResolveWorkspace(projectRoot, record.CWD)
				if ok {
					projectCache[record.CWD] = resolved
				} else {
					unresolvedProjects[record.CWD] = struct{}{}
				}
			}
		}
		if !ok {
			report.Summary.UnmappedThreadCount++
			continue
		}

		project := projectsByPath[resolved.Path]
		if project == nil {
			project = newProject(resolved.Name, resolved.Path)
			projectsByPath[resolved.Path] = project
		}

		scanned, cached := sessionCache[record.RolloutPath]
		if !cached {
			scanned.summary, scanned.err = sessionevents.ScanFile(record.RolloutPath)
			sessionCache[record.RolloutPath] = scanned
			if scanned.err != nil {
				report.Sources.Sessions.FilesMissing++
				report.Sources.Sessions.Status = "degraded"
			} else {
				report.Sources.Sessions.FilesScanned++
				report.Sources.Sessions.ParseErrors += scanned.summary.ParseErrors
				if scanned.summary.ParseErrors > 0 {
					report.Sources.Sessions.Status = "degraded"
				}
			}
		}

		assessment := statusengine.Evaluate(scanned.summary.Lifecycle, now, options.StaleAfter)
		lastActivity := record.UpdatedAt
		if scanned.summary.Lifecycle.LastActivityAt != nil {
			lastActivity = scanned.summary.Lifecycle.LastActivityAt.UTC()
		}

		thread := Thread{
			ID:             record.ID,
			ProjectID:      project.ID,
			Title:          record.Title,
			Status:         assessment.Status,
			StatusQuality:  assessment.Quality,
			StatusReason:   assessment.Reason,
			CreatedAt:      record.CreatedAt,
			UpdatedAt:      record.UpdatedAt,
			StartedAt:      scanned.summary.Lifecycle.StartedAt,
			LastActivityAt: lastActivity,
			DeepLink:       threadDeepLink(record.ID),
			Source:         "codex_app",
			CLIVersion:     record.CLIVersion,
		}
		project.Threads = append(project.Threads, thread)
		project.ThreadCounts[thread.Status]++
		report.Summary.StatusCounts[thread.Status]++
		report.Summary.ThreadCount++
	}

	activities, projectProcessErr := options.DetectProjectProcesses(ctx, projectRoot)
	if projectProcessErr != nil {
		report.Sources.Processes.Status = "degraded"
		report.Sources.Processes.ProjectProcessesStatus = "degraded"
	} else {
		for _, activity := range activities {
			path := filepath.Clean(activity.ProjectPath)
			project := projectsByPath[path]
			if project == nil {
				project = newProject(activity.ProjectName, path)
				projectsByPath[path] = project
			}
			project.Runtime.Processes = append(project.Runtime.Processes, BackgroundProcess{
				ID: activity.ID, Runtime: activity.Runtime, Label: activity.Label,
				Status: activity.Status, StatusQuality: activity.Quality,
				StartedAt: activity.StartedAt, RestartCount: activity.RestartCount,
			})
			project.Runtime.ProcessCount++
			report.Summary.BackgroundProcessCount++
			report.Sources.Processes.ProjectProcessCount++
			if activity.Status == projectprocess.StatusRestarting {
				project.Runtime.Status = projectprocess.StatusRestarting
				project.Runtime.StatusQuality = projectprocess.QualityUncertain
			} else if project.Runtime.Status == projectprocess.StatusNone {
				project.Runtime.Status = projectprocess.StatusRunning
				project.Runtime.StatusQuality = projectprocess.QualityInferred
			}
		}
	}

	for _, project := range projectsByPath {
		if summary, ok := projectSummaries[filepath.Clean(project.CanonicalPath)]; ok {
			project.Summary = summary.Text
			project.SummarySource = summary.Source
			project.SummaryQuality = summary.Quality
			project.SummaryUpdatedAt = summary.UpdatedAt
		}
		project.Priority = projectprefs.PriorityUnset
		if preference, ok := projectPreferences[project.Name]; ok {
			project.Priority = preference.Priority
			project.PriorityRank = preference.Rank
			updatedAt := preference.UpdatedAt
			project.PreferenceUpdatedAt = &updatedAt
		}
		sort.Slice(project.Threads, func(i, j int) bool {
			left := project.Threads[i]
			right := project.Threads[j]
			if statusPriority(left.Status) != statusPriority(right.Status) {
				return statusPriority(left.Status) < statusPriority(right.Status)
			}
			return left.LastActivityAt.After(right.LastActivityAt)
		})
		if len(project.Threads) > 0 {
			leading := project.Threads[0]
			project.Status = leading.Status
			project.StatusQuality = leading.StatusQuality
			for _, thread := range project.Threads {
				if thread.LastActivityAt.After(project.LastActivityAt) {
					project.LastActivityAt = thread.LastActivityAt
				}
			}
		}
		if project.Runtime.Status == projectprocess.StatusRestarting {
			project.Status = statusengine.StatusSuspectedAbnormal
			project.StatusQuality = statusengine.QualityUncertain
		} else if project.Runtime.Status == projectprocess.StatusRunning {
			project.Status = statusengine.StatusWorking
			project.StatusQuality = statusengine.QualityInferred
		}
		for _, process := range project.Runtime.Processes {
			if process.StartedAt.After(project.LastActivityAt) {
				project.LastActivityAt = process.StartedAt
			}
		}
		if project.Status == statusengine.StatusWorking {
			report.Summary.ActiveProjectCount++
		}
		if needsAttention(project.Status) {
			report.Summary.AttentionProjectCount++
		}
		report.Projects = append(report.Projects, *project)
	}

	sort.Slice(report.Projects, func(i, j int) bool {
		left := report.Projects[i]
		right := report.Projects[j]
		if statusPriority(left.Status) != statusPriority(right.Status) {
			return statusPriority(left.Status) < statusPriority(right.Status)
		}
		if !left.LastActivityAt.Equal(right.LastActivityAt) {
			return left.LastActivityAt.After(right.LastActivityAt)
		}
		return left.Name < right.Name
	})

	report.Summary.ProjectCount = len(report.Projects)
	return report, nil
}

func needsAttention(status statusengine.Status) bool {
	return status == statusengine.StatusWaiting || status == statusengine.StatusInterrupted ||
		status == statusengine.StatusFailed || status == statusengine.StatusSuspectedAbnormal
}

func newProject(name, path string) *Project {
	return &Project{
		ID: stableProjectID(path), Name: name, CanonicalPath: path,
		Status: statusengine.StatusUnknown, StatusQuality: statusengine.QualityUncertain,
		ThreadCounts: make(map[statusengine.Status]int),
		Runtime:      ProjectRuntime{Status: projectprocess.StatusNone, Processes: []BackgroundProcess{}},
	}
}

func stableProjectID(path string) string {
	hash := sha256.Sum256([]byte(filepath.Clean(path)))
	return "project_" + hex.EncodeToString(hash[:8])
}

func threadDeepLink(threadID string) string {
	return (&url.URL{Scheme: "codex", Host: "threads", Path: "/" + threadID}).String()
}

func statusPriority(status statusengine.Status) int {
	switch status {
	case statusengine.StatusSuspectedAbnormal:
		return 0
	case statusengine.StatusWaiting:
		return 1
	case statusengine.StatusWorking:
		return 2
	case statusengine.StatusFailed:
		return 3
	case statusengine.StatusInterrupted:
		return 4
	case statusengine.StatusCompleted:
		return 5
	case statusengine.StatusIdle:
		return 6
	default:
		return 7
	}
}
