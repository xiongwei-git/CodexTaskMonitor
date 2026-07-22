package monitor

import (
	"context"
	"testing"
	"time"

	"github.com/xiongwei-git/agent-task-monitor/internal/probe"
)

func TestStorePublishesOnlyWhenSnapshotContentChanges(t *testing.T) {
	report := probe.Report{Summary: probe.Summary{ProjectCount: 1}}
	store := NewStore(func(context.Context) (probe.Report, error) {
		report.GeneratedAt = time.Now().UTC()
		return report, nil
	})

	if _, err := store.Refresh(context.Background()); err != nil {
		t.Fatalf("initial Refresh() error = %v", err)
	}
	updates, cancel := store.Subscribe()
	defer cancel()

	changed, err := store.Refresh(context.Background())
	if err != nil {
		t.Fatalf("unchanged Refresh() error = %v", err)
	}
	if changed {
		t.Fatal("unchanged Refresh() changed = true, want false")
	}

	report.Summary.ProjectCount = 2
	changed, err = store.Refresh(context.Background())
	if err != nil {
		t.Fatalf("changed Refresh() error = %v", err)
	}
	if !changed {
		t.Fatal("changed Refresh() changed = false, want true")
	}

	select {
	case <-updates:
	case <-time.After(time.Second):
		t.Fatal("subscriber did not receive snapshot update")
	}
}

func TestStoreReturnsInitialisationErrorUntilSnapshotExists(t *testing.T) {
	store := NewStore(func(context.Context) (probe.Report, error) {
		return probe.Report{}, context.DeadlineExceeded
	})

	if _, err := store.Refresh(context.Background()); err == nil {
		t.Fatal("Refresh() error = nil, want runner error")
	}
	if _, err := store.Snapshot(); err == nil {
		t.Fatal("Snapshot() error = nil, want unavailable error")
	}
}
