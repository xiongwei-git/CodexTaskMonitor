package api

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"mime"
	"net"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/xiongwei-git/agent-task-monitor/internal/probe"
	"github.com/xiongwei-git/agent-task-monitor/internal/projectprefs"
	"github.com/xiongwei-git/agent-task-monitor/internal/statusengine"
)

const apiVersion = "v1"

type SnapshotSource interface {
	Snapshot() (probe.Report, error)
	Subscribe() (<-chan struct{}, func())
}

type PreferenceStore interface {
	SetPriority(string, projectprefs.Priority) (projectprefs.Preference, error)
	Reorder(projectprefs.Priority, []string) error
}

type Meta struct {
	GeneratedAt time.Time `json:"generatedAt"`
	APIVersion  string    `json:"apiVersion"`
	NextCursor  *string   `json:"nextCursor"`
}

type errorBody struct {
	Error struct {
		Code      string `json:"code"`
		Message   string `json:"message"`
		RequestID string `json:"requestId"`
	} `json:"error"`
}

type overviewData struct {
	Summary   probe.Summary  `json:"summary"`
	Sources   probe.Sources  `json:"sources"`
	Attention []probe.Thread `json:"attention"`
	Recent    []probe.Thread `json:"recent"`
}

type projectView struct {
	ID                  string                      `json:"id"`
	Name                string                      `json:"name"`
	CanonicalPath       string                      `json:"canonicalPath"`
	Summary             string                      `json:"summary"`
	SummarySource       string                      `json:"summarySource"`
	SummaryQuality      string                      `json:"summaryQuality"`
	SummaryUpdatedAt    string                      `json:"summaryUpdatedAt,omitempty"`
	Priority            projectprefs.Priority       `json:"priority"`
	PriorityRank        int                         `json:"priorityRank"`
	PreferenceUpdatedAt *time.Time                  `json:"preferenceUpdatedAt,omitempty"`
	Status              statusengine.Status         `json:"status"`
	StatusQuality       statusengine.Quality        `json:"statusQuality"`
	LastActivityAt      time.Time                   `json:"lastActivityAt"`
	ThreadCounts        map[statusengine.Status]int `json:"threadCounts"`
	ThreadTotal         int                         `json:"threadTotal"`
	Runtime             probe.ProjectRuntime        `json:"runtime"`
}

type preferenceView struct {
	ProjectID           string                `json:"projectId"`
	Priority            projectprefs.Priority `json:"priority"`
	PriorityRank        int                   `json:"rank"`
	PreferenceUpdatedAt *time.Time            `json:"updatedAt,omitempty"`
}

type updatePreferenceRequest struct {
	Priority projectprefs.Priority `json:"priority"`
}

type updateOrderRequest struct {
	Priority   projectprefs.Priority `json:"priority"`
	ProjectIDs []string              `json:"projectIds"`
}

type handler struct {
	source      SnapshotSource
	preferences PreferenceStore
	static      http.Handler
	ids         atomic.Uint64
}

func NewHandler(source SnapshotSource, preferences PreferenceStore, assets fs.FS) http.Handler {
	h := &handler{source: source, preferences: preferences, static: http.FileServer(http.FS(assets))}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", h.health)
	mux.HandleFunc("GET /api/v1/overview", h.overview)
	mux.HandleFunc("GET /api/v1/projects", h.projects)
	mux.HandleFunc("GET /api/v1/projects/{projectId}", h.project)
	mux.HandleFunc("PATCH /api/v1/projects/{projectId}/preferences", h.updateProjectPreference)
	mux.HandleFunc("PUT /api/v1/project-order", h.updateProjectOrder)
	mux.HandleFunc("GET /api/v1/threads", h.threads)
	mux.HandleFunc("GET /api/v1/threads/{threadId}", h.thread)
	mux.HandleFunc("GET /api/v1/sources", h.sources)
	mux.HandleFunc("GET /api/v1/events", h.events)
	mux.Handle("GET /", h.static)
	return h.securityHeaders(mux)
}

func (h *handler) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Cache-Control", "no-store")
		response.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' data:; connect-src 'self'; base-uri 'none'; frame-ancestors 'none'; form-action 'none'")
		response.Header().Set("Referrer-Policy", "no-referrer")
		response.Header().Set("X-Content-Type-Options", "nosniff")
		response.Header().Set("X-Frame-Options", "DENY")
		response.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")

		if !isLoopbackHost(request.Host) || !sameOrigin(request) {
			h.writeError(response, http.StatusForbidden, "request_forbidden", "Request origin is not allowed.")
			return
		}
		next.ServeHTTP(response, request)
	})
}

func (h *handler) health(response http.ResponseWriter, _ *http.Request) {
	writeJSON(response, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *handler) overview(response http.ResponseWriter, _ *http.Request) {
	report, ok := h.snapshot(response)
	if !ok {
		return
	}

	threads := flattenThreads(report.Projects)
	sort.Slice(threads, func(i, j int) bool { return threads[i].LastActivityAt.After(threads[j].LastActivityAt) })
	recent := append([]probe.Thread(nil), threads...)
	if len(recent) > 12 {
		recent = recent[:12]
	}
	attention := make([]probe.Thread, 0, 6)
	for _, thread := range threads {
		if needsAttention(thread.Status) {
			attention = append(attention, thread)
			if len(attention) == 6 {
				break
			}
		}
	}

	writeJSON(response, http.StatusOK, map[string]any{
		"data": overviewData{Summary: report.Summary, Sources: report.Sources, Attention: attention, Recent: recent},
		"meta": Meta{GeneratedAt: report.GeneratedAt, APIVersion: apiVersion},
	})
}

func (h *handler) projects(response http.ResponseWriter, request *http.Request) {
	report, ok := h.snapshot(response)
	if !ok {
		return
	}
	limit, offset, ok := h.listOptions(response, request)
	if !ok {
		return
	}

	statuses, ok := h.statusFilters(response, request)
	if !ok {
		return
	}
	search := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("search")))
	sortMode := request.URL.Query().Get("sort")
	if sortMode == "" {
		sortMode = "priority"
	}
	if sortMode != "priority" && sortMode != "attention" && sortMode != "last_activity" && sortMode != "name" {
		h.writeError(response, http.StatusUnprocessableEntity, "invalid_sort", "The requested sort is not supported.")
		return
	}

	projects := make([]projectView, 0, len(report.Projects))
	for _, project := range report.Projects {
		if len(statuses) > 0 && !statuses[project.Status] {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(project.Name), search) &&
			!strings.Contains(strings.ToLower(project.Summary), search) {
			continue
		}
		projects = append(projects, newProjectView(project))
	}
	sortProjects(projects, sortMode)
	page, next := paginate(projects, offset, limit)
	writeJSON(response, http.StatusOK, map[string]any{
		"data": page,
		"meta": Meta{GeneratedAt: report.GeneratedAt, APIVersion: apiVersion, NextCursor: next},
	})
}

func (h *handler) project(response http.ResponseWriter, request *http.Request) {
	report, ok := h.snapshot(response)
	if !ok {
		return
	}
	projectID := request.PathValue("projectId")
	for _, project := range report.Projects {
		if project.ID == projectID {
			writeJSON(response, http.StatusOK, map[string]any{
				"data": project,
				"meta": Meta{GeneratedAt: report.GeneratedAt, APIVersion: apiVersion},
			})
			return
		}
	}
	h.writeError(response, http.StatusNotFound, "project_not_found", "Project was not found.")
}

func (h *handler) updateProjectPreference(response http.ResponseWriter, request *http.Request) {
	report, ok := h.snapshot(response)
	if !ok {
		return
	}
	project, ok := findProject(report.Projects, request.PathValue("projectId"))
	if !ok {
		h.writeError(response, http.StatusNotFound, "project_not_found", "Project was not found.")
		return
	}
	var input updatePreferenceRequest
	if !h.decodeJSON(response, request, &input) {
		return
	}
	if !validPriority(input.Priority, true) {
		h.writeError(response, http.StatusUnprocessableEntity, "invalid_priority", "Priority must be p0, p1, p2, p3, or unset.")
		return
	}
	if h.preferences == nil {
		h.writeError(response, http.StatusServiceUnavailable, "preferences_unavailable", "Project preferences are temporarily unavailable.")
		return
	}
	preference, err := h.preferences.SetPriority(project.Name, input.Priority)
	if err != nil {
		if errors.Is(err, projectprefs.ErrInvalidPriority) {
			h.writeError(response, http.StatusUnprocessableEntity, "invalid_priority", "Priority is not supported.")
			return
		}
		h.writeError(response, http.StatusServiceUnavailable, "preferences_unavailable", "Project preferences could not be saved.")
		return
	}
	view := preferenceView{ProjectID: project.ID, Priority: preference.Priority, PriorityRank: preference.Rank}
	if !preference.UpdatedAt.IsZero() {
		updatedAt := preference.UpdatedAt
		view.PreferenceUpdatedAt = &updatedAt
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"data": view,
		"meta": Meta{GeneratedAt: report.GeneratedAt, APIVersion: apiVersion},
	})
}

func (h *handler) updateProjectOrder(response http.ResponseWriter, request *http.Request) {
	report, ok := h.snapshot(response)
	if !ok {
		return
	}
	var input updateOrderRequest
	if !h.decodeJSON(response, request, &input) {
		return
	}
	if !validPriority(input.Priority, false) {
		h.writeError(response, http.StatusUnprocessableEntity, "invalid_priority", "Order priority must be p0, p1, p2, or p3.")
		return
	}
	if len(input.ProjectIDs) == 0 || len(input.ProjectIDs) > 100 {
		h.writeError(response, http.StatusUnprocessableEntity, "invalid_project_order", "Project order must contain between 1 and 100 projects.")
		return
	}
	projectKeys := make([]string, 0, len(input.ProjectIDs))
	for _, projectID := range input.ProjectIDs {
		project, exists := findProject(report.Projects, projectID)
		if !exists {
			h.writeError(response, http.StatusNotFound, "project_not_found", "A project in the requested order was not found.")
			return
		}
		projectKeys = append(projectKeys, project.Name)
	}
	if h.preferences == nil {
		h.writeError(response, http.StatusServiceUnavailable, "preferences_unavailable", "Project preferences are temporarily unavailable.")
		return
	}
	if err := h.preferences.Reorder(input.Priority, projectKeys); err != nil {
		if errors.Is(err, projectprefs.ErrInvalidOrder) {
			h.writeError(response, http.StatusConflict, "project_order_conflict", "Project priorities changed; refresh before reordering.")
			return
		}
		h.writeError(response, http.StatusServiceUnavailable, "preferences_unavailable", "Project order could not be saved.")
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"data": map[string]any{"priority": input.Priority, "projectIds": input.ProjectIDs},
		"meta": Meta{GeneratedAt: report.GeneratedAt, APIVersion: apiVersion},
	})
}

func (h *handler) threads(response http.ResponseWriter, request *http.Request) {
	report, ok := h.snapshot(response)
	if !ok {
		return
	}
	limit, offset, ok := h.listOptions(response, request)
	if !ok {
		return
	}
	statuses, ok := h.statusFilters(response, request)
	if !ok {
		return
	}
	projectID := request.URL.Query().Get("projectId")
	search := strings.ToLower(strings.TrimSpace(request.URL.Query().Get("search")))

	threads := flattenThreads(report.Projects)
	filtered := threads[:0]
	for _, thread := range threads {
		if projectID != "" && thread.ProjectID != projectID {
			continue
		}
		if len(statuses) > 0 && !statuses[thread.Status] {
			continue
		}
		if search != "" && !strings.Contains(strings.ToLower(thread.Title), search) {
			continue
		}
		filtered = append(filtered, thread)
	}
	sort.Slice(filtered, func(i, j int) bool {
		return filtered[i].LastActivityAt.After(filtered[j].LastActivityAt)
	})
	page, next := paginate(filtered, offset, limit)
	writeJSON(response, http.StatusOK, map[string]any{
		"data": page,
		"meta": Meta{GeneratedAt: report.GeneratedAt, APIVersion: apiVersion, NextCursor: next},
	})
}

func (h *handler) thread(response http.ResponseWriter, request *http.Request) {
	report, ok := h.snapshot(response)
	if !ok {
		return
	}
	threadID := request.PathValue("threadId")
	for _, thread := range flattenThreads(report.Projects) {
		if thread.ID == threadID {
			writeJSON(response, http.StatusOK, map[string]any{
				"data": thread,
				"meta": Meta{GeneratedAt: report.GeneratedAt, APIVersion: apiVersion},
			})
			return
		}
	}
	h.writeError(response, http.StatusNotFound, "thread_not_found", "Task was not found.")
}

func (h *handler) sources(response http.ResponseWriter, _ *http.Request) {
	report, ok := h.snapshot(response)
	if !ok {
		return
	}
	writeJSON(response, http.StatusOK, map[string]any{
		"data": report.Sources,
		"meta": Meta{GeneratedAt: report.GeneratedAt, APIVersion: apiVersion},
	})
}

func (h *handler) events(response http.ResponseWriter, request *http.Request) {
	flusher, ok := response.(http.Flusher)
	if !ok {
		h.writeError(response, http.StatusInternalServerError, "stream_unavailable", "Live updates are unavailable.")
		return
	}
	response.Header().Set("Content-Type", "text/event-stream")
	response.Header().Set("Cache-Control", "no-cache, no-transform")
	response.Header().Set("X-Accel-Buffering", "no")

	updates, cancel := h.source.Subscribe()
	defer cancel()
	if !h.sendSnapshotEvent(response, flusher) {
		return
	}
	heartbeat := time.NewTicker(15 * time.Second)
	defer heartbeat.Stop()
	for {
		select {
		case <-request.Context().Done():
			return
		case _, open := <-updates:
			if !open || !h.sendSnapshotEvent(response, flusher) {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(response, ": keep-alive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

func (h *handler) sendSnapshotEvent(response http.ResponseWriter, flusher http.Flusher) bool {
	report, err := h.source.Snapshot()
	if err != nil {
		return false
	}
	now := time.Now().UTC()
	eventID := strconv.FormatInt(now.UnixNano(), 36)
	payload, err := json.Marshal(map[string]any{
		"eventId":    eventID,
		"occurredAt": now,
		"type":       "snapshot.updated",
		"data":       map[string]any{"generatedAt": report.GeneratedAt},
	})
	if err != nil {
		return false
	}
	if _, err := fmt.Fprintf(response, "id: %s\nevent: snapshot.updated\ndata: %s\n\n", eventID, payload); err != nil {
		return false
	}
	flusher.Flush()
	return true
}

func (h *handler) snapshot(response http.ResponseWriter) (probe.Report, bool) {
	report, err := h.source.Snapshot()
	if err != nil {
		h.writeError(response, http.StatusServiceUnavailable, "source_unavailable", "Codex state is temporarily unavailable.")
		return probe.Report{}, false
	}
	return report, true
}

func (h *handler) decodeJSON(response http.ResponseWriter, request *http.Request, target any) bool {
	mediaType, _, err := mime.ParseMediaType(request.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" {
		h.writeError(response, http.StatusUnsupportedMediaType, "unsupported_media_type", "Content-Type must be application/json.")
		return false
	}
	request.Body = http.MaxBytesReader(response, request.Body, 16<<10)
	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			h.writeError(response, http.StatusRequestEntityTooLarge, "request_too_large", "Request body is too large.")
			return false
		}
		h.writeError(response, http.StatusBadRequest, "invalid_request", "Request body is not valid JSON.")
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		h.writeError(response, http.StatusBadRequest, "invalid_request", "Request body must contain one JSON object.")
		return false
	}
	return true
}

func (h *handler) listOptions(response http.ResponseWriter, request *http.Request) (int, int, bool) {
	limit := 50
	if raw := request.URL.Query().Get("limit"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed < 1 || parsed > 100 {
			h.writeError(response, http.StatusUnprocessableEntity, "invalid_limit", "Limit must be between 1 and 100.")
			return 0, 0, false
		}
		limit = parsed
	}
	offset := 0
	if cursor := request.URL.Query().Get("cursor"); cursor != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || !strings.HasPrefix(string(decoded), "offset:") {
			h.writeError(response, http.StatusUnprocessableEntity, "invalid_cursor", "Cursor is invalid.")
			return 0, 0, false
		}
		parsed, err := strconv.Atoi(strings.TrimPrefix(string(decoded), "offset:"))
		if err != nil || parsed < 0 {
			h.writeError(response, http.StatusUnprocessableEntity, "invalid_cursor", "Cursor is invalid.")
			return 0, 0, false
		}
		offset = parsed
	}
	return limit, offset, true
}

func (h *handler) statusFilters(response http.ResponseWriter, request *http.Request) (map[statusengine.Status]bool, bool) {
	filters := make(map[statusengine.Status]bool)
	for _, raw := range request.URL.Query()["status"] {
		status := statusengine.Status(raw)
		if !validStatus(status) {
			h.writeError(response, http.StatusUnprocessableEntity, "invalid_status_filter", "The status filter is not supported.")
			return nil, false
		}
		filters[status] = true
	}
	return filters, true
}

func (h *handler) writeError(response http.ResponseWriter, status int, code, message string) {
	requestID := fmt.Sprintf("request_%x", h.ids.Add(1))
	body := errorBody{}
	body.Error.Code = code
	body.Error.Message = message
	body.Error.RequestID = requestID
	writeJSON(response, status, body)
}

func writeJSON(response http.ResponseWriter, status int, value any) {
	response.Header().Set("Content-Type", "application/json; charset=utf-8")
	response.Header().Set("Cache-Control", "no-store")
	response.WriteHeader(status)
	_ = json.NewEncoder(response).Encode(value)
}

func isLoopbackHost(value string) bool {
	host := value
	if parsed, _, err := net.SplitHostPort(value); err == nil {
		host = parsed
	}
	host = strings.Trim(host, "[]")
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func sameOrigin(request *http.Request) bool {
	origin := request.Header.Get("Origin")
	if origin == "" {
		return true
	}
	parsed, err := url.Parse(origin)
	if err != nil || parsed.Scheme != "http" || parsed.Path != "" {
		return false
	}
	return strings.EqualFold(parsed.Host, request.Host)
}

func validStatus(status statusengine.Status) bool {
	switch status {
	case statusengine.StatusWorking, statusengine.StatusWaiting, statusengine.StatusCompleted,
		statusengine.StatusInterrupted, statusengine.StatusFailed, statusengine.StatusSuspectedAbnormal,
		statusengine.StatusIdle, statusengine.StatusUnknown:
		return true
	default:
		return false
	}
}

func needsAttention(status statusengine.Status) bool {
	return status == statusengine.StatusWaiting || status == statusengine.StatusFailed ||
		status == statusengine.StatusInterrupted || status == statusengine.StatusSuspectedAbnormal
}

func flattenThreads(projects []probe.Project) []probe.Thread {
	var threads []probe.Thread
	for _, project := range projects {
		threads = append(threads, project.Threads...)
	}
	return threads
}

func findProject(projects []probe.Project, projectID string) (probe.Project, bool) {
	for _, project := range projects {
		if project.ID == projectID {
			return project, true
		}
	}
	return probe.Project{}, false
}

func validPriority(priority projectprefs.Priority, allowUnset bool) bool {
	switch priority {
	case projectprefs.PriorityP0, projectprefs.PriorityP1, projectprefs.PriorityP2, projectprefs.PriorityP3:
		return true
	case projectprefs.PriorityUnset:
		return allowUnset
	default:
		return false
	}
}

func newProjectView(project probe.Project) projectView {
	return projectView{
		ID: project.ID, Name: project.Name, CanonicalPath: project.CanonicalPath,
		Summary: project.Summary, SummarySource: project.SummarySource,
		SummaryQuality: project.SummaryQuality, SummaryUpdatedAt: project.SummaryUpdatedAt,
		Priority: project.Priority, PriorityRank: project.PriorityRank,
		PreferenceUpdatedAt: project.PreferenceUpdatedAt,
		Status:              project.Status, StatusQuality: project.StatusQuality,
		LastActivityAt: project.LastActivityAt, ThreadCounts: project.ThreadCounts,
		ThreadTotal: len(project.Threads), Runtime: project.Runtime,
	}
}

func sortProjects(projects []projectView, mode string) {
	sort.Slice(projects, func(i, j int) bool {
		left, right := projects[i], projects[j]
		switch mode {
		case "name":
			return strings.ToLower(left.Name) < strings.ToLower(right.Name)
		case "last_activity":
			return left.LastActivityAt.After(right.LastActivityAt)
		case "priority":
			leftPriority := userPriority(left.Priority)
			rightPriority := userPriority(right.Priority)
			if leftPriority != rightPriority {
				return leftPriority < rightPriority
			}
			if left.Priority != projectprefs.PriorityUnset && left.PriorityRank != right.PriorityRank {
				if left.PriorityRank == 0 {
					return false
				}
				if right.PriorityRank == 0 {
					return true
				}
				return left.PriorityRank < right.PriorityRank
			}
			fallthrough
		default:
			leftPriority := statusPriority(left.Status)
			rightPriority := statusPriority(right.Status)
			if leftPriority != rightPriority {
				return leftPriority < rightPriority
			}
			return left.LastActivityAt.After(right.LastActivityAt)
		}
	})
}

func userPriority(priority projectprefs.Priority) int {
	switch priority {
	case projectprefs.PriorityP0:
		return 0
	case projectprefs.PriorityP1:
		return 1
	case projectprefs.PriorityP2:
		return 2
	case projectprefs.PriorityP3:
		return 3
	default:
		return 4
	}
}

func statusPriority(status statusengine.Status) int {
	if status == statusengine.StatusWorking {
		return 0
	}
	if needsAttention(status) {
		return 1
	}
	if status == statusengine.StatusCompleted {
		return 2
	}
	return 3
}

func paginate[T any](values []T, offset, limit int) ([]T, *string) {
	if offset >= len(values) {
		return []T{}, nil
	}
	end := offset + limit
	if end > len(values) {
		end = len(values)
	}
	var next *string
	if end < len(values) {
		encoded := base64.RawURLEncoding.EncodeToString([]byte(fmt.Sprintf("offset:%d", end)))
		next = &encoded
	}
	return values[offset:end], next
}
