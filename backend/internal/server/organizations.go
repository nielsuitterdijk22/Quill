package server

import (
	"net/http"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
)

// orgResponse is the public JSON shape for an organization (an org-kind tenant)
// paired with the caller's role in it.
type orgResponse struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
	Role string `json:"role"`
}

type createOrgRequest struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// handleListOrganizations returns the organizations the authenticated user
// belongs to, with their role, for nav and the org switcher.
func (s *Server) handleListOrganizations(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	orgs, err := s.platform.ListOrganizations(r.Context(), actor)
	if err != nil {
		s.logger.Error("list organizations failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not list organizations")
		return
	}
	out := make([]orgResponse, 0, len(orgs))
	for _, o := range orgs {
		out = append(out, orgResponse{Slug: o.Slug, Name: o.Name, Role: o.Role})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"organizations": out})
}

// handleCreateOrganization provisions an org tenant (with the creator as admin)
// and its first same-named project. It returns the org and the created project so
// the onboarding flow can continue into the import step.
func (s *Server) handleCreateOrganization(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createOrgRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	tenant, project, err := s.platform.CreateOrganization(r.Context(), actor, req.Slug, req.Name)
	if err != nil {
		s.writePlatformError(w, err, "could not create organization")
		return
	}
	s.logAudit(r, "org.created", "tenant", tenant.ID.String(), map[string]any{
		"slug": tenant.Slug,
		"name": tenant.Name,
	})
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"org":     orgResponse{Slug: tenant.Slug, Name: tenant.Name, Role: "admin"},
		"project": newProjectResponse(project),
	})
}
