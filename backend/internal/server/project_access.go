package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
)

type setProjectRoleRequest struct {
	Role string `json:"role"`
}

func (s *Server) handleListProjectAccess(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	access, err := s.platform.ListProjectAccess(r.Context(), actor, chi.URLParam(r, "slug"))
	if err != nil {
		s.writePlatformError(w, err, "could not list project access")
		return
	}
	httpx.JSON(w, http.StatusOK, access)
}

func (s *Server) handleSetProjectUserRole(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req setProjectRoleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	project, user := chi.URLParam(r, "slug"), chi.URLParam(r, "user")
	if err := s.platform.SetProjectUserRole(r.Context(), actor, project, user, req.Role); err != nil {
		s.writePlatformError(w, err, "could not set project access")
		return
	}
	s.logAudit(r, "project.access_set", "project", project, map[string]any{"user": user, "role": req.Role})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRemoveProjectUser(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "user"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_request", "invalid user id")
		return
	}
	project := chi.URLParam(r, "slug")
	if err := s.platform.RemoveProjectUser(r.Context(), actor, project, userID); err != nil {
		s.writePlatformError(w, err, "could not remove project access")
		return
	}
	s.logAudit(r, "project.access_removed", "project", project, map[string]any{"userId": userID.String()})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleSetProjectGroupRole(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req setProjectRoleRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	project, group := chi.URLParam(r, "slug"), chi.URLParam(r, "group")
	if err := s.platform.SetProjectGroupRole(r.Context(), actor, project, group, req.Role); err != nil {
		s.writePlatformError(w, err, "could not set group access")
		return
	}
	s.logAudit(r, "project.group_access_set", "project", project, map[string]any{"group": group, "role": req.Role})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleRemoveProjectGroup(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	project, group := chi.URLParam(r, "slug"), chi.URLParam(r, "group")
	if err := s.platform.RemoveProjectGroup(r.Context(), actor, project, group); err != nil {
		s.writePlatformError(w, err, "could not remove group access")
		return
	}
	s.logAudit(r, "project.group_access_removed", "project", project, map[string]any{"group": group})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleClaimProjectOwnership is the org admin's audited break-glass.
func (s *Server) handleClaimProjectOwnership(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	project := chi.URLParam(r, "slug")
	if err := s.platform.SelfGrantProjectOwner(r.Context(), actor, project); err != nil {
		s.writePlatformError(w, err, "could not claim project ownership")
		return
	}
	s.logAudit(r, "project.admin_self_grant", "project", project, map[string]any{"role": "owner"})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
