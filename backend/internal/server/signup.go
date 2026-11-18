package server

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/zitadel"
)

type signupRequest struct {
	Username string `json:"username"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

// handleSignup registers a person: a handle, an email and a password, like
// GitHub — no first/last name. Organizations are created after sign-in. The
// account lives in Zitadel (which emails the verification code); Quill's own
// user row appears at first sign-in, with the handle as its username. Public
// and rate limited; answers the same for a taken username or email so the form
// cannot be used to probe which accounts exist.
func (s *Server) handleSignup(w http.ResponseWriter, r *http.Request) {
	if s.signup == nil || !s.signup.Enabled() {
		httpx.Error(w, http.StatusNotImplemented, "not_configured", "sign-up is not available on this server")
		return
	}
	var req signupRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	username := strings.ToLower(strings.TrimSpace(req.Username))
	email := strings.TrimSpace(req.Email)
	if !platform.ValidHandle(username) {
		httpx.Error(w, http.StatusBadRequest, "invalid_username", "Username: 1–63 lowercase letters, digits, '.', '_' or '-', starting with a letter or digit, and not a reserved word.")
		return
	}
	if addr, err := mail.ParseAddress(email); err != nil || addr.Address != email {
		httpx.Error(w, http.StatusBadRequest, "invalid_email", "Enter a valid email address.")
		return
	}
	if len(req.Password) < 8 || len(req.Password) > 128 {
		httpx.Error(w, http.StatusBadRequest, "invalid_password", "Password must be 8–128 characters.")
		return
	}
	if _, err := s.store.GetUserByUsername(r.Context(), username); err == nil {
		httpx.Error(w, http.StatusConflict, "taken", "That username or email is already in use.")
		return
	} else if !errors.Is(err, pgx.ErrNoRows) {
		s.logger.Error("signup: username lookup failed", "error", err)
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not create the account")
		return
	}
	if err := s.signup.CreateAccount(r.Context(), username, email, req.Password); err != nil {
		if errors.Is(err, zitadel.ErrConflict) {
			httpx.Error(w, http.StatusConflict, "taken", "That username or email is already in use.")
			return
		}
		// Zitadel's password-complexity policy surfaces as a 400 here.
		s.logger.Warn("signup: zitadel rejected the account", "error", err)
		httpx.Error(w, http.StatusBadRequest, "rejected", "Could not create the account. Check the password meets the requirements (length, upper/lower case, a digit).")
		return
	}
	s.logAudit(r, "auth.signup", "user", username, nil)
	httpx.JSON(w, http.StatusCreated, map[string]any{"username": username, "next": "verify_email"})
}
