package httpapi

import (
	"net/http"
	"strings"

	"github.com/itswl/quotapulse/internal/model"
	"github.com/itswl/quotapulse/internal/provider"
	"github.com/itswl/quotapulse/internal/store"
)

func trimSpace(s string) string { return strings.TrimSpace(s) }

// Implementation note.
// Implementation note.
func (s *Server) requireDynamicConfig(w http.ResponseWriter, what string) bool {
	if s.Settings.EnableDynamicConfig {
		return true
	}
	fail(w, http.StatusServiceUnavailable, "Changing "+what+" requires database dynamic configuration; set ENABLE_DYNAMIC_CONFIG=true")
	return false
}

func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	etagJSON(w, r, map[string]any{"status": "success", "providers": provider.All()})
}

// Implementation note.
type maskedProject struct {
	Name         string  `json:"name"`
	Provider     string  `json:"provider"`
	APIKey       string  `json:"api_key"`
	Threshold    float64 `json:"threshold"`
	Type         string  `json:"type"`
	OwnerProject *string `json:"owner_project"`
	Enabled      bool    `json:"enabled"`
	FromEnv      bool    `json:"from_env"`
}

func (s *Server) handleListProjects(w http.ResponseWriter, r *http.Request) {
	cfg := s.Resolver.Load(r.Context())
	projects := make([]maskedProject, 0, len(cfg.Projects))
	for _, p := range cfg.Projects {
		projects = append(projects, maskedProject{
			Name: p.Name, Provider: p.Provider, APIKey: maskSecret(p.APIKey, 4, 4),
			Threshold: p.Threshold, Type: p.Type, OwnerProject: p.OwnerProject,
			Enabled: p.Enabled, FromEnv: p.FromEnv,
		})
	}
	etagJSON(w, r, map[string]any{"status": "success", "projects": projects})
}

type projectRequest struct {
	Name         string   `json:"name"`
	Provider     *string  `json:"provider"`
	APIKey       *string  `json:"api_key"`
	Threshold    *float64 `json:"threshold"`
	Type         *string  `json:"type"`
	OwnerProject *string  `json:"owner_project"`
	Enabled      *bool    `json:"enabled"`
}

// Implementation note.
// Implementation note.
func (s *Server) handleSaveProject(w http.ResponseWriter, r *http.Request) {
	if !s.requireDynamicConfig(w, "projects") {
		return
	}
	var body projectRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	body.Name = trimSpace(body.Name)
	if body.Name == "" {
		failValidation(w, []string{"name: cannot be empty"})
		return
	}

	cfg := s.Resolver.Load(r.Context())
	existing := findProject(cfg.Projects, body.Name)
	isNew := existing == nil

	target := model.Project{Name: body.Name, Enabled: true}
	if existing != nil {
		target = *existing
	}
	applyProjectPatch(&target, body)

	if isNew {
		var missing []string
		if target.Provider == "" {
			missing = append(missing, "provider")
		}
		if target.APIKey == "" {
			missing = append(missing, "api_key")
		}
		if len(missing) > 0 {
			fail(w, http.StatusBadRequest, "A new project is missing required fields: "+strings.Join(missing, ", "))
			return
		}
	}
	if _, known := provider.Lookup(target.Provider); !known {
		fail(w, http.StatusBadRequest, "Unknown provider: "+target.Provider+"; supported: "+strings.Join(provider.Keys(), ", "))
		return
	}
	model.NormalizeProject(&target)
	// Implementation note.
	target.FromEnv = false

	if err := s.Store.UpsertProject(r.Context(), target); err != nil {
		s.log().Error("Failed to save project configuration", "project", body.Name, "error", err)
		fail(w, http.StatusInternalServerError, "Save failed")
		return
	}
	s.log().Info("[AUDIT] Project saved", "project", body.Name, "new", isNew)

	s.refreshOne(r, target.Name)
	action := "updated"
	if isNew {
		action = "added"
	}
	ok(w, map[string]any{"message": "Project [" + body.Name + "] " + action})
}

func applyProjectPatch(target *model.Project, body projectRequest) {
	if body.Provider != nil && *body.Provider != "" {
		target.Provider = strings.ToLower(trimSpace(*body.Provider))
	}
	if body.APIKey != nil && trimSpace(*body.APIKey) != "" {
		target.APIKey = trimSpace(*body.APIKey)
	}
	if body.Threshold != nil {
		target.Threshold = *body.Threshold
	}
	if body.Type != nil && *body.Type != "" {
		target.Type = *body.Type
	}
	if body.OwnerProject != nil {
		target.OwnerProject = model.OwnerProjectOf(*body.OwnerProject)
	}
	if body.Enabled != nil {
		target.Enabled = *body.Enabled
	}
}

type nameRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleDeleteProject(w http.ResponseWriter, r *http.Request) {
	if !s.requireDynamicConfig(w, "projects") {
		return
	}
	var body nameRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.Name == "" {
		fail(w, http.StatusBadRequest, "Missing required parameter: name")
		return
	}

	cfg := s.Resolver.Load(r.Context())
	existing := findProject(cfg.Projects, body.Name)
	if existing == nil {
		fail(w, http.StatusNotFound, "Project not found: "+body.Name)
		return
	}
	// Implementation note.
	if existing.FromEnv {
		fail(w, http.StatusBadRequest, "Project ["+body.Name+"] was auto-discovered from environment variables; remove "+
			strings.ToUpper(existing.Provider)+"_API_KEY and restart")
		return
	}

	if err := s.Store.DeleteProject(r.Context(), body.Name); err != nil {
		s.log().Error("Failed to delete project configuration", "project", body.Name, "error", err)
		fail(w, http.StatusInternalServerError, "Delete failed")
		return
	}
	s.State.RemoveBalanceProject(body.Name)
	if s.OnBalanceUpdated != nil {
		s.OnBalanceUpdated(s.State.Balance().Projects)
	}
	s.log().Info("[AUDIT] Project deleted", "project", body.Name)
	ok(w, map[string]any{"message": "Project [" + body.Name + "] deleted"})
}

type thresholdRequest struct {
	ProjectName  string   `json:"project_name"`
	NewThreshold *float64 `json:"new_threshold"`
}

// Implementation note.
func (s *Server) handleUpdateThreshold(w http.ResponseWriter, r *http.Request) {
	if !s.requireDynamicConfig(w, "projects") {
		return
	}
	var body thresholdRequest
	if !decodeJSON(w, r, &body) {
		return
	}
	if body.ProjectName == "" || body.NewThreshold == nil {
		fail(w, http.StatusBadRequest, "Missing required parameters: project_name, new_threshold")
		return
	}
	if *body.NewThreshold < 0 {
		fail(w, http.StatusBadRequest, "The threshold cannot be negative")
		return
	}

	cfg := s.Resolver.Load(r.Context())
	target := findProject(cfg.Projects, body.ProjectName)
	if target == nil {
		fail(w, http.StatusNotFound, "Project not found: "+body.ProjectName)
		return
	}
	updated := *target
	updated.Threshold = *body.NewThreshold
	updated.FromEnv = false

	if err := s.Store.UpsertProject(r.Context(), updated); err != nil {
		s.log().Error("Failed to update threshold", "project", body.ProjectName, "error", err)
		fail(w, http.StatusInternalServerError, "Save failed")
		return
	}
	s.log().Info("[AUDIT] Project threshold updated", "project", body.ProjectName,
		"old", target.Threshold, "new", *body.NewThreshold)

	s.refreshOne(r, body.ProjectName)
	ok(w, map[string]any{"message": "Threshold updated for project [" + body.ProjectName + "]"})
}

// Implementation note.
func (s *Server) refreshOne(r *http.Request, name string) {
	outcome, err := s.Monitor.Run(r.Context(), name, !s.Settings.EnableWebAlarm)
	if err != nil {
		s.log().Warn("Failed to refresh project", "project", name, "error", err)
		return
	}
	s.State.MergeBalance(outcome.Results)
	if s.OnBalanceUpdated != nil {
		s.OnBalanceUpdated(s.State.Balance().Projects)
	}
}

func findProject(projects []model.Project, name string) *model.Project {
	for i := range projects {
		if projects[i].Name == name {
			return &projects[i]
		}
	}
	return nil
}

// Implementation note.
func storeWriteStatus(err error) int {
	if err == store.ErrDisabled {
		return http.StatusServiceUnavailable
	}
	return http.StatusInternalServerError
}
