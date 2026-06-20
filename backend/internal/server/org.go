package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
)

// handleListOrgMembers returns the members of the caller's Clerk organisation.
// Only available when Clerk is enabled (non-nil clerk_org_id on the tenant).
func (s *Server) handleListOrgMembers(w http.ResponseWriter, r *http.Request) {
	clerkOrgID, ok := s.resolveClerkOrg(w, r)
	if !ok {
		return
	}
	members, err := s.clerk.ListOrgMembers(r.Context(), clerkOrgID)
	if err != nil {
		s.logger.Error("list org members failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not list org members")
		return
	}
	type memberResponse struct {
		MembershipID string `json:"membershipId"`
		ClerkUserID  string `json:"clerkUserId"`
		Email        string `json:"email"`
		DisplayName  string `json:"displayName"`
		Role         string `json:"role"`
	}
	out := make([]memberResponse, 0, len(members))
	for _, m := range members {
		out = append(out, memberResponse{
			MembershipID: m.MembershipID,
			ClerkUserID:  m.ClerkUserID,
			Email:        m.Email,
			DisplayName:  m.DisplayName,
			Role:         m.Role,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"members": out})
}

// handleListOrgInvitations returns pending invitations for the caller's org.
func (s *Server) handleListOrgInvitations(w http.ResponseWriter, r *http.Request) {
	clerkOrgID, ok := s.resolveClerkOrg(w, r)
	if !ok {
		return
	}
	invitations, err := s.clerk.ListOrgInvitations(r.Context(), clerkOrgID)
	if err != nil {
		s.logger.Error("list org invitations failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not list org invitations")
		return
	}
	type invitationResponse struct {
		InvitationID string `json:"invitationId"`
		Email        string `json:"email"`
		Role         string `json:"role"`
		Status       string `json:"status"`
	}
	out := make([]invitationResponse, 0, len(invitations))
	for _, inv := range invitations {
		out = append(out, invitationResponse{
			InvitationID: inv.InvitationID,
			Email:        inv.EmailAddress,
			Role:         inv.Role,
			Status:       inv.Status,
		})
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"invitations": out})
}

type inviteOrgMemberRequest struct {
	Email string `json:"email"`
	Role  string `json:"role"`
}

// handleInviteOrgMember sends a Clerk invitation email. The invited user joins
// the org automatically when they sign up or sign in via Clerk.
func (s *Server) handleInviteOrgMember(w http.ResponseWriter, r *http.Request) {
	clerkOrgID, ok := s.resolveClerkOrg(w, r)
	if !ok {
		return
	}
	var req inviteOrgMemberRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Email == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "email is required")
		return
	}
	role := req.Role
	if role == "" {
		role = "org:member"
	}
	if role != "org:member" && role != "org:admin" {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "role must be org:member or org:admin")
		return
	}
	if err := s.clerk.InviteOrgMember(r.Context(), clerkOrgID, req.Email, role); err != nil {
		s.logger.Error("invite org member failed", "email", req.Email, "error", err)
		httpx.Error(w, http.StatusBadGateway, "invite_failed", "could not send invitation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRemoveOrgMember removes a member from the caller's Clerk organisation
// by their membership ID.
func (s *Server) handleRemoveOrgMember(w http.ResponseWriter, r *http.Request) {
	clerkOrgID, ok := s.resolveClerkOrg(w, r)
	if !ok {
		return
	}
	membershipID := chi.URLParam(r, "membershipID")
	if _, err := uuid.Parse(membershipID); err != nil {
		// Clerk membership IDs are not UUIDs; just validate non-empty.
		if membershipID == "" {
			httpx.Error(w, http.StatusBadRequest, "invalid_input", "membership ID is required")
			return
		}
	}
	if err := s.clerk.RemoveOrgMember(r.Context(), clerkOrgID, membershipID); err != nil {
		s.logger.Error("remove org member failed", "membership", membershipID, "error", err)
		httpx.Error(w, http.StatusBadGateway, "remove_failed", "could not remove member")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRevokeOrgInvitation cancels a pending invitation by its invitation ID.
func (s *Server) handleRevokeOrgInvitation(w http.ResponseWriter, r *http.Request) {
	clerkOrgID, ok := s.resolveClerkOrg(w, r)
	if !ok {
		return
	}
	invitationID := chi.URLParam(r, "invitationID")
	if invitationID == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "invitation ID is required")
		return
	}
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if err := s.clerk.RevokeOrgInvitation(r.Context(), clerkOrgID, invitationID, id.UserID.String()); err != nil {
		s.logger.Error("revoke org invitation failed", "invitation", invitationID, "error", err)
		httpx.Error(w, http.StatusBadGateway, "revoke_failed", "could not revoke invitation")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// resolveClerkOrg looks up the caller's tenant and returns its Clerk org ID.
// Returns ("", false) and writes an error response if Clerk is not enabled,
// the actor has no tenant, or the tenant has no linked Clerk org.
func (s *Server) resolveClerkOrg(w http.ResponseWriter, r *http.Request) (string, bool) {
	if s.clerk == nil || !s.clerk.Enabled() {
		httpx.Error(w, http.StatusNotFound, "not_found", "org management requires Clerk authentication")
		return "", false
	}
	actor, ok := actorFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return "", false
	}
	if actor.TenantID == (actor.TenantID) && actor.TenantID.String() == "00000000-0000-0000-0000-000000000000" {
		httpx.Error(w, http.StatusBadRequest, "no_org", "your account is not part of an organisation")
		return "", false
	}
	tenant, err := s.store.GetTenantByID(r.Context(), actor.TenantID)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not load tenant")
		return "", false
	}
	if !tenant.ClerkOrgID.Valid || tenant.ClerkOrgID.String == "" {
		httpx.Error(w, http.StatusBadRequest, "no_org", "your account is not part of a Clerk organisation")
		return "", false
	}
	return tenant.ClerkOrgID.String, true
}
