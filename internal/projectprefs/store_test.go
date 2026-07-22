package projectprefs

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLoadMissingFileReturnsEmptyPreferences(t *testing.T) {
	preferences, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(preferences) != 0 {
		t.Fatalf("preferences = %#v, want empty", preferences)
	}
}

func TestStoreSetsPriorityWithStableRankAndAtomicBackup(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project-preferences.json")
	now := time.Date(2026, 7, 21, 8, 30, 0, 0, time.UTC)
	store := NewStore(Options{Path: path, Now: func() time.Time { return now }})

	alpha, err := store.SetPriority("Alpha", PriorityP0)
	if err != nil {
		t.Fatalf("SetPriority(Alpha) error = %v", err)
	}
	if alpha.Priority != PriorityP0 || alpha.Rank != 1000 {
		t.Fatalf("Alpha preference = %#v", alpha)
	}
	unchanged, err := store.SetPriority("Alpha", PriorityP0)
	if err != nil {
		t.Fatalf("SetPriority(Alpha unchanged) error = %v", err)
	}
	if unchanged.Rank != alpha.Rank {
		t.Fatalf("unchanged rank = %d, want %d", unchanged.Rank, alpha.Rank)
	}
	beta, err := store.SetPriority("Beta", PriorityP0)
	if err != nil {
		t.Fatalf("SetPriority(Beta) error = %v", err)
	}
	if beta.Rank != 2000 {
		t.Fatalf("Beta rank = %d, want 2000", beta.Rank)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat preferences: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("preferences mode = %o, want 600", info.Mode().Perm())
	}
	if _, err := os.Stat(path + ".bak"); err != nil {
		t.Fatalf("stat backup: %v", err)
	}
}

func TestStoreRemovesPreferenceWhenPriorityIsUnset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project-preferences.json")
	store := NewStore(Options{Path: path})
	if _, err := store.SetPriority("Alpha", PriorityP1); err != nil {
		t.Fatalf("set preference: %v", err)
	}
	preference, err := store.SetPriority("Alpha", PriorityUnset)
	if err != nil {
		t.Fatalf("unset preference: %v", err)
	}
	if preference.Priority != PriorityUnset || preference.Rank != 0 {
		t.Fatalf("unset preference = %#v", preference)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if _, exists := loaded["Alpha"]; exists {
		t.Fatalf("Alpha remained in %#v", loaded)
	}
}

func TestStoreReordersOnlyProjectsInTheSamePriority(t *testing.T) {
	store := NewStore(Options{Path: filepath.Join(t.TempDir(), "preferences.json")})
	for _, key := range []string{"Alpha", "Beta", "Gamma"} {
		if _, err := store.SetPriority(key, PriorityP1); err != nil {
			t.Fatalf("set %s: %v", key, err)
		}
	}
	if err := store.Reorder(PriorityP1, []string{"Gamma", "Alpha", "Beta"}); err != nil {
		t.Fatalf("Reorder() error = %v", err)
	}
	loaded, err := store.Load()
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if loaded["Gamma"].Rank != 1000 || loaded["Alpha"].Rank != 2000 || loaded["Beta"].Rank != 3000 {
		t.Fatalf("ranks = %#v", loaded)
	}
	if err := store.Reorder(PriorityP1, []string{"Alpha", "Alpha", "Beta"}); !errors.Is(err, ErrInvalidOrder) {
		t.Fatalf("duplicate Reorder() error = %v, want ErrInvalidOrder", err)
	}
}

func TestStoreRejectsUnsafeProjectKeysAndInvalidPriorities(t *testing.T) {
	store := NewStore(Options{Path: filepath.Join(t.TempDir(), "preferences.json")})
	if _, err := store.SetPriority("../Outside", PriorityP0); !errors.Is(err, ErrInvalidProjectKey) {
		t.Fatalf("unsafe key error = %v", err)
	}
	if _, err := store.SetPriority("Alpha", Priority("urgent")); !errors.Is(err, ErrInvalidPriority) {
		t.Fatalf("invalid priority error = %v", err)
	}
}
