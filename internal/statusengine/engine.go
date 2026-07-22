package statusengine

import "time"

type Status string

const (
	StatusWorking           Status = "working"
	StatusWaiting           Status = "waiting"
	StatusCompleted         Status = "completed"
	StatusInterrupted       Status = "interrupted"
	StatusFailed            Status = "failed"
	StatusSuspectedAbnormal Status = "suspected_abnormal"
	StatusIdle              Status = "idle"
	StatusUnknown           Status = "unknown"
)

type Quality string

const (
	QualityExact     Quality = "exact"
	QualityInferred  Quality = "inferred"
	QualityUncertain Quality = "uncertain"
)

type Reason string

const (
	ReasonRecentSessionActivity     Reason = "recent_session_activity"
	ReasonTaskComplete              Reason = "task_complete"
	ReasonTurnAborted               Reason = "turn_aborted"
	ReasonStaleWithoutTerminalEvent Reason = "stale_without_terminal_event"
	ReasonNoLifecycleEvidence       Reason = "no_lifecycle_evidence"
)

type Lifecycle struct {
	StartedAt      *time.Time
	CompletedAt    *time.Time
	InterruptedAt  *time.Time
	LastActivityAt *time.Time
}

type Assessment struct {
	Status  Status
	Quality Quality
	Reason  Reason
}

func Evaluate(lifecycle Lifecycle, now time.Time, staleAfter time.Duration) Assessment {
	if lifecycle.StartedAt == nil {
		return Assessment{
			Status:  StatusUnknown,
			Quality: QualityUncertain,
			Reason:  ReasonNoLifecycleEvidence,
		}
	}

	if terminal, ok := latestTerminalAfterStart(lifecycle); ok {
		return terminal
	}

	lastActivity := lifecycle.StartedAt
	if lifecycle.LastActivityAt != nil && lifecycle.LastActivityAt.After(*lastActivity) {
		lastActivity = lifecycle.LastActivityAt
	}

	if staleAfter > 0 && now.Sub(*lastActivity) >= staleAfter {
		return Assessment{
			Status:  StatusSuspectedAbnormal,
			Quality: QualityUncertain,
			Reason:  ReasonStaleWithoutTerminalEvent,
		}
	}

	return Assessment{
		Status:  StatusWorking,
		Quality: QualityInferred,
		Reason:  ReasonRecentSessionActivity,
	}
}

func latestTerminalAfterStart(lifecycle Lifecycle) (Assessment, bool) {
	type terminal struct {
		at         *time.Time
		assessment Assessment
	}

	candidates := []terminal{
		{
			at: lifecycle.CompletedAt,
			assessment: Assessment{
				Status:  StatusCompleted,
				Quality: QualityExact,
				Reason:  ReasonTaskComplete,
			},
		},
		{
			at: lifecycle.InterruptedAt,
			assessment: Assessment{
				Status:  StatusInterrupted,
				Quality: QualityExact,
				Reason:  ReasonTurnAborted,
			},
		},
	}

	var latest terminal
	for _, candidate := range candidates {
		if candidate.at == nil || candidate.at.Before(*lifecycle.StartedAt) {
			continue
		}
		if latest.at == nil || candidate.at.After(*latest.at) {
			latest = candidate
		}
	}

	if latest.at == nil {
		return Assessment{}, false
	}
	return latest.assessment, true
}
