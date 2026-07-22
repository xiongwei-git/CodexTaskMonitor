package monitor

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/xiongwei-git/agent-task-monitor/internal/probe"
)

var errSnapshotUnavailable = errors.New("snapshot unavailable")

type Runner func(context.Context) (probe.Report, error)

type Store struct {
	runner Runner

	mu          sync.RWMutex
	report      probe.Report
	hasSnapshot bool
	fingerprint [sha256.Size]byte
	subscribers map[chan struct{}]struct{}
}

func NewStore(runner Runner) *Store {
	return &Store{runner: runner, subscribers: make(map[chan struct{}]struct{})}
}

func (store *Store) Snapshot() (probe.Report, error) {
	store.mu.RLock()
	defer store.mu.RUnlock()
	if !store.hasSnapshot {
		return probe.Report{}, errSnapshotUnavailable
	}
	return store.report, nil
}

func (store *Store) Refresh(ctx context.Context) (bool, error) {
	report, err := store.runner(ctx)
	if err != nil {
		return false, err
	}
	fingerprint, err := snapshotFingerprint(report)
	if err != nil {
		return false, err
	}

	store.mu.Lock()
	changed := !store.hasSnapshot || store.fingerprint != fingerprint
	store.report = report
	store.hasSnapshot = true
	store.fingerprint = fingerprint
	if changed {
		for subscriber := range store.subscribers {
			select {
			case subscriber <- struct{}{}:
			default:
			}
		}
	}
	store.mu.Unlock()
	return changed, nil
}

func (store *Store) Subscribe() (<-chan struct{}, func()) {
	updates := make(chan struct{}, 1)
	store.mu.Lock()
	store.subscribers[updates] = struct{}{}
	store.mu.Unlock()

	var once sync.Once
	cancel := func() {
		once.Do(func() {
			store.mu.Lock()
			delete(store.subscribers, updates)
			close(updates)
			store.mu.Unlock()
		})
	}
	return updates, cancel
}

func (store *Store) Start(ctx context.Context, interval time.Duration, onError func(error)) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if _, err := store.Refresh(ctx); err != nil && onError != nil {
				onError(err)
			}
		}
	}
}

func snapshotFingerprint(report probe.Report) ([sha256.Size]byte, error) {
	report.GeneratedAt = time.Time{}
	encoded, err := json.Marshal(report)
	if err != nil {
		return [sha256.Size]byte{}, err
	}
	return sha256.Sum256(encoded), nil
}
