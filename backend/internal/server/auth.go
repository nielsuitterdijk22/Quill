package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/auth"
	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// maxAuthBody caps auth request bodies to a small size.
const maxAuthBody = 1 << 16 // 64 KiB

// userResponse is the public JSON shape for a Quill user. It omits internal and
// Forgejo-linkage fields.
type userResponse struct {
	ID          string    `json:"id"`
	Username    string    `json:"username"`
	Email       string    `json:"email"`
	DisplayName string    `json:"displayName"`
	IsAdmin     bool      `json:"isAdmin"`
	IsActive    bool      `json:"isActive"`
	CreatedAt   time.Time `json:"createdAt"`
}

func newUserResponse(u db.User) userResponse {
	return userResponse{
		ID:          u.ID.String(),
		Username:    u.Username,
		Email:       u.Email,
		DisplayName: u.DisplayName,
		IsAdmin:     u.IsAdmin,
		IsActive:    u.IsActive,
		CreatedAt:   u.CreatedAt,
	}
}

type registerRequest struct {
	Username    string `json:"username"`
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Password    string `json:"password"`
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// authResponse is returned by register and login: a token plus the user. The
// frontend stores the token in an httpOnly cookie; API clients use it as a bearer.
type authResponse struct {
	Token string       `json:"token"`
	User  userResponse `json:"user"`
}

// handleRegister creates a local account and returns a session token. The first
// user to register becomes an admin.
func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	id, err := s.auth.Register(r.Context(), auth.RegisterInput{
		Username:    req.Username,
		Email:       req.Email,
		DisplayName: req.DisplayName,
		Password:    req.Password,
	})
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidInput):
			httpx.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
		case errors.Is(err, auth.ErrUserExists):
			httpx.Error(w, http.StatusConflict, "user_exists", "a user with that username or email already exists")
		default:
			s.logger.Error("register failed", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "internal", "could not create account")
		}
		return
	}

	token, _, err := s.auth.Tokens().Issue(id)
	if err != nil {
		s.logger.Error("issue token failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not issue token")
		return
	}

	user, err := s.auth.CurrentUser(r.Context(), id)
	if err != nil {
		s.logger.Error("load user after register failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "account created but could not be loaded")
		return
	}
	httpx.JSON(w, http.StatusCreated, authResponse{Token: token, User: newUserResponse(user)})
}

// handleLogin authenticates credentials and returns a session token.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	token, id, err := s.auth.Login(r.Context(), auth.Credentials{
		Username: req.Username,
		Password: req.Password,
	})
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			s.logAudit(r, "auth.sign_in_failed", "user", req.Username, map[string]any{"username": req.Username})
			httpx.Error(w, http.StatusUnauthorized, "invalid_credentials", "incorrect username or password")
			return
		}
		s.logger.Error("login failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not sign in")
		return
	}

	user, err := s.auth.CurrentUser(r.Context(), id)
	if err != nil {
		s.logger.Error("load user after login failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "signed in but could not load user")
		return
	}
	s.logAudit(r, "auth.sign_in", "user", id.UserID.String(), map[string]any{"username": id.Username})
	httpx.JSON(w, http.StatusOK, authResponse{Token: token, User: newUserResponse(user)})
}

// handleMe returns the authenticated user (requireAuth must run first).
func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	user, err := s.auth.CurrentUser(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "user no longer exists")
		return
	}
	httpx.JSON(w, http.StatusOK, newUserResponse(user))
}

// handleMyContributions proxies the signed-in user's Forgejo contribution
// heatmap (the GitHub-style commit calendar). Returns an empty series rather
// than an error when Forgejo is disabled or the user isn't linked yet, so the
// profile renders an empty graph instead of failing.
func (s *Server) handleMyContributions(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	empty := map[string]any{"contributions": []any{}}
	if s.forgejo == nil || !s.forgejo.Enabled() {
		httpx.JSON(w, http.StatusOK, empty)
		return
	}
	user, err := s.store.GetUserByID(r.Context(), id.UserID)
	if err != nil || !user.ForgejoUsername.Valid || user.ForgejoUsername.String == "" {
		httpx.JSON(w, http.StatusOK, empty)
		return
	}

	entries, err := s.forgejo.UserHeatmap(r.Context(), user.ForgejoUsername.String)
	if err != nil {
		s.logger.Warn("fetch contribution heatmap failed", "user", user.ForgejoUsername.String, "error", err)
		httpx.JSON(w, http.StatusOK, empty)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"contributions": entries})
}

type updateProfileRequest struct {
	DisplayName string `json:"displayName"`
}

// handleUpdateProfile saves the signed-in user's editable profile fields and
// returns the refreshed user (requireAuth must run first).
func (s *Server) handleUpdateProfile(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req updateProfileRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	user, err := s.auth.UpdateProfile(r.Context(), id, req.DisplayName)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidInput) {
			httpx.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
			return
		}
		s.logger.Error("update profile failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not update profile")
		return
	}
	httpx.JSON(w, http.StatusOK, newUserResponse(user))
}

type adminResetPasswordRequest struct {
	NewPassword string `json:"newPassword"`
}

// handleAdminResetPassword lets a platform admin set any user's password without
// knowing the current one. Returns 204 on success.
func (s *Server) handleAdminResetPassword(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	var req adminResetPasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.auth.AdminResetPassword(r.Context(), username, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidInput):
			httpx.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
		case errors.Is(err, auth.ErrInvalidCredentials):
			httpx.Error(w, http.StatusNotFound, "not_found", "user not found")
		default:
			s.logger.Error("admin reset password failed", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "internal", "could not reset password")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type updateEmailRequest struct {
	Email string `json:"email"`
}

// handleUpdateEmail replaces the signed-in user's email address.
func (s *Server) handleUpdateEmail(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req updateEmailRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	user, err := s.auth.UpdateEmail(r.Context(), id, req.Email)
	if err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidInput):
			httpx.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
		case errors.Is(err, auth.ErrUserExists):
			httpx.Error(w, http.StatusConflict, "email_taken", "that email address is already in use")
		default:
			s.logger.Error("update email failed", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "internal", "could not update email")
		}
		return
	}
	httpx.JSON(w, http.StatusOK, newUserResponse(user))
}

type changePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// handleChangePassword lets the signed-in user update their local password after
// verifying the current one. Returns 204 on success.
func (s *Server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req changePasswordRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.auth.ChangePassword(r.Context(), id, req.CurrentPassword, req.NewPassword); err != nil {
		switch {
		case errors.Is(err, auth.ErrInvalidInput):
			httpx.Error(w, http.StatusBadRequest, "invalid_input", err.Error())
		case errors.Is(err, auth.ErrInvalidCredentials):
			httpx.Error(w, http.StatusBadRequest, "wrong_password", "current password is incorrect")
		default:
			s.logger.Error("change password failed", "error", err)
			httpx.Error(w, http.StatusInternalServerError, "internal", "could not update password")
		}
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleExportMyData returns a JSON export of the signed-in user's personal data
// (GDPR Article 20 portability). The response is suitable for download.
func (s *Server) handleExportMyData(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	user, err := s.auth.CurrentUser(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not load user")
		return
	}

	tokens, _ := s.store.ListGitTokensByUser(r.Context(), id.UserID)
	type tokenExport struct {
		Name      string    `json:"name"`
		CreatedAt time.Time `json:"createdAt"`
	}
	tokenExports := make([]tokenExport, len(tokens))
	for i, t := range tokens {
		tokenExports[i] = tokenExport{Name: t.Name, CreatedAt: t.CreatedAt}
	}

	projects, _ := s.store.ListProjectsByUser(r.Context(), id.UserID)
	type projectExport struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
		Role string `json:"role"`
	}
	projectExports := make([]projectExport, len(projects))
	for i, p := range projects {
		projectExports[i] = projectExport{Name: p.Name, Slug: p.Slug, Role: p.MemberRole}
	}

	export := map[string]any{
		"exportedAt": time.Now().UTC(),
		"profile": map[string]any{
			"username":    user.Username,
			"email":       user.Email,
			"displayName": user.DisplayName,
			"createdAt":   user.CreatedAt,
		},
		"gitTokens":          tokenExports,
		"projectMemberships": projectExports,
	}

	w.Header().Set("Content-Disposition", `attachment; filename="quill-export.json"`)
	httpx.JSON(w, http.StatusOK, export)
}

// handleDeleteAccount purges the signed-in user's account (GDPR erasure). The
// response clears the session cookie so the client returns to the login page.
func (s *Server) handleDeleteAccount(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}

	// Capture the Clerk subject before deletion: the cascade removes the
	// auth_identities rows, so we must read it first. Deleting the Clerk-side
	// user is what invalidates the session and prevents the deleted account from
	// being re-provisioned on the next request (which otherwise loops /sign-in).
	clerkSubject := ""
	if s.clerk != nil && s.clerk.Enabled() {
		if idents, err := s.store.ListAuthIdentitiesForUser(r.Context(), id.UserID); err == nil {
			for _, ai := range idents {
				if ai.Provider == auth.ProviderClerk {
					clerkSubject = ai.Subject
					break
				}
			}
		}
	}

	// Purge the user's solely-owned projects (repos, pipelines, Forgejo orgs)
	// BEFORE deleting the user, while their memberships still resolve the project
	// list. Otherwise the project/repo rows survive the cascade and a later
	// re-signup hits "repository already exists" on import. Best-effort: failures
	// are logged inside the purge and must not block account deletion.
	if err := s.platform.PurgeOwnedProjects(r.Context(), id.UserID); err != nil {
		s.logger.Warn("purge owned projects failed during account deletion",
			"username", id.Username, "error", err)
	}

	if err := s.auth.DeleteAccount(r.Context(), id); err != nil {
		s.logger.Error("delete account failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not delete account")
		return
	}

	if clerkSubject != "" {
		if err := s.clerk.DeleteUser(r.Context(), clerkSubject); err != nil {
			// Non-fatal: the Quill account is already gone. Log so an orphaned
			// Clerk user can be cleaned up, but still report success to the client.
			s.logger.Warn("could not delete Clerk user on account deletion",
				"username", id.Username, "error", err)
		}
	}

	s.logger.Info("account deleted", "username", id.Username)
	w.WriteHeader(http.StatusNoContent)
}

// handleLogout is a no-op for stateless tokens; the frontend clears its cookie.
// It exists so clients have a uniform endpoint to call.
func (s *Server) handleLogout(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusNoContent)
}

// handleListUsers returns all users (admin only).
func (s *Server) handleListUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.store.ListUsers(r.Context(), db.ListUsersParams{Limit: 500, Offset: 0})
	if err != nil {
		s.logger.Error("list users failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not list users")
		return
	}
	out := make([]userResponse, len(users))
	for i, u := range users {
		out[i] = newUserResponse(u)
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"users": out})
}

type setUserActiveRequest struct {
	Active bool `json:"active"`
}

// handleSetUserActive enables or disables a user account (admin only).
func (s *Server) handleSetUserActive(w http.ResponseWriter, r *http.Request) {
	username := chi.URLParam(r, "username")
	var req setUserActiveRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if err := s.store.SetUserActive(r.Context(), username, req.Active); err != nil {
		s.logger.Error("set user active failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not update user")
		return
	}
	action := "admin.user_deactivated"
	if req.Active {
		action = "admin.user_activated"
	}
	s.logAudit(r, action, "user", username, map[string]any{"username": username})
	w.WriteHeader(http.StatusNoContent)
}

// decodeJSON strictly decodes a size-limited JSON body. It writes a 400 and
// returns false on any error.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, maxAuthBody)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return false
	}
	return true
}
