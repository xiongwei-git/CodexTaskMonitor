package api

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/xiongwei-git/agent-task-monitor/internal/probe"
	"github.com/xiongwei-git/agent-task-monitor/internal/projectprefs"
	"github.com/xiongwei-git/agent-task-monitor/internal/statusengine"
)

func TestHandlerServesDashboardAndVersionedSnapshotAPI(t *testing.T) {
	source := newFakeSource(sampleReport())
	handler := NewHandler(source, testPreferenceStore(t), testAssets())

	pageRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:4747/", nil)
	pageResponse := httptest.NewRecorder()
	handler.ServeHTTP(pageResponse, pageRequest)
	if pageResponse.Code != http.StatusOK {
		t.Fatalf("dashboard status = %d, want 200", pageResponse.Code)
	}
	if !strings.Contains(pageResponse.Body.String(), "Agent Task Monitor") {
		t.Fatalf("dashboard body = %q, want product name", pageResponse.Body.String())
	}
	if got := pageResponse.Header().Get("Content-Security-Policy"); !strings.Contains(got, "default-src 'self'") {
		t.Fatalf("Content-Security-Policy = %q", got)
	}

	apiRequest := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:4747/api/v1/overview", nil)
	apiResponse := httptest.NewRecorder()
	handler.ServeHTTP(apiResponse, apiRequest)
	if apiResponse.Code != http.StatusOK {
		t.Fatalf("overview status = %d, want 200; body = %s", apiResponse.Code, apiResponse.Body.String())
	}
	var envelope struct {
		Data struct {
			Summary probe.Summary `json:"summary"`
		} `json:"data"`
		Meta Meta `json:"meta"`
	}
	if err := json.Unmarshal(apiResponse.Body.Bytes(), &envelope); err != nil {
		t.Fatalf("decode overview: %v", err)
	}
	if envelope.Data.Summary.ProjectCount != 1 || envelope.Meta.APIVersion != "v1" {
		t.Fatalf("overview envelope = %#v", envelope)
	}
}

func TestHandlerRejectsNonLoopbackHostAndCrossOriginRequests(t *testing.T) {
	handler := NewHandler(newFakeSource(sampleReport()), testPreferenceStore(t), testAssets())

	for _, tt := range []struct {
		name   string
		host   string
		origin string
	}{
		{name: "non-loopback host", host: "evil.example"},
		{name: "cross origin", host: "127.0.0.1:4747", origin: "https://evil.example"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:4747/api/v1/overview", nil)
			request.Host = tt.host
			if tt.origin != "" {
				request.Header.Set("Origin", tt.origin)
			}
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != http.StatusForbidden {
				t.Fatalf("status = %d, want 403", response.Code)
			}
			if strings.Contains(response.Body.String(), "/Users/") {
				t.Fatalf("error leaked local path: %s", response.Body.String())
			}
		})
	}
}

func TestHandlerValidatesProjectListLimit(t *testing.T) {
	handler := NewHandler(newFakeSource(sampleReport()), testPreferenceStore(t), testAssets())
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:4747/api/v1/projects?limit=101", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "invalid_limit") {
		t.Fatalf("body = %s, want stable error code", response.Body.String())
	}
}

func TestProjectListIncludesSummaryAndSearchesIt(t *testing.T) {
	report := sampleReport()
	report.Projects[0].Summary = "汇总本地任务状态的实时看板。"
	report.Projects[0].SummarySource = "manual_override"
	report.Projects[0].SummaryQuality = "confirmed"
	handler := NewHandler(newFakeSource(report), testPreferenceStore(t), testAssets())
	request := httptest.NewRequest(http.MethodGet, "http://127.0.0.1:4747/api/v1/projects?search=实时看板", nil)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"summary":"汇总本地任务状态的实时看板。"`) ||
		!strings.Contains(response.Body.String(), `"summaryQuality":"confirmed"`) {
		t.Fatalf("body = %s", response.Body.String())
	}
}

func TestHandlerSendsSnapshotEventImmediately(t *testing.T) {
	source := newFakeSource(sampleReport())
	server := httptest.NewServer(NewHandler(source, testPreferenceStore(t), testAssets()))
	defer server.Close()

	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, server.URL+"/api/v1/events", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	response, err := server.Client().Do(request)
	if err != nil {
		t.Fatalf("events request: %v", err)
	}
	defer response.Body.Close()
	if response.Header.Get("Content-Type") != "text/event-stream" {
		t.Fatalf("Content-Type = %q", response.Header.Get("Content-Type"))
	}

	reader := bufio.NewReader(response.Body)
	var event strings.Builder
	for i := 0; i < 5; i++ {
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			t.Fatalf("read event: %v", readErr)
		}
		event.WriteString(line)
		if line == "\n" {
			break
		}
	}
	if !strings.Contains(event.String(), "event: snapshot.updated") {
		t.Fatalf("event = %q", event.String())
	}
}

func TestHandlerUpdatesAProjectPriority(t *testing.T) {
	preferences := testPreferenceStore(t)
	handler := NewHandler(newFakeSource(sampleReport()), preferences, testAssets())
	request := httptest.NewRequest(http.MethodPatch, "http://127.0.0.1:4747/api/v1/projects/project_alpha/preferences", strings.NewReader(`{"priority":"p0"}`))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Origin", "http://127.0.0.1:4747")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), `"priority":"p0"`) || !strings.Contains(response.Body.String(), `"rank":1000`) {
		t.Fatalf("body = %s", response.Body.String())
	}
	loaded, err := preferences.Load()
	if err != nil {
		t.Fatalf("load preferences: %v", err)
	}
	if loaded["Alpha"].Priority != projectprefs.PriorityP0 {
		t.Fatalf("stored preference = %#v", loaded)
	}
}

func TestHandlerRejectsInvalidPreferenceWrites(t *testing.T) {
	handler := NewHandler(newFakeSource(sampleReport()), testPreferenceStore(t), testAssets())
	tests := []struct {
		name        string
		projectID   string
		contentType string
		body        string
		wantStatus  int
		wantCode    string
	}{
		{name: "unknown project", projectID: "missing", contentType: "application/json", body: `{"priority":"p0"}`, wantStatus: 404, wantCode: "project_not_found"},
		{name: "invalid priority", projectID: "project_alpha", contentType: "application/json", body: `{"priority":"urgent"}`, wantStatus: 422, wantCode: "invalid_priority"},
		{name: "unknown field", projectID: "project_alpha", contentType: "application/json", body: `{"priority":"p0","path":"/tmp"}`, wantStatus: 400, wantCode: "invalid_request"},
		{name: "wrong content type", projectID: "project_alpha", contentType: "text/plain", body: `{"priority":"p0"}`, wantStatus: 415, wantCode: "unsupported_media_type"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodPatch, "http://127.0.0.1:4747/api/v1/projects/"+tt.projectID+"/preferences", strings.NewReader(tt.body))
			request.Header.Set("Content-Type", tt.contentType)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)
			if response.Code != tt.wantStatus || !strings.Contains(response.Body.String(), tt.wantCode) {
				t.Fatalf("status/body = %d %s", response.Code, response.Body.String())
			}
		})
	}
}

func TestHandlerReordersProjectsWithinPriority(t *testing.T) {
	report := sampleReport()
	beta := report.Projects[0]
	beta.ID = "project_beta"
	beta.Name = "Beta"
	beta.CanonicalPath = "/Users/example/CodeX/Beta"
	report.Projects = append(report.Projects, beta)
	preferences := testPreferenceStore(t)
	if _, err := preferences.SetPriority("Alpha", projectprefs.PriorityP1); err != nil {
		t.Fatalf("set Alpha priority: %v", err)
	}
	if _, err := preferences.SetPriority("Beta", projectprefs.PriorityP1); err != nil {
		t.Fatalf("set Beta priority: %v", err)
	}
	handler := NewHandler(newFakeSource(report), preferences, testAssets())
	request := httptest.NewRequest(http.MethodPut, "http://127.0.0.1:4747/api/v1/project-order", strings.NewReader(`{"priority":"p1","projectIds":["project_beta","project_alpha"]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("status = %d; body = %s", response.Code, response.Body.String())
	}
	loaded, err := preferences.Load()
	if err != nil {
		t.Fatalf("load preferences: %v", err)
	}
	if loaded["Beta"].Rank != 1000 || loaded["Alpha"].Rank != 2000 {
		t.Fatalf("stored order = %#v", loaded)
	}
}

func TestSortProjectsByUserPriorityAndRank(t *testing.T) {
	now := time.Date(2026, 7, 21, 5, 0, 0, 0, time.UTC)
	projects := []projectView{
		{ID: "unset", Name: "Unset", LastActivityAt: now.Add(time.Hour)},
		{ID: "p1-second", Name: "P1 second", Priority: projectprefs.PriorityP1, PriorityRank: 2000},
		{ID: "p0", Name: "P0", Priority: projectprefs.PriorityP0, PriorityRank: 1000},
		{ID: "p1-first", Name: "P1 first", Priority: projectprefs.PriorityP1, PriorityRank: 1000},
	}
	sortProjects(projects, "priority")
	want := []string{"p0", "p1-first", "p1-second", "unset"}
	for index, id := range want {
		if projects[index].ID != id {
			t.Fatalf("projects[%d] = %q, want %q", index, projects[index].ID, id)
		}
	}
}

type fakeSource struct {
	report  probe.Report
	updates chan struct{}
}

func newFakeSource(report probe.Report) *fakeSource {
	return &fakeSource{report: report, updates: make(chan struct{}, 1)}
}

func (source *fakeSource) Snapshot() (probe.Report, error) { return source.report, nil }
func (source *fakeSource) Subscribe() (<-chan struct{}, func()) {
	return source.updates, func() {}
}

func sampleReport() probe.Report {
	now := time.Date(2026, 7, 21, 5, 0, 0, 0, time.UTC)
	return probe.Report{
		GeneratedAt: now,
		Sources: probe.Sources{
			StateDB:  probe.StateDBSource{Status: "healthy", ThreadRecords: 1},
			Sessions: probe.SessionsSource{Status: "healthy", FilesScanned: 1},
		},
		Summary: probe.Summary{
			ProjectCount: 1,
			ThreadCount:  1,
			StatusCounts: map[statusengine.Status]int{statusengine.StatusWorking: 1},
		},
		Projects: []probe.Project{{
			ID:             "project_alpha",
			Name:           "Alpha",
			CanonicalPath:  "/Users/example/CodeX/Alpha",
			Status:         statusengine.StatusWorking,
			StatusQuality:  statusengine.QualityInferred,
			LastActivityAt: now,
			ThreadCounts:   map[statusengine.Status]int{statusengine.StatusWorking: 1},
			Threads: []probe.Thread{{
				ID:             "thread_alpha",
				ProjectID:      "project_alpha",
				Title:          "Build dashboard",
				Status:         statusengine.StatusWorking,
				StatusQuality:  statusengine.QualityInferred,
				StatusReason:   statusengine.ReasonRecentSessionActivity,
				LastActivityAt: now,
				DeepLink:       "codex://threads/thread_alpha",
			}},
		}},
	}
}

func testAssets() fstest.MapFS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte("<!doctype html><title>Agent Task Monitor</title>")},
	}
}

func testPreferenceStore(t *testing.T) *projectprefs.Store {
	t.Helper()
	return projectprefs.NewStore(projectprefs.Options{
		Path: filepath.Join(t.TempDir(), "project-preferences.json"),
		Now:  func() time.Time { return time.Date(2026, 7, 21, 8, 30, 0, 0, time.UTC) },
	})
}
