package projectmeta

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadReturnsSummaryByCanonicalProjectPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project-registry.json")
	content := `{
  "version": 3,
  "projects": [{
    "name": "Alpha",
    "path": "/Users/example/CodeX/Alpha",
    "summary": "汇总本地任务状态的实时看板。",
    "summary_source": "manual_override",
    "summary_quality": "confirmed",
    "summary_updated_at": "2026-07-21"
  }]
}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}

	summaries, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	wantPath := filepath.Clean("/Users/example/CodeX/Alpha")
	got := summaries[wantPath]
	if got.Text != "汇总本地任务状态的实时看板。" || got.Source != "manual_override" || got.Quality != "confirmed" {
		t.Fatalf("summary = %#v", got)
	}
	if got.UpdatedAt != "2026-07-21" {
		t.Fatalf("updated at = %q", got.UpdatedAt)
	}
}

func TestLoadRejectsUnsupportedRegistryVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project-registry.json")
	if err := os.WriteFile(path, []byte(`{"version":2,"projects":[]}`), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("Load() error = nil, want unsupported version error")
	}
}

func TestLoadSkipsIncompleteRecords(t *testing.T) {
	path := filepath.Join(t.TempDir(), "project-registry.json")
	if err := os.WriteFile(path, []byte(`{"version":3,"projects":[{"path":"/tmp/Alpha"}]}`), 0o600); err != nil {
		t.Fatalf("write registry: %v", err)
	}
	summaries, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if len(summaries) != 0 {
		t.Fatalf("summaries = %#v, want empty", summaries)
	}
}
