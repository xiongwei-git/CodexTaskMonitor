package projectprefs

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

const (
	fileVersion = 1
	rankStep    = 1000
)

var (
	ErrInvalidProjectKey = errors.New("invalid project key")
	ErrInvalidPriority   = errors.New("invalid priority")
	ErrInvalidOrder      = errors.New("invalid project order")
)

type Priority string

const (
	PriorityUnset Priority = "unset"
	PriorityP0    Priority = "p0"
	PriorityP1    Priority = "p1"
	PriorityP2    Priority = "p2"
	PriorityP3    Priority = "p3"
)

type Preference struct {
	Priority  Priority  `json:"priority"`
	Rank      int       `json:"rank"`
	UpdatedAt time.Time `json:"updated_at"`
}

type Options struct {
	Path string
	Now  func() time.Time
}

type Store struct {
	path string
	now  func() time.Time
	mu   sync.Mutex
}

type fileData struct {
	Version  int                   `json:"version"`
	Projects map[string]Preference `json:"projects"`
}

func NewStore(options Options) *Store {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	return &Store{path: options.Path, now: now}
}

func Load(path string) (map[string]Preference, error) {
	contents, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return map[string]Preference{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read project preferences: %w", err)
	}
	var data fileData
	if err := json.Unmarshal(contents, &data); err != nil {
		return nil, fmt.Errorf("decode project preferences: %w", err)
	}
	if data.Version != fileVersion {
		return nil, fmt.Errorf("unsupported project preferences version: %d", data.Version)
	}
	if data.Projects == nil {
		data.Projects = map[string]Preference{}
	}
	for key, preference := range data.Projects {
		if err := validateProjectKey(key); err != nil {
			return nil, err
		}
		if !preference.Priority.validAssigned() || preference.Rank <= 0 {
			return nil, fmt.Errorf("%w: project %q", ErrInvalidPriority, key)
		}
	}
	return data.Projects, nil
}

func (store *Store) Load() (map[string]Preference, error) {
	store.mu.Lock()
	defer store.mu.Unlock()
	return Load(store.path)
}

func (store *Store) SetPriority(projectKey string, priority Priority) (Preference, error) {
	if err := validateProjectKey(projectKey); err != nil {
		return Preference{}, err
	}
	if !priority.valid() {
		return Preference{}, ErrInvalidPriority
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	projects, err := Load(store.path)
	if err != nil {
		return Preference{}, err
	}
	if priority == PriorityUnset {
		delete(projects, projectKey)
		if err := store.write(projects); err != nil {
			return Preference{}, err
		}
		return Preference{Priority: PriorityUnset}, nil
	}

	current, exists := projects[projectKey]
	if exists && current.Priority == priority {
		return current, nil
	}
	rank := rankStep
	for _, preference := range projects {
		if preference.Priority == priority && preference.Rank >= rank {
			rank = preference.Rank + rankStep
		}
	}
	preference := Preference{Priority: priority, Rank: rank, UpdatedAt: store.now().UTC()}
	projects[projectKey] = preference
	if err := store.write(projects); err != nil {
		return Preference{}, err
	}
	return preference, nil
}

func (store *Store) Reorder(priority Priority, projectKeys []string) error {
	if !priority.validAssigned() || len(projectKeys) == 0 {
		return ErrInvalidOrder
	}
	seen := make(map[string]struct{}, len(projectKeys))
	for _, key := range projectKeys {
		if err := validateProjectKey(key); err != nil {
			return err
		}
		if _, duplicate := seen[key]; duplicate {
			return ErrInvalidOrder
		}
		seen[key] = struct{}{}
	}

	store.mu.Lock()
	defer store.mu.Unlock()
	projects, err := Load(store.path)
	if err != nil {
		return err
	}
	for _, key := range projectKeys {
		preference, exists := projects[key]
		if !exists || preference.Priority != priority {
			return ErrInvalidOrder
		}
	}
	omitted := make([]string, 0)
	for key, preference := range projects {
		if preference.Priority == priority {
			if _, included := seen[key]; !included {
				omitted = append(omitted, key)
			}
		}
	}
	sort.Slice(omitted, func(i, j int) bool { return projects[omitted[i]].Rank < projects[omitted[j]].Rank })
	projectKeys = append(projectKeys, omitted...)
	now := store.now().UTC()
	for index, key := range projectKeys {
		preference, exists := projects[key]
		if !exists || preference.Priority != priority {
			return ErrInvalidOrder
		}
		preference.Rank = (index + 1) * rankStep
		preference.UpdatedAt = now
		projects[key] = preference
	}
	return store.write(projects)
}

func (store *Store) write(projects map[string]Preference) error {
	data := fileData{Version: fileVersion, Projects: projects}
	encoded, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode project preferences: %w", err)
	}
	encoded = append(encoded, '\n')
	if previous, readErr := os.ReadFile(store.path); readErr == nil {
		if err := atomicWrite(store.path+".bak", previous); err != nil {
			return fmt.Errorf("backup project preferences: %w", err)
		}
	} else if !errors.Is(readErr, fs.ErrNotExist) {
		return fmt.Errorf("read existing project preferences: %w", readErr)
	}
	if err := atomicWrite(store.path, encoded); err != nil {
		return fmt.Errorf("write project preferences: %w", err)
	}
	return nil
}

func atomicWrite(path string, contents []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".project-preferences-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(contents); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	return os.Rename(temporaryPath, path)
}

func validateProjectKey(value string) error {
	if value == "" || value == "." || value == ".." || strings.TrimSpace(value) != value ||
		strings.ContainsAny(value, `/\\`) || filepath.Base(value) != value {
		return ErrInvalidProjectKey
	}
	return nil
}

func (priority Priority) valid() bool {
	return priority == PriorityUnset || priority.validAssigned()
}

func (priority Priority) validAssigned() bool {
	switch priority {
	case PriorityP0, PriorityP1, PriorityP2, PriorityP3:
		return true
	default:
		return false
	}
}
