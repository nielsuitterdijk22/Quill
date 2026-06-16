package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

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

// handleLogout is a no-op for stateless tokens; the frontend clears its cookie.
// It exists so clients have a uniform endpoint to call.
func (s *Server) handleLogout(w http.ResponseWriter, _ *http.Request) {
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
