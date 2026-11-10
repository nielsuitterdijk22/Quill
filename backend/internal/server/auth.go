package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

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
	// HasAvatar reports whether the user has an uploaded profile picture
	// (served at GET /users/{id}/avatar). Only filled where it matters (/me).
	HasAvatar bool `json:"hasAvatar,omitempty"`
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
	resp := newUserResponse(user)
	if _, err := s.store.GetUserAvatar(r.Context(), user.ID); err == nil {
		resp.HasAvatar = true
	}
	httpx.JSON(w, http.StatusOK, resp)
}

// avatarLimits constrain profile-picture uploads: content types served safely
// by <img> tags and a size a Postgres bytea column can carry without pain.
var avatarAllowedTypes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
	"image/gif":  true,
}

const avatarMaxBytes = 2 << 20 // 2 MiB

// handleUpdateAvatar stores (or replaces) the signed-in user's profile picture.
// The raw request body is the image bytes; the Content-Type header carries the
// media type. PUT /me/avatar.
func (s *Server) handleUpdateAvatar(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	mediaType := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]))
	if !avatarAllowedTypes[mediaType] {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "profile picture must be a PNG, JPEG, WebP or GIF image")
		return
	}
	body := http.MaxBytesReader(w, r.Body, avatarMaxBytes+1)
	data, err := io.ReadAll(body)
	if err != nil || len(data) == 0 || len(data) > avatarMaxBytes {
		httpx.Error(w, http.StatusRequestEntityTooLarge, "too_large", "profile picture must be at most 2 MiB")
		return
	}
	if err := s.store.UpsertUserAvatar(r.Context(), id.UserID, mediaType, data); err != nil {
		s.writePlatformError(w, err, "could not save the picture")
		return
	}
	s.logAudit(r, "profile.avatar_updated", "user", id.UserID.String(), map[string]any{
		"bytes": len(data), "type": mediaType,
	})
	w.WriteHeader(http.StatusNoContent)
}

// handleDeleteAvatar removes the signed-in user's profile picture. DELETE /me/avatar.
func (s *Server) handleDeleteAvatar(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	if err := s.store.DeleteUserAvatar(r.Context(), id.UserID); err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not delete the picture")
		return
	}
	s.logAudit(r, "profile.avatar_deleted", "user", id.UserID.String(), nil)
	w.WriteHeader(http.StatusNoContent)
}

// handleGetUserAvatar serves a user's profile picture publicly (so <img> tags
// render without a bearer) with a short cache. Unknown user or no picture: 404
// with no information leak. GET /users/{id}/avatar.
func (s *Server) handleGetUserAvatar(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "no such user")
		return
	}
	avatar, err := s.store.GetUserAvatar(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusNotFound, "not_found", "no such user")
		return
	}
	w.Header().Set("Content-Type", avatar.MediaType)
	w.Header().Set("Cache-Control", "public, max-age=60")
	etag := fmt.Sprintf(`"%d-%s"`, avatar.UpdatedAt.Unix(), id)
	w.Header().Set("ETag", etag)
	if match := r.Header.Get("If-None-Match"); match == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Length", strconv.Itoa(len(avatar.Data)))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(avatar.Data)
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
		httpx.JSON(w, http.StatusOK, map[string]any{
			"contributions": []any{},
			// TEMPORARY: surface the Forgego error so the cause is readable
			// from outside the container (no logs reachable via scw CLI).
			"_error": err.Error(),
		})
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

type updateUsernameRequest struct {
	Username string `json:"username"`
}

// handleUpdateUsername lets the signed-in user choose their handle during
// onboarding (allowed only before any project exists). It renames the backing
// Forgejo account too so the Quill username and git identity stay in sync
// (requireAuth must run first).
func (s *Server) handleUpdateUsername(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	var req updateUsernameRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	user, err := s.platform.SetUsername(r.Context(), id.UserID, req.Username)
	if err != nil {
		s.writePlatformError(w, err, "could not update username")
		return
	}
	s.logAudit(r, "auth.username_changed", "user", id.UserID.String(), map[string]any{"username": user.Username})
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

	// Capture the IdP subject before deletion: the cascade removes the
	// auth_identities rows, so we must read it first. Deleting the IdP-side user
	// is what invalidates the session and prevents the deleted account from being
	// re-provisioned on the next request (which otherwise loops /sign-in).
	idpSubject := ""
	if s.externalAuthEnabled() {
		if idents, err := s.store.ListAuthIdentitiesForUser(r.Context(), id.UserID); err == nil {
			for _, ai := range idents {
				if ai.Provider == s.verifier.Provider() {
					idpSubject = ai.Subject
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

	if idpSubject != "" {
		if err := s.verifier.DeleteUser(r.Context(), idpSubject); err != nil {
			// Non-fatal: the Quill account is already gone. Log so an orphaned
			// IdP-side user can be cleaned up, but still report success to the client.
			s.logger.Warn("could not delete IdP user on account deletion",
				"provider", s.verifier.Provider(), "username", id.Username, "error", err)
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
