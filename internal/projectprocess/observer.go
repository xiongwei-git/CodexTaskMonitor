package projectprocess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	StatusNone       Status = "none"
	StatusRunning    Status = "running"
	StatusRestarting Status = "restarting"
)

type Quality string

const (
	QualityInferred  Quality = "inferred"
	QualityUncertain Quality = "uncertain"
)

type Activity struct {
	ID           string    `json:"id"`
	ProjectName  string    `json:"projectName"`
	ProjectPath  string    `json:"projectPath"`
	Runtime      string    `json:"runtime"`
	Label        string    `json:"label"`
	Status       Status    `json:"status"`
	Quality      Quality   `json:"statusQuality"`
	StartedAt    time.Time `json:"startedAt"`
	RestartCount int       `json:"restartCount"`
}

type Options struct {
	Now              func() time.Time
	RunPS            func(context.Context) (string, error)
	MinimumAge       time.Duration
	RestartWindow    time.Duration
	RestartThreshold int
}

type Observer struct {
	now              func() time.Time
	runPS            func(context.Context) (string, error)
	minimumAge       time.Duration
	restartWindow    time.Duration
	restartThreshold int

	mu      sync.Mutex
	history map[string]processHistory
}

type processHistory struct {
	PID       int
	LastSeen  time.Time
	StartedAt time.Time
	Restarts  []time.Time
}

type candidate struct {
	PID          int
	Elapsed      time.Duration
	ProjectName  string
	ProjectPath  string
	Runtime      string
	Label        string
	IdentityHash string
}

func NewObserver(options Options) *Observer {
	if options.Now == nil {
		options.Now = time.Now
	}
	if options.RunPS == nil {
		options.RunPS = runPS
	}
	if options.MinimumAge <= 0 {
		options.MinimumAge = 10 * time.Second
	}
	if options.RestartWindow <= 0 {
		options.RestartWindow = 5 * time.Minute
	}
	if options.RestartThreshold <= 0 {
		options.RestartThreshold = 2
	}
	return &Observer{
		now: options.Now, runPS: options.RunPS, minimumAge: options.MinimumAge,
		restartWindow: options.RestartWindow, restartThreshold: options.RestartThreshold,
		history: make(map[string]processHistory),
	}
}

func (observer *Observer) Detect(ctx context.Context, projectRoot string) ([]Activity, error) {
	if projectRoot == "" {
		return nil, fmt.Errorf("project root is required")
	}
	output, err := observer.runPS(ctx)
	if err != nil {
		return nil, fmt.Errorf("inspect project processes: %w", err)
	}
	now := observer.now().UTC().Truncate(time.Second)
	candidates := parsePSOutput(output, filepath.Clean(projectRoot), observer.minimumAge)
	return observer.track(candidates, now), nil
}

func (observer *Observer) track(candidates []candidate, now time.Time) []Activity {
	observer.mu.Lock()
	defer observer.mu.Unlock()

	activities := make([]Activity, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, item := range candidates {
		key := item.IdentityHash
		if _, duplicate := seen[key]; duplicate {
			key = stableID(key + "|parallel|" + strconv.Itoa(item.PID))
		}
		seen[key] = struct{}{}

		history := observer.history[key]
		history.Restarts = recentRestarts(history.Restarts, now.Add(-observer.restartWindow))
		if history.PID != 0 && history.PID != item.PID && now.Sub(history.LastSeen) <= observer.restartWindow {
			history.Restarts = append(history.Restarts, now)
		}
		if history.PID != item.PID || history.StartedAt.IsZero() {
			history.StartedAt = now.Add(-item.Elapsed)
		}
		history.PID = item.PID
		history.LastSeen = now
		observer.history[key] = history

		status := StatusRunning
		quality := QualityInferred
		if len(history.Restarts) >= observer.restartThreshold {
			status = StatusRestarting
			quality = QualityUncertain
		}
		activities = append(activities, Activity{
			ID: stableID(key), ProjectName: item.ProjectName, ProjectPath: item.ProjectPath,
			Runtime: item.Runtime, Label: item.Label, Status: status, Quality: quality,
			StartedAt: history.StartedAt, RestartCount: len(history.Restarts),
		})
	}

	for key, history := range observer.history {
		if now.Sub(history.LastSeen) > observer.restartWindow {
			delete(observer.history, key)
		}
	}
	sort.Slice(activities, func(i, j int) bool {
		if activities[i].ProjectName != activities[j].ProjectName {
			return activities[i].ProjectName < activities[j].ProjectName
		}
		return activities[i].Label < activities[j].Label
	})
	return activities
}

func parsePSOutput(output, projectRoot string, minimumAge time.Duration) []candidate {
	var candidates []candidate
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		elapsed, err := parseElapsed(fields[2])
		if err != nil || elapsed < minimumAge {
			continue
		}
		command := strings.Join(fields[4:], " ")
		executable := filepath.Base(strings.Trim(fields[4], `"'`))
		if strings.EqualFold(executable, "agent-task-monitor") {
			continue
		}
		projectName, projectPath, label, ok := commandProject(command, projectRoot)
		if !ok || projectName == "AgentTaskMonitor" && strings.Contains(command, "agent-task-monitor") {
			continue
		}
		candidates = append(candidates, candidate{
			PID: pid, Elapsed: elapsed, ProjectName: projectName, ProjectPath: projectPath,
			Runtime: runtimeName(executable), Label: label,
			IdentityHash: hashText(projectPath + "\x00" + command),
		})
	}
	return candidates
}

func commandProject(command, projectRoot string) (string, string, string, bool) {
	var firstPath, preferredLabel string
	prefix := projectRoot + string(filepath.Separator)
	for _, raw := range strings.Fields(command) {
		index := strings.Index(raw, prefix)
		if index < 0 {
			continue
		}
		path := strings.Trim(raw[index:], `"'(),;`)
		if cut := strings.IndexAny(path, `"',;`); cut >= 0 {
			path = path[:cut]
		}
		if firstPath == "" {
			firstPath = path
		}
		extension := strings.ToLower(filepath.Ext(path))
		switch extension {
		case ".py", ".js", ".mjs", ".cjs", ".ts", ".sh", ".rb", ".jar":
			if preferredLabel == "" {
				preferredLabel = filepath.Base(path)
			}
		}
	}
	if firstPath == "" {
		return "", "", "", false
	}
	relative, err := filepath.Rel(projectRoot, firstPath)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return "", "", "", false
	}
	projectName := strings.Split(relative, string(filepath.Separator))[0]
	if projectName == "" || projectName == "." || projectName == ".." {
		return "", "", "", false
	}
	if preferredLabel == "" {
		preferredLabel = filepath.Base(firstPath)
	}
	return projectName, filepath.Join(projectRoot, projectName), preferredLabel, true
}

func parseElapsed(value string) (time.Duration, error) {
	parts := strings.Split(value, "-")
	var days int
	clock := value
	if len(parts) == 2 {
		parsed, err := strconv.Atoi(parts[0])
		if err != nil {
			return 0, err
		}
		days = parsed
		clock = parts[1]
	} else if len(parts) > 2 {
		return 0, fmt.Errorf("invalid elapsed time")
	}
	clockParts := strings.Split(clock, ":")
	if len(clockParts) < 2 || len(clockParts) > 3 {
		return 0, fmt.Errorf("invalid elapsed time")
	}
	values := make([]int, len(clockParts))
	for index, part := range clockParts {
		parsed, err := strconv.Atoi(part)
		if err != nil {
			return 0, err
		}
		values[index] = parsed
	}
	var hours, minutes, seconds int
	if len(values) == 3 {
		hours, minutes, seconds = values[0], values[1], values[2]
	} else {
		minutes, seconds = values[0], values[1]
	}
	return time.Duration(days)*24*time.Hour + time.Duration(hours)*time.Hour +
		time.Duration(minutes)*time.Minute + time.Duration(seconds)*time.Second, nil
}

func recentRestarts(values []time.Time, cutoff time.Time) []time.Time {
	kept := values[:0]
	for _, value := range values {
		if !value.Before(cutoff) {
			kept = append(kept, value)
		}
	}
	return kept
}

func runtimeName(executable string) string {
	lower := strings.ToLower(executable)
	switch {
	case strings.HasPrefix(lower, "python"):
		return "Python"
	case lower == "node" || lower == "nodejs":
		return "Node.js"
	case lower == "docker" || lower == "com.docker.backend":
		return "Docker"
	case lower == "java":
		return "Java"
	case lower == "ruby":
		return "Ruby"
	case lower == "bash" || lower == "zsh" || lower == "sh":
		return "Shell"
	default:
		if executable == "" {
			return "Process"
		}
		return executable
	}
}

func runPS(ctx context.Context) (string, error) {
	output, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,etime=,state=,command=").Output()
	return string(output), err
}

func stableID(value string) string {
	return "process_" + hashText(value)[:16]
}

func hashText(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}
