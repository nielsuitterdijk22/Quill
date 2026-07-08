package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
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

// ---- members ---------------------------------------------------------------

type orgMemberResponse struct {
	UserID      string    `json:"userId"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	Role        string    `json:"role"`
	Since       time.Time `json:"since"`
}

type updateMemberRequest struct {
	Role string `json:"role"`
}

// handleListOrgMembers returns an organization's members (any member may view).
func (s *Server) handleListOrgMembers(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	members, err := s.platform.ListOrgMembers(r.Context(), actor, chi.URLParam(r, "slug"))
	if err != nil {
		s.writePlatformError(w, err, "could not list members")
		return
	}
	out := make([]orgMemberResponse, 0, len(members))
	for _, m := range members {
		out = append(out, orgMemberResponse{
			UserID:      m.UserID.String(),
			Username:    m.Username,
			Email:       m.Email,
			DisplayName: m.DisplayName,
			Role:        m.Role,
			Since:       m.Since,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"members": out})
}

// handleUpdateOrgMemberRole changes a member's role (admin only).
func (s *Server) handleUpdateOrgMemberRole(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid", "invalid user id")
		return
	}
	var req updateMemberRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.platform.UpdateOrgMemberRole(r.Context(), actor, chi.URLParam(r, "slug"), userID, req.Role); err != nil {
		s.writePlatformError(w, err, "could not update member")
		return
	}
	s.logAudit(r, "org.member.role_changed", "user", userID.String(), map[string]any{
		"org":  chi.URLParam(r, "slug"),
		"role": req.Role,
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleRemoveOrgMember removes a member from an organization (admin only).
func (s *Server) handleRemoveOrgMember(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	userID, err := uuid.Parse(chi.URLParam(r, "userId"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid", "invalid user id")
		return
	}
	if err := s.platform.RemoveOrgMember(r.Context(), actor, chi.URLParam(r, "slug"), userID); err != nil {
		s.writePlatformError(w, err, "could not remove member")
		return
	}
	s.logAudit(r, "org.member.removed", "user", userID.String(), map[string]any{
		"org": chi.URLParam(r, "slug"),
	})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// ---- invites ---------------------------------------------------------------

type inviteResponse struct {
	ID        string    `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	ExpiresAt time.Time `json:"expiresAt"`
	CreatedAt time.Time `json:"createdAt"`
}

type createInviteRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// handleListInvites returns an organization's pending invitations (admin only).
func (s *Server) handleListInvites(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	invites, err := s.platform.ListInvites(r.Context(), actor, chi.URLParam(r, "slug"))
	if err != nil {
		s.writePlatformError(w, err, "could not list invites")
		return
	}
	out := make([]inviteResponse, 0, len(invites))
	for _, iv := range invites {
		out = append(out, inviteResponse{
			ID:        iv.ID.String(),
			Email:     iv.Email,
			Role:      iv.Role,
			ExpiresAt: iv.ExpiresAt,
			CreatedAt: iv.CreatedAt,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"invites": out})
}

// handleCreateInvite invites a person by email (admin only). It returns the
// invite plus a one-time accept token; the browser builds the shareable link.
func (s *Server) handleCreateInvite(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req createInviteRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	res, err := s.platform.CreateInvite(r.Context(), actor, chi.URLParam(r, "slug"), req.Email, req.Role)
	if err != nil {
		s.writePlatformError(w, err, "could not create invite")
		return
	}
	s.logAudit(r, "org.invite.created", "invite", res.Invite.ID.String(), map[string]any{
		"org":   chi.URLParam(r, "slug"),
		"email": res.Invite.Email,
		"role":  res.Invite.Role,
	})
	httpx.JSON(w, http.StatusCreated, map[string]any{
		"invite": inviteResponse{
			ID:        res.Invite.ID.String(),
			Email:     res.Invite.Email,
			Role:      res.Invite.Role,
			ExpiresAt: res.Invite.ExpiresAt,
			CreatedAt: res.Invite.CreatedAt,
		},
		"token":        res.Token,
		"emailedByIdp": res.EmailedByIdP,
	})
}

// handleRevokeInvite cancels a pending invitation (admin only).
func (s *Server) handleRevokeInvite(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	inviteID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid", "invalid invite id")
		return
	}
	if err := s.platform.RevokeInvite(r.Context(), actor, chi.URLParam(r, "slug"), inviteID); err != nil {
		s.writePlatformError(w, err, "could not revoke invite")
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleAcceptInvite adds the authenticated user to the invited organization. The
// token in the path is the bearer secret. Returns the org slug for redirect.
func (s *Server) handleAcceptInvite(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	slug, err := s.platform.AcceptInvite(r.Context(), actor, chi.URLParam(r, "token"))
	if err != nil {
		s.writePlatformError(w, err, "could not accept invite")
		return
	}
	s.logAudit(r, "org.invite.accepted", "tenant", slug, map[string]any{"org": slug})
	httpx.JSON(w, http.StatusOK, orgResponse{Slug: slug})
}

// ---- SSO -------------------------------------------------------------------

type ssoResponse struct {
	Configured  bool      `json:"configured"`
	Protocol    string    `json:"protocol"`
	Issuer      string    `json:"issuer"`
	ClientID    string    `json:"clientId"`
	EmailDomain string    `json:"emailDomain"`
	Enabled     bool      `json:"enabled"`
	HasSecret   bool      `json:"hasSecret"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

type setSSORequest struct {
	Protocol     string `json:"protocol"`
	Issuer       string `json:"issuer"`
	ClientID     string `json:"clientId"`
	ClientSecret string `json:"clientSecret"`
	EmailDomain  string `json:"emailDomain"`
	Enabled      bool   `json:"enabled"`
}

func newSSOResponse(v platform.SSOConfigView) ssoResponse {
	return ssoResponse{
		Configured:  v.Configured,
		Protocol:    v.Protocol,
		Issuer:      v.Issuer,
		ClientID:    v.ClientID,
		EmailDomain: v.EmailDomain,
		Enabled:     v.Enabled,
		HasSecret:   v.HasSecret,
		UpdatedAt:   v.UpdatedAt,
	}
}

// handleGetOrgSSO returns an organization's SSO configuration (admin only, never
// the secret).
func (s *Server) handleGetOrgSSO(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	view, err := s.platform.GetOrgSSO(r.Context(), actor, chi.URLParam(r, "slug"))
	if err != nil {
		s.writePlatformError(w, err, "could not load SSO settings")
		return
	}
	httpx.JSON(w, http.StatusOK, newSSOResponse(view))
}

// handleSetOrgSSO creates or updates an organization's SSO configuration (admin
// only). A blank clientSecret preserves the stored one.
func (s *Server) handleSetOrgSSO(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req setSSORequest
	if !decodeJSON(w, r, &req) {
		return
	}
	view, err := s.platform.SetOrgSSO(r.Context(), actor, chi.URLParam(r, "slug"), platform.SSOConfigInput{
		Protocol:     req.Protocol,
		Issuer:       req.Issuer,
		ClientID:     req.ClientID,
		ClientSecret: req.ClientSecret,
		EmailDomain:  req.EmailDomain,
		Enabled:      req.Enabled,
	})
	if err != nil {
		s.writePlatformError(w, err, "could not save SSO settings")
		return
	}
	s.logAudit(r, "org.sso.updated", "tenant", chi.URLParam(r, "slug"), map[string]any{
		"org":      chi.URLParam(r, "slug"),
		"protocol": view.Protocol,
		"enabled":  view.Enabled,
	})
	httpx.JSON(w, http.StatusOK, newSSOResponse(view))
}

// handleDeleteOrgSSO removes an organization's SSO configuration (admin only).
func (s *Server) handleDeleteOrgSSO(w http.ResponseWriter, r *http.Request) {
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if err := s.platform.DeleteOrgSSO(r.Context(), actor, chi.URLParam(r, "slug")); err != nil {
		s.writePlatformError(w, err, "could not remove SSO settings")
		return
	}
	s.logAudit(r, "org.sso.removed", "tenant", chi.URLParam(r, "slug"), map[string]any{"org": chi.URLParam(r, "slug")})
	httpx.JSON(w, http.StatusOK, map[string]any{"ok": true})
}
