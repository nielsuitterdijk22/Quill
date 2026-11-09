package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
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

// handleBillingCheckout starts a Creem checkout session for upgrading the org
// to the Team plan. Returns the hosted-checkout URL the browser should be sent
// to; when payments are not configured, a truthful 501 lets the UI say so.
func handleBillingCheckout(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		actor, ok := actorFrom(r.Context())
		if !ok {
			httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
			return
		}
		orgSlug := chi.URLParam(r, "slug")
		successURL := orgBillingURL(s.cfg, orgSlug)
		requestID := fmt.Sprintf("%s-%d", orgSlug, time.Now().Unix())
		url, err := s.platform.StartTeamCheckout(r.Context(), actor, orgSlug, successURL, requestID)
		if err != nil {
			if errors.Is(err, platform.ErrPaymentsNotConfigured) {
				httpx.Error(w, http.StatusNotImplemented, "not_configured", "Card payments are not activated on this server yet — contact us to upgrade early.")
				return
			}
			s.writePlatformError(w, err, "could not start checkout")
			return
		}
		s.logAudit(r, "billing.checkout_started", "org", orgSlug, map[string]any{
			"org": orgSlug,
		})
		httpx.JSON(w, http.StatusOK, map[string]any{"url": url})
	}
}

// orgBillingURL builds the post-payment success URL: the org's settings page
// (where the billing panel lives) with a flag the UI can react to.
func orgBillingURL(cfg *config.Config, orgSlug string) string {
	if len(cfg.CORSAllowedOrigins) > 0 && cfg.CORSAllowedOrigins[0] != "" {
		return fmt.Sprintf("%s/orgs/%s/settings?upgraded=1", cfg.CORSAllowedOrigins[0], orgSlug)
	}
	return fmt.Sprintf("/orgs/%s/settings?upgraded=1", orgSlug)
}

// handleCreemWebhook applies verified Creem events (checkout.completed ->
// plan upgrade). Signature-verified; no Quill auth involved. Always 200 on
// ignored events so Creem doesn't retry; errors on tampered payloads.
func (s *Server) handleCreemWebhook(w http.ResponseWriter, r *http.Request) {
	body := http.MaxBytesReader(w, r.Body, 1<<20)
	raw, err := io.ReadAll(body)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_body", "could not read webhook body")
		return
	}
	orgSlug, err := s.platform.ProcessCreemWebhook(r.Context(), raw, r.Header.Get("creem-signature"))
	if err != nil {
		if errors.Is(err, platform.ErrPaymentsNotConfigured) {
			httpx.Error(w, http.StatusNotImplemented, "not_configured", "payments are not configured on this server")
			return
		}
		if errors.Is(err, platform.ErrInvalidInput) {
			httpx.Error(w, http.StatusBadRequest, "invalid_signature_or_payload", err.Error())
			return
		}
		// Unknown org / transient failure: 200 with a warning would silently
		// drop the event, so surface 5xx — Creem retries with backoff.
		s.logger.Error("creem webhook processing failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not process webhook")
		return
	}
	if orgSlug != "" {
		s.logAudit(r, "billing.plan_upgraded_creem", "org", orgSlug, map[string]any{
			"org": orgSlug,
		})
	}
	w.WriteHeader(http.StatusOK)
}
