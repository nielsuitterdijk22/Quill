package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
)

type billingResponse struct {
	Plan   string         `json:"plan"`
	Usage  billingUsage   `json:"usage"`
	Limits *billingLimits `json:"limits,omitempty"`
}

type billingUsage struct {
	Repos           int64   `json:"repos"`
	StorageBytes    int64   `json:"storageBytes"`
	PipelineMinutes float64 `json:"pipelineMinutes"`
}

type billingLimits struct {
	Repos           *int64   `json:"repos,omitempty"`
	StorageBytes    *int64   `json:"storageBytes,omitempty"`
	PipelineMinutes *float64 `json:"pipelineMinutes,omitempty"`
}

type setPlanRequest struct {
	Plan string `json:"plan"`
}

// handleBillingGet returns the tenant's plan and metered usage (tenant admins
// and platform admins only).
func handleBillingGet(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		view, err := s.platform.GetBilling(r.Context(), actor, chi.URLParam(r, "slug"))
		if err != nil {
			s.writePlatformError(w, err, "could not load billing")
			return
		}
		resp := billingResponse{
			Plan:  view.Plan,
			Usage: billingUsage(view.Usage),
		}
		if view.Limits != nil {
			resp.Limits = &billingLimits{
				Repos:           view.Limits.Repos,
				StorageBytes:    view.Limits.StorageBytes,
				PipelineMinutes: view.Limits.PipelineMinutes,
			}
		}
		httpx.JSON(w, http.StatusOK, resp)
	}
}

// handleBillingSetPlan changes the tenant's plan (platform admin only).
func handleBillingSetPlan(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		var req setPlanRequest
		if !decodeJSON(w, r, &req) {
			return
		}
		orgSlug := chi.URLParam(r, "slug")
		tenant, err := s.platform.SetTenantPlan(r.Context(), actor, orgSlug, req.Plan)
		if err != nil {
			s.writePlatformError(w, err, "could not set plan")
			return
		}
		s.logAudit(r, "billing.plan_changed", "tenant", tenant.ID.String(), map[string]any{
			"org": orgSlug, "plan": req.Plan,
		})
		httpx.JSON(w, http.StatusOK, map[string]any{"plan": tenant.Plan})
	}
}
