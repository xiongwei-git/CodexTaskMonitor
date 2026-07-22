package cli

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunHelp(t *testing.T) {
	var stdout, stderr bytes.Buffer

	exitCode := Run([]string{"help"}, &stdout, &stderr)
	if exitCode != 0 {
		t.Fatalf("exit code = %d, want 0; stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stdout.String(), "agent-task-monitor probe") {
		t.Fatalf("stdout = %q, want probe usage", stdout.String())
	}
	if !strings.Contains(stdout.String(), "agent-task-monitor serve") {
		t.Fatalf("stdout = %q, want serve usage", stdout.String())
	}
}

func TestRunServeRejectsNonLoopbackAddress(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exitCode := Run([]string{"serve", "--addr", "0.0.0.0:4747"}, &stdout, &stderr)
	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2; stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "loopback") {
		t.Fatalf("stderr = %q, want loopback validation", stderr.String())
	}
}

func TestRunRejectsNonPositiveStaleDurationBeforeReadingFiles(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tempDir := t.TempDir()

	exitCode := Run([]string{
		"probe",
		"--codex-home", filepath.Join(tempDir, "missing-codex-home"),
		"--project-root", filepath.Join(tempDir, "CodeX"),
		"--stale-after", "0s",
	}, &stdout, &stderr)

	if exitCode != 2 {
		t.Fatalf("exit code = %d, want 2; stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "stale-after must be positive") {
		t.Fatalf("stderr = %q, want validation message", stderr.String())
	}
}

func TestRunReportsMissingStateDatabaseWithoutStackTrace(t *testing.T) {
	var stdout, stderr bytes.Buffer
	tempDir := t.TempDir()

	exitCode := Run([]string{
		"probe",
		"--codex-home", filepath.Join(tempDir, "missing-codex-home"),
		"--project-root", filepath.Join(tempDir, "CodeX"),
	}, &stdout, &stderr)

	if exitCode != 1 {
		t.Fatalf("exit code = %d, want 1; stderr = %q", exitCode, stderr.String())
	}
	if !strings.Contains(stderr.String(), "probe failed") {
		t.Fatalf("stderr = %q, want safe probe error", stderr.String())
	}
	if strings.Contains(stderr.String(), "goroutine") {
		t.Fatalf("stderr leaked stack trace: %q", stderr.String())
	}
}
