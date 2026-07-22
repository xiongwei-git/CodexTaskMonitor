package projectmeta

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const supportedRegistryVersion = 3

type Summary struct {
	Text      string
	Source    string
	Quality   string
	UpdatedAt string
}

type registry struct {
	Version  int             `json:"version"`
	Projects []projectRecord `json:"projects"`
}

type projectRecord struct {
	Path             string `json:"path"`
	Summary          string `json:"summary"`
	SummarySource    string `json:"summary_source"`
	SummaryQuality   string `json:"summary_quality"`
	SummaryUpdatedAt string `json:"summary_updated_at"`
}

func Load(path string) (map[string]Summary, error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read project registry: %w", err)
	}
	var data registry
	if err := json.Unmarshal(contents, &data); err != nil {
		return nil, fmt.Errorf("decode project registry: %w", err)
	}
	if data.Version != supportedRegistryVersion {
		return nil, fmt.Errorf("unsupported project registry version: %d", data.Version)
	}
	summaries := make(map[string]Summary, len(data.Projects))
	for _, project := range data.Projects {
		path := strings.TrimSpace(project.Path)
		text := strings.TrimSpace(project.Summary)
		if path == "" || text == "" {
			continue
		}
		summaries[filepath.Clean(path)] = Summary{
			Text: text, Source: strings.TrimSpace(project.SummarySource),
			Quality:   strings.TrimSpace(project.SummaryQuality),
			UpdatedAt: strings.TrimSpace(project.SummaryUpdatedAt),
		}
	}
	return summaries, nil
}
