package server

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

type billingResponse struct {
	Plan        string         `json:"plan"`
	Usage       billingUsage   `json:"usage"`
	Limits      *billingLimits `json:"limits,omitempty"`
	Locked      bool           `json:"locked"`
	Seats       int            `json:"seats"`
	BilledSeats int            `json:"billedSeats"`
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
			Plan:        view.Plan,
			Usage:       billingUsage(view.Usage),
			Locked:      view.Locked,
			Seats:       view.Seats,
			BilledSeats: view.BilledSeats,
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

// handleBillingCheckout starts a Creem checkout session for the org's Team Pro
// subscription. Returns the hosted-checkout URL the browser should be sent
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
		return fmt.Sprintf("%s/orgs/%s/settings/billing?upgraded=1", cfg.CORSAllowedOrigins[0], orgSlug)
	}
	return fmt.Sprintf("/orgs/%s/settings/billing?upgraded=1", orgSlug)
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
	res, err := s.platform.ProcessCreemWebhook(r.Context(), raw, r.Header.Get("creem-signature"))
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
	if res.TenantSlug != "" {
		action := "billing.plan_lapsed_creem"
		if res.Upgraded {
			action = "billing.plan_upgraded_creem"
		}
		s.logAudit(r, action, "org", res.TenantSlug, map[string]any{
			"org": res.TenantSlug, "plan": res.Plan,
		})
	}
	w.WriteHeader(http.StatusOK)
}

// handleMyBilling returns the caller's personal account billing view (plan,
// usage, limits) — the meters behind the settings page's billing panel.
func (s *Server) handleMyBilling(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	view, err := s.platform.GetPersonalBilling(r.Context(), actor)
	if err != nil {
		s.writePlatformError(w, err, "could not load billing")
		return
	}
	resp := billingResponse{
		Plan:        view.Plan,
		Usage:       billingUsage(view.Usage),
		Locked:      view.Locked,
		Seats:       view.Seats,
		BilledSeats: view.BilledSeats,
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

// handleMyBillingCheckout starts a Creem checkout session for the caller's
// personal account (Starter or Pro). 501 when payments aren't
// configured mirrors the org flow.
func (s *Server) handleMyBillingCheckout(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	// Body is optional ({"plan":"starter"|"pro"}); an empty body means Pro.
	var req struct {
		Plan string `json:"plan"`
	}
	if r.ContentLength != 0 && !decodeJSON(w, r, &req) {
		return
	}
	if req.Plan == "" {
		req.Plan = platform.PlanPro
	}
	successURL := "/settings?upgraded=1"
	if len(s.cfg.CORSAllowedOrigins) > 0 && s.cfg.CORSAllowedOrigins[0] != "" {
		successURL = s.cfg.CORSAllowedOrigins[0] + successURL
	}
	requestID := fmt.Sprintf("user-%s-%d", actor.UserID, time.Now().Unix())
	url, err := s.platform.StartPersonalCheckout(r.Context(), actor, req.Plan, successURL, requestID)
	if err != nil {
		if errors.Is(err, platform.ErrPaymentsNotConfigured) {
			httpx.Error(w, http.StatusNotImplemented, "not_configured", "Card payments are not activated on this server yet — contact us to upgrade early.")
			return
		}
		s.writePlatformError(w, err, "could not start checkout")
		return
	}
	s.logAudit(r, "billing.personal_checkout_started", "user", actor.UserID.String(), map[string]any{
		"user": actor.UserID.String(),
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"url": url})
}

// handleRedeemBeta moves the caller's personal account to the beta plan using
// an invite code from a hand-issued link.
func (s *Server) handleRedeemBeta(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req struct {
		Code string `json:"code"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.platform.RedeemBetaInvite(r.Context(), actor, req.Code); err != nil {
		s.writePlatformError(w, err, "could not redeem beta invite")
		return
	}
	s.logAudit(r, "billing.beta_redeemed", "user", actor.UserID.String(), nil)
	httpx.JSON(w, http.StatusOK, map[string]any{"plan": platform.PlanBeta})
}

// handleCreateBetaInvite mints a beta invite (platform admin). The code and a
// ready-to-share link are returned once.
func (s *Server) handleCreateBetaInvite(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req struct {
		Note    string `json:"note"`
		MaxUses int    `json:"maxUses"`
		Days    int    `json:"days"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	inv, code, err := s.platform.CreateBetaInvite(r.Context(), actor, req.Note, req.MaxUses, req.Days)
	if err != nil {
		s.writePlatformError(w, err, "could not create beta invite")
		return
	}
	s.logAudit(r, "billing.beta_invite_created", "beta_invite", inv.ID.String(), map[string]any{
		"note": inv.Note, "maxUses": inv.MaxUses, "expiresAt": inv.ExpiresAt,
	})
	link := "/beta/" + code
	if len(s.cfg.CORSAllowedOrigins) > 0 && s.cfg.CORSAllowedOrigins[0] != "" {
		link = s.cfg.CORSAllowedOrigins[0] + link
	}
	httpx.JSON(w, http.StatusCreated, map[string]any{"invite": inv, "code": code, "link": link})
}

// handleListBetaInvites lists beta invites (platform admin).
func (s *Server) handleListBetaInvites(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	invs, err := s.platform.ListBetaInvites(r.Context(), actor)
	if err != nil {
		s.writePlatformError(w, err, "could not list beta invites")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"invites": invs})
}

// handleRevokeBetaInvite disables a beta invite (platform admin).
func (s *Server) handleRevokeBetaInvite(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_id", "invalid invite id")
		return
	}
	if err := s.platform.RevokeBetaInvite(r.Context(), actor, id); err != nil {
		s.writePlatformError(w, err, "could not revoke beta invite")
		return
	}
	s.logAudit(r, "billing.beta_invite_revoked", "beta_invite", id.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}
