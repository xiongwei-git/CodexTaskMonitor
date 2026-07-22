package projectresolver

import (
	"io"
	"os"
	"path/filepath"
	"strings"
)

type Project struct {
	Name string
	Path string
}

func Resolve(projectRoot, cwd string) (Project, bool) {
	cleanRoot := filepath.Clean(projectRoot)
	cleanCWD := filepath.Clean(cwd)

	relative, err := filepath.Rel(cleanRoot, cleanCWD)
	if err != nil || relative == "." || relative == ".." {
		return Project{}, false
	}
	if strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return Project{}, false
	}

	projectName := strings.Split(relative, string(filepath.Separator))[0]
	if projectName == "" || projectName == "." || projectName == ".." {
		return Project{}, false
	}

	return Project{
		Name: projectName,
		Path: filepath.Join(cleanRoot, projectName),
	}, true
}

// ResolveWorkspace resolves both canonical CodeX paths and strongly identified
// workspace aliases. It intentionally refuses fuzzy name matching.
func ResolveWorkspace(projectRoot, cwd string) (Project, bool) {
	if project, ok := Resolve(projectRoot, cwd); ok {
		return project, true
	}

	cleanCWD := filepath.Clean(cwd)
	if filepath.Base(filepath.Dir(cleanCWD)) != ".chatgpt-projects" {
		return Project{}, false
	}

	projectName, ok := declaredChatGPTProjectName(filepath.Join(cleanCWD, "AGENTS.md"))
	if !ok || filepath.Base(projectName) != projectName || projectName == "." || projectName == ".." {
		return Project{}, false
	}

	canonicalPath := filepath.Join(filepath.Clean(projectRoot), projectName)
	info, err := os.Stat(canonicalPath)
	if err != nil || !info.IsDir() {
		return Project{}, false
	}

	return Project{Name: projectName, Path: canonicalPath}, true
}

func declaredChatGPTProjectName(path string) (string, bool) {
	file, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer file.Close()

	content, err := io.ReadAll(io.LimitReader(file, 64<<10))
	if err != nil {
		return "", false
	}
	text := string(content)
	const prefix = "local mirror of the ChatGPT project “"
	start := strings.Index(text, prefix)
	if start < 0 {
		return "", false
	}
	remaining := text[start+len(prefix):]
	end := strings.Index(remaining, "”")
	if end <= 0 {
		return "", false
	}
	name := strings.TrimSpace(remaining[:end])
	return name, name != ""
}
