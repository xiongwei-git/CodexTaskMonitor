package processprobe

import "testing"

func TestParsePSOutputCountsCodexProcesses(t *testing.T) {
	input := `
  101 /Applications/ChatGPT.app/Contents/MacOS/ChatGPT
  102 /Applications/ChatGPT.app/Contents/Resources/codex
  103 /Applications/ChatGPT.app/Contents/Resources/codex
  104 /usr/bin/sed
`

	got := parsePSOutput(input)
	if got.CodexProcessCount != 2 {
		t.Fatalf("CodexProcessCount = %d, want 2", got.CodexProcessCount)
	}
	if !got.CodexRunning {
		t.Fatal("CodexRunning = false, want true")
	}
	if !got.ChatGPTAppRunning {
		t.Fatal("ChatGPTAppRunning = false, want true")
	}
}

func TestParsePSOutputReturnsFalseForUnrelatedProcesses(t *testing.T) {
	got := parsePSOutput("101 /usr/bin/sed\n")
	if got.CodexRunning || got.ChatGPTAppRunning || got.CodexProcessCount != 0 {
		t.Fatalf("snapshot = %#v, want no Codex or ChatGPT process", got)
	}
}
