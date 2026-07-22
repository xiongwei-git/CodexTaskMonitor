package projectprocess

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestObserverMapsLongRunningProcessWithoutExposingCommandArguments(t *testing.T) {
	now := time.Date(2026, 7, 21, 5, 0, 0, 0, time.UTC)
	output := `25201 1 03:12 S /opt/homebrew/bin/python3 -u /Users/example/CodeX/FeishuKnowNexus/migration-jobs/resume_tree_by_node.py --token super-secret --report /Users/example/CodeX/FeishuKnowNexus/reports/a.json`
	observer := NewObserver(Options{
		Now: func() time.Time { return now },
		RunPS: func(context.Context) (string, error) {
			return output, nil
		},
		MinimumAge: 10 * time.Second,
	})

	activities, err := observer.Detect(context.Background(), "/Users/example/CodeX")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(activities) != 1 {
		t.Fatalf("activities = %#v, want one project process", activities)
	}
	activity := activities[0]
	if activity.ProjectName != "FeishuKnowNexus" || activity.Runtime != "Python" || activity.Label != "resume_tree_by_node.py" {
		t.Fatalf("activity = %#v", activity)
	}
	if activity.Status != StatusRunning || activity.Quality != QualityInferred {
		t.Fatalf("status = %q/%q, want running/inferred", activity.Status, activity.Quality)
	}
	encoded, err := json.Marshal(activity)
	if err != nil {
		t.Fatalf("marshal activity: %v", err)
	}
	if strings.Contains(string(encoded), "super-secret") || strings.Contains(string(encoded), "--token") {
		t.Fatalf("public activity leaked raw command arguments: %s", encoded)
	}
}

func TestObserverMarksRepeatedProcessReplacementAsRestarting(t *testing.T) {
	now := time.Date(2026, 7, 21, 5, 0, 0, 0, time.UTC)
	pid := 100
	observer := NewObserver(Options{
		Now: func() time.Time { return now },
		RunPS: func(context.Context) (string, error) {
			return processLine(pid), nil
		},
		MinimumAge:       time.Second,
		RestartWindow:    5 * time.Minute,
		RestartThreshold: 2,
	})

	for step := 0; step < 3; step++ {
		activities, err := observer.Detect(context.Background(), "/Users/example/CodeX")
		if err != nil {
			t.Fatalf("Detect() step %d error = %v", step, err)
		}
		if len(activities) != 1 {
			t.Fatalf("Detect() step %d activities = %#v", step, activities)
		}
		if step < 2 && activities[0].Status != StatusRunning {
			t.Fatalf("step %d status = %q, want running", step, activities[0].Status)
		}
		if step == 2 {
			if activities[0].Status != StatusRestarting || activities[0].RestartCount != 2 {
				t.Fatalf("restarted activity = %#v, want restarting after two replacements", activities[0])
			}
		}
		pid++
		now = now.Add(30 * time.Second)
	}
}

func TestObserverSkipsShortLivedAndMonitorProcesses(t *testing.T) {
	output := strings.Join([]string{
		`300 1 00:02 R /usr/bin/python3 /Users/example/CodeX/Alpha/quick.py`,
		`301 1 05:00 S /Users/example/CodeX/AgentTaskMonitor/bin/agent-task-monitor serve`,
		`302 1 05:00 S /usr/bin/python3 /Users/example/Other/outside.py`,
	}, "\n")
	observer := NewObserver(Options{
		RunPS:      func(context.Context) (string, error) { return output, nil },
		MinimumAge: 10 * time.Second,
	})

	activities, err := observer.Detect(context.Background(), "/Users/example/CodeX")
	if err != nil {
		t.Fatalf("Detect() error = %v", err)
	}
	if len(activities) != 0 {
		t.Fatalf("activities = %#v, want irrelevant processes skipped", activities)
	}
}

func TestObserverKeepsInferredStartTimeStableForTheSameProcess(t *testing.T) {
	now := time.Date(2026, 7, 21, 5, 0, 0, 400_000_000, time.UTC)
	elapsed := "01:00"
	observer := NewObserver(Options{
		Now: func() time.Time { return now },
		RunPS: func(context.Context) (string, error) {
			return strings.Replace(processLine(100), "01:00", elapsed, 1), nil
		},
		MinimumAge: time.Second,
	})

	first, err := observer.Detect(context.Background(), "/Users/example/CodeX")
	if err != nil {
		t.Fatalf("first Detect() error = %v", err)
	}
	now = now.Add(2*time.Second + 350*time.Millisecond)
	elapsed = "01:02"
	second, err := observer.Detect(context.Background(), "/Users/example/CodeX")
	if err != nil {
		t.Fatalf("second Detect() error = %v", err)
	}
	if !first[0].StartedAt.Equal(second[0].StartedAt) {
		t.Fatalf("start time changed from %s to %s for the same PID", first[0].StartedAt, second[0].StartedAt)
	}
}

func processLine(pid int) string {
	return strings.Join([]string{
		strconv.Itoa(pid), "1", "01:00", "S",
		"/usr/bin/python3", "/Users/example/CodeX/FeishuKnowNexus/migration.py", "--report", "/Users/example/CodeX/FeishuKnowNexus/report.json",
	}, " ")
}
