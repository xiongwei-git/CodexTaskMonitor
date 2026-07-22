package sessionevents

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/xiongwei-git/agent-task-monitor/internal/statusengine"
)

const maxJSONLLineBytes = 8 * 1024 * 1024

type Summary struct {
	Lifecycle   statusengine.Lifecycle
	ValidEvents int
	ParseErrors int
}

type eventEnvelope struct {
	Timestamp string `json:"timestamp"`
	Type      string `json:"type"`
	Payload   struct {
		Type string `json:"type"`
	} `json:"payload"`
}

func ScanFile(path string) (Summary, error) {
	file, err := os.Open(path)
	if err != nil {
		return Summary{}, fmt.Errorf("open session event file: %w", err)
	}
	defer file.Close()

	return Scan(file)
}

func Scan(reader io.Reader) (Summary, error) {
	var summary Summary
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), maxJSONLLineBytes)

	for scanner.Scan() {
		var event eventEnvelope
		if err := json.Unmarshal(scanner.Bytes(), &event); err != nil {
			summary.ParseErrors++
			continue
		}

		timestamp, err := time.Parse(time.RFC3339Nano, event.Timestamp)
		if err != nil {
			summary.ParseErrors++
			continue
		}

		summary.ValidEvents++
		if summary.Lifecycle.LastActivityAt == nil || timestamp.After(*summary.Lifecycle.LastActivityAt) {
			summary.Lifecycle.LastActivityAt = cloneTime(timestamp)
		}

		if event.Type != "event_msg" {
			continue
		}

		switch event.Payload.Type {
		case "task_started":
			summary.Lifecycle.StartedAt = cloneTime(timestamp)
			summary.Lifecycle.CompletedAt = nil
			summary.Lifecycle.InterruptedAt = nil
		case "task_complete":
			if summary.Lifecycle.StartedAt != nil {
				summary.Lifecycle.CompletedAt = cloneTime(timestamp)
			}
		case "turn_aborted":
			if summary.Lifecycle.StartedAt != nil {
				summary.Lifecycle.InterruptedAt = cloneTime(timestamp)
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return summary, fmt.Errorf("scan session event file: %w", err)
	}
	return summary, nil
}

func cloneTime(value time.Time) *time.Time {
	cloned := value
	return &cloned
}
