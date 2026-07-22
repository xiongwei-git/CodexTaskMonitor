package statusengine

import (
	"testing"
	"time"
)

func TestEvaluate(t *testing.T) {
	now := time.Date(2026, 7, 21, 3, 0, 0, 0, time.UTC)
	staleAfter := 10 * time.Minute

	tests := []struct {
		name        string
		lifecycle   Lifecycle
		wantStatus  Status
		wantQuality Quality
		wantReason  Reason
	}{
		{
			name: "recent activity is working",
			lifecycle: Lifecycle{
				StartedAt:      ptrTime(now.Add(-2 * time.Minute)),
				LastActivityAt: ptrTime(now.Add(-5 * time.Second)),
			},
			wantStatus:  StatusWorking,
			wantQuality: QualityInferred,
			wantReason:  ReasonRecentSessionActivity,
		},
		{
			name: "explicit completion wins",
			lifecycle: Lifecycle{
				StartedAt:      ptrTime(now.Add(-2 * time.Minute)),
				CompletedAt:    ptrTime(now.Add(-time.Minute)),
				LastActivityAt: ptrTime(now.Add(-time.Minute)),
			},
			wantStatus:  StatusCompleted,
			wantQuality: QualityExact,
			wantReason:  ReasonTaskComplete,
		},
		{
			name: "explicit abort is interrupted",
			lifecycle: Lifecycle{
				StartedAt:      ptrTime(now.Add(-2 * time.Minute)),
				InterruptedAt:  ptrTime(now.Add(-time.Minute)),
				LastActivityAt: ptrTime(now.Add(-time.Minute)),
			},
			wantStatus:  StatusInterrupted,
			wantQuality: QualityExact,
			wantReason:  ReasonTurnAborted,
		},
		{
			name: "stale unclosed task is only suspected abnormal",
			lifecycle: Lifecycle{
				StartedAt:      ptrTime(now.Add(-30 * time.Minute)),
				LastActivityAt: ptrTime(now.Add(-20 * time.Minute)),
			},
			wantStatus:  StatusSuspectedAbnormal,
			wantQuality: QualityUncertain,
			wantReason:  ReasonStaleWithoutTerminalEvent,
		},
		{
			name:        "missing lifecycle remains unknown",
			lifecycle:   Lifecycle{},
			wantStatus:  StatusUnknown,
			wantQuality: QualityUncertain,
			wantReason:  ReasonNoLifecycleEvidence,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Evaluate(tt.lifecycle, now, staleAfter)
			if got.Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q", got.Status, tt.wantStatus)
			}
			if got.Quality != tt.wantQuality {
				t.Fatalf("quality = %q, want %q", got.Quality, tt.wantQuality)
			}
			if got.Reason != tt.wantReason {
				t.Fatalf("reason = %q, want %q", got.Reason, tt.wantReason)
			}
		})
	}
}

func TestEvaluateUsesTheLatestTerminalEvent(t *testing.T) {
	now := time.Date(2026, 7, 21, 3, 0, 0, 0, time.UTC)

	got := Evaluate(Lifecycle{
		StartedAt:      ptrTime(now.Add(-3 * time.Minute)),
		CompletedAt:    ptrTime(now.Add(-2 * time.Minute)),
		InterruptedAt:  ptrTime(now.Add(-time.Minute)),
		LastActivityAt: ptrTime(now.Add(-time.Minute)),
	}, now, 10*time.Minute)

	if got.Status != StatusInterrupted {
		t.Fatalf("status = %q, want %q", got.Status, StatusInterrupted)
	}
}

func ptrTime(value time.Time) *time.Time {
	return &value
}
