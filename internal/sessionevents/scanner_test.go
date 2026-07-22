package sessionevents

import (
	"strings"
	"testing"
	"time"
)

func TestScanTracksTheLatestTaskLifecycle(t *testing.T) {
	input := strings.Join([]string{
		`{"timestamp":"2026-07-21T03:00:00Z","type":"event_msg","payload":{"type":"task_started"}}`,
		`{"timestamp":"2026-07-21T03:00:05Z","type":"event_msg","payload":{"type":"agent_reasoning","text":"must not be retained"}}`,
		`{"timestamp":"2026-07-21T03:00:10Z","type":"event_msg","payload":{"type":"task_complete"}}`,
		`{"timestamp":"2026-07-21T03:05:00Z","type":"event_msg","payload":{"type":"task_started"}}`,
		`{"timestamp":"2026-07-21T03:05:02Z","type":"response_item","payload":{"type":"message","content":"must not be retained"}}`,
	}, "\n")

	got, err := Scan(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}

	wantStarted := time.Date(2026, 7, 21, 3, 5, 0, 0, time.UTC)
	wantActivity := time.Date(2026, 7, 21, 3, 5, 2, 0, time.UTC)
	if got.Lifecycle.StartedAt == nil || !got.Lifecycle.StartedAt.Equal(wantStarted) {
		t.Fatalf("StartedAt = %v, want %v", got.Lifecycle.StartedAt, wantStarted)
	}
	if got.Lifecycle.CompletedAt != nil {
		t.Fatalf("CompletedAt = %v, want nil for latest task", got.Lifecycle.CompletedAt)
	}
	if got.Lifecycle.LastActivityAt == nil || !got.Lifecycle.LastActivityAt.Equal(wantActivity) {
		t.Fatalf("LastActivityAt = %v, want %v", got.Lifecycle.LastActivityAt, wantActivity)
	}
	if got.ValidEvents != 5 {
		t.Fatalf("ValidEvents = %d, want 5", got.ValidEvents)
	}
}

func TestScanRecordsCompletionAndInterruption(t *testing.T) {
	tests := []struct {
		name            string
		terminalType    string
		wantCompleted   bool
		wantInterrupted bool
	}{
		{name: "complete", terminalType: "task_complete", wantCompleted: true},
		{name: "interrupted", terminalType: "turn_aborted", wantInterrupted: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := strings.Join([]string{
				`{"timestamp":"2026-07-21T03:00:00Z","type":"event_msg","payload":{"type":"task_started"}}`,
				`{"timestamp":"2026-07-21T03:01:00Z","type":"event_msg","payload":{"type":"` + tt.terminalType + `"}}`,
			}, "\n")

			got, err := Scan(strings.NewReader(input))
			if err != nil {
				t.Fatalf("Scan() error = %v", err)
			}
			if (got.Lifecycle.CompletedAt != nil) != tt.wantCompleted {
				t.Fatalf("CompletedAt present = %v, want %v", got.Lifecycle.CompletedAt != nil, tt.wantCompleted)
			}
			if (got.Lifecycle.InterruptedAt != nil) != tt.wantInterrupted {
				t.Fatalf("InterruptedAt present = %v, want %v", got.Lifecycle.InterruptedAt != nil, tt.wantInterrupted)
			}
		})
	}
}

func TestScanToleratesMalformedConcurrentTailWithoutRetainingContent(t *testing.T) {
	input := strings.Join([]string{
		`{"timestamp":"2026-07-21T03:00:00Z","type":"event_msg","payload":{"type":"task_started","secret":"do-not-retain"}}`,
		`{"timestamp":"2026-07-21T03:00:05Z"`,
	}, "\n")

	got, err := Scan(strings.NewReader(input))
	if err != nil {
		t.Fatalf("Scan() error = %v", err)
	}
	if got.ParseErrors != 1 {
		t.Fatalf("ParseErrors = %d, want 1", got.ParseErrors)
	}
	if got.Lifecycle.StartedAt == nil {
		fatalf := "StartedAt is nil; valid event before malformed tail must survive"
		t.Fatal(fatalf)
	}
}
