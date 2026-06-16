package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// teamResponse is the public JSON shape for a team.
type teamResponse struct {
	ID          string    `json:"id"`
	Slug        string    `json:"slug"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
}

func newTeamResponse(t db.Team) teamResponse {
	return teamResponse{
		ID:          t.ID.String(),
		Slug:        t.Slug,
		Name:        t.Name,
		Description: t.Description,
		CreatedAt:   t.CreatedAt,
	}
}

// teamMemberResponse is the public JSON shape for a team member: the user plus
// their role within the team.
type teamMemberResponse struct {
	ID          string `json:"id"`
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role"`
}

func newTeamMemberResponse(m db.ListTeamMembersRow) teamMemberResponse {
	return teamMemberResponse{
		ID:          m.ID.String(),
		Username:    m.Username,
		Email:       m.Email,
		DisplayName: m.DisplayName,
		Role:        m.MemberRole,
	}
}

// myTeamResponse is a team the signed-in user belongs to, annotated with its org
// so the cross-org "/teams" page can link back to each one.
type myTeamResponse struct {
	ID          string `json:"id"`
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Role        string `json:"role"`
	OrgSlug     string `json:"orgSlug"`
	OrgName     string `json:"orgName"`
}

func newMyTeamResponse(t db.ListTeamsByUserRow) myTeamResponse {
	return myTeamResponse{
		ID:          t.ID.String(),
		Slug:        t.Slug,
		Name:        t.Name,
		Description: t.Description,
		Role:        t.MemberRole,
		OrgSlug:     t.OrgSlug,
		OrgName:     t.OrgName,
	}
}

type createTeamRequest struct {
	Slug        string `json:"slug"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

type addTeamMemberRequest struct {
	Username string `json:"username"`
	Role     string `json:"role"`
}

// handleListTeams returns the teams within an organization.
func (s *Server) handleListTeams(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	org, teams, err := s.platform.ListTeams(r.Context(), actor, chi.URLParam(r, "slug"))
	if err != nil {
		s.writePlatformError(w, err, "could not list teams")
		return
	}
	out := make([]teamResponse, 0, len(teams))
	for _, t := range teams {
		out = append(out, newTeamResponse(t))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"organization": newOrgResponse(org),
		"teams":        out,
	})
}

// handleCreateTeam provisions a team under an organization (org owners only).
func (s *Server) handleCreateTeam(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createTeamRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	team, err := s.platform.CreateTeam(r.Context(), actor, chi.URLParam(r, "slug"), platform.CreateTeamInput{
		Slug:        req.Slug,
		Name:        req.Name,
		Description: req.Description,
	})
	if err != nil {
		s.writePlatformError(w, err, "could not create team")
		return
	}
	httpx.JSON(w, http.StatusCreated, newTeamResponse(team))
}

// handleGetTeam returns a single team and its members.
func (s *Server) handleGetTeam(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	org, team, members, err := s.platform.GetTeam(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "team"))
	if err != nil {
		s.writePlatformError(w, err, "could not load team")
		return
	}
	mout := make([]teamMemberResponse, 0, len(members))
	for _, m := range members {
		mout = append(mout, newTeamMemberResponse(m))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{
		"organization": newOrgResponse(org),
		"team":         newTeamResponse(team),
		"members":      mout,
	})
}

// handleAddTeamMember adds (or updates the role of) a user in a team by username.
func (s *Server) handleAddTeamMember(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req addTeamMemberRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	err := s.platform.AddTeamMember(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "team"), req.Username, req.Role)
	if err != nil {
		s.writePlatformError(w, err, "could not add team member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRemoveTeamMember removes a user from a team by user ID.
func (s *Server) handleRemoveTeamMember(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userID"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "invalid user id")
		return
	}
	if err := s.platform.RemoveTeamMember(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "team"), userID); err != nil {
		s.writePlatformError(w, err, "could not remove team member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleListMyTeams returns every team the signed-in user belongs to, across all
// organizations.
func (s *Server) handleListMyTeams(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	teams, err := s.platform.ListMyTeams(r.Context(), actor)
	if err != nil {
		s.writePlatformError(w, err, "could not list your teams")
		return
	}
	out := make([]myTeamResponse, 0, len(teams))
	for _, t := range teams {
		out = append(out, newMyTeamResponse(t))
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"teams": out})
}
