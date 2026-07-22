package processprobe

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

type Snapshot struct {
	CodexRunning      bool `json:"codexRunning"`
	CodexProcessCount int  `json:"codexProcessCount"`
	ChatGPTAppRunning bool `json:"chatgptAppRunning"`
}

func Detect(ctx context.Context) (Snapshot, error) {
	output, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,comm=").Output()
	if err != nil {
		return Snapshot{}, fmt.Errorf("inspect local processes: %w", err)
	}
	return parsePSOutput(string(output)), nil
}

func parsePSOutput(output string) Snapshot {
	var snapshot Snapshot
	for _, line := range strings.Split(output, "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		command := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), fields[0]))
		switch filepath.Base(command) {
		case "codex":
			snapshot.CodexProcessCount++
		case "ChatGPT":
			snapshot.ChatGPTAppRunning = true
		}
	}
	snapshot.CodexRunning = snapshot.CodexProcessCount > 0
	return snapshot
}
