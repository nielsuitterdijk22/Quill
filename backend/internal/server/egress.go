package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// Pipeline egress settings (2026-11-15): organization policy at
// /tenants/{tenant}/egress (and /me/egress for the caller's personal
// tenant), repository extras at /projects/{slug}/repos/{repo}/egress.

type setTenantEgressRequest struct {
	Mode string `json:"mode"`
	// Hosts null resets the allowlist to the defaults.
	Hosts *[]string `json:"hosts"`
}

type setRepoEgressRequest struct {
	Hosts []string `json:"hosts"`
}

func (s *Server) handleGetTenantEgress(w http.ResponseWriter, r *http.Request) {
	s.getTenantEgress(w, r, chi.URLParam(r, "tenant"))
}

func (s *Server) handleSetTenantEgress(w http.ResponseWriter, r *http.Request) {
	s.setTenantEgress(w, r, chi.URLParam(r, "tenant"))
}

func (s *Server) handleGetMyEgress(w http.ResponseWriter, r *http.Request) {
	if slug, ok := s.myTenantSlug(w, r); ok {
		s.getTenantEgress(w, r, slug)
	}
}

func (s *Server) handleSetMyEgress(w http.ResponseWriter, r *http.Request) {
	if slug, ok := s.myTenantSlug(w, r); ok {
		s.setTenantEgress(w, r, slug)
	}
}

func (s *Server) myTenantSlug(w http.ResponseWriter, r *http.Request) (string, bool) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return "", false
	}
	slug, err := s.platform.MyTenantSlug(r.Context(), actor)
	if err != nil {
		s.writePlatformError(w, err, "could not load your account")
		return "", false
	}
	return slug, true
}

func (s *Server) getTenantEgress(w http.ResponseWriter, r *http.Request, tenant string) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	v, err := s.platform.GetTenantEgress(r.Context(), actor, tenant)
	if err != nil {
		s.writePlatformError(w, err, "could not load the egress policy")
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

func (s *Server) setTenantEgress(w http.ResponseWriter, r *http.Request, tenant string) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req setTenantEgressRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	v, err := s.platform.SetTenantEgress(r.Context(), actor, tenant, platform.TenantEgressInput{Mode: req.Mode, Hosts: req.Hosts})
	if err != nil {
		s.writePlatformError(w, err, "could not save the egress policy")
		return
	}
	s.logAudit(r, "egress.updated", "tenant", tenant, map[string]any{
		"scope": "tenant", "tenant": tenant, "mode": v.Mode, "hosts": len(v.Hosts), "customized": v.Customized,
	})
	httpx.JSON(w, http.StatusOK, v)
}

func (s *Server) handleGetRepoEgress(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	v, err := s.platform.GetRepoEgress(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"))
	if err != nil {
		s.writePlatformError(w, err, "could not load the egress policy")
		return
	}
	httpx.JSON(w, http.StatusOK, v)
}

func (s *Server) handleSetRepoEgress(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req setRepoEgressRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	v, err := s.platform.SetRepoEgress(r.Context(), actor, chi.URLParam(r, "slug"), chi.URLParam(r, "repo"), req.Hosts)
	if err != nil {
		s.writePlatformError(w, err, "could not save the egress policy")
		return
	}
	s.logAudit(r, "egress.updated", "repository", chi.URLParam(r, "slug")+"/"+chi.URLParam(r, "repo"), map[string]any{
		"scope": "repository", "project": chi.URLParam(r, "slug"), "repo": chi.URLParam(r, "repo"), "hosts": len(v.Hosts),
	})
	httpx.JSON(w, http.StatusOK, v)
}
