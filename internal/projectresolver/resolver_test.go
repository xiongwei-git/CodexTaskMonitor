package projectresolver

import (
	"os"
	"path/filepath"
	"testing"
)

func TestResolve(t *testing.T) {
	root := "/Users/example/CodeX"

	tests := []struct {
		name     string
		cwd      string
		wantPath string
		wantName string
		wantOK   bool
	}{
		{
			name:     "direct project",
			cwd:      "/Users/example/CodeX/AgentTaskMonitor",
			wantPath: "/Users/example/CodeX/AgentTaskMonitor",
			wantName: "AgentTaskMonitor",
			wantOK:   true,
		},
		{
			name:     "nested working directory",
			cwd:      "/Users/example/CodeX/AgentTaskMonitor/internal/statusengine",
			wantPath: "/Users/example/CodeX/AgentTaskMonitor",
			wantName: "AgentTaskMonitor",
			wantOK:   true,
		},
		{
			name:   "root itself is not a project",
			cwd:    "/Users/example/CodeX",
			wantOK: false,
		},
		{
			name:   "outside root",
			cwd:    "/Users/example/Documents/Codex/temp",
			wantOK: false,
		},
		{
			name:   "prefix collision is outside root",
			cwd:    "/Users/example/CodeX-old/Project",
			wantOK: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := Resolve(root, tt.cwd)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if got.Path != tt.wantPath {
				t.Fatalf("Path = %q, want %q", got.Path, tt.wantPath)
			}
			if got.Name != tt.wantName {
				t.Fatalf("Name = %q, want %q", got.Name, tt.wantName)
			}
		})
	}
}

func TestResolveWorkspaceMapsChatGPTProjectMirrorByDeclaredName(t *testing.T) {
	tempDir := t.TempDir()
	projectRoot := filepath.Join(tempDir, "CodeX")
	canonicalPath := filepath.Join(projectRoot, "AgentTaskMonitor")
	mirrorPath := filepath.Join(tempDir, ".codex", ".chatgpt-projects", "g-p-example")

	if err := os.MkdirAll(canonicalPath, 0o755); err != nil {
		t.Fatalf("create canonical project: %v", err)
	}
	if err := os.MkdirAll(mirrorPath, 0o755); err != nil {
		t.Fatalf("create mirror: %v", err)
	}
	agents := "# ChatGPT project context\n\nThis directory is a local mirror of the ChatGPT project “AgentTaskMonitor”.\n"
	if err := os.WriteFile(filepath.Join(mirrorPath, "AGENTS.md"), []byte(agents), 0o600); err != nil {
		t.Fatalf("write mirror metadata: %v", err)
	}

	got, ok := ResolveWorkspace(projectRoot, mirrorPath)
	if !ok {
		t.Fatal("ResolveWorkspace() ok = false, want true")
	}
	if got.Name != "AgentTaskMonitor" || got.Path != canonicalPath {
		t.Fatalf("project = %#v, want canonical AgentTaskMonitor", got)
	}
}

func TestResolveWorkspaceDoesNotGuessWhenDeclaredProjectIsMissing(t *testing.T) {
	tempDir := t.TempDir()
	projectRoot := filepath.Join(tempDir, "CodeX")
	mirrorPath := filepath.Join(tempDir, ".codex", ".chatgpt-projects", "g-p-example")
	if err := os.MkdirAll(mirrorPath, 0o755); err != nil {
		t.Fatalf("create mirror: %v", err)
	}
	if err := os.WriteFile(
		filepath.Join(mirrorPath, "AGENTS.md"),
		[]byte("This directory is a local mirror of the ChatGPT project “Missing Project”.\n"),
		0o600,
	); err != nil {
		t.Fatalf("write mirror metadata: %v", err)
	}

	if got, ok := ResolveWorkspace(projectRoot, mirrorPath); ok {
		t.Fatalf("ResolveWorkspace() = %#v, true; want no speculative match", got)
	}
}
