package server

import (
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"unicode"

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
	if msg := passwordProblem(req.Password); msg != "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_password", msg)
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
	if err := s.signup.CreateAccount(r.Context(), username, email, req.Password, s.verifyEmailURL()); err != nil {
		if errors.Is(err, zitadel.ErrConflict) {
			httpx.Error(w, http.StatusConflict, "taken", "That username or email is already in use.")
			return
		}
		// Zitadel's password-complexity policy surfaces as a 400 here.
		s.logger.Warn("signup: zitadel rejected the account", "error", err)
		httpx.Error(w, http.StatusBadRequest, "rejected", "Could not create the account. Check the password meets the requirements (8+ characters with an uppercase letter, a lowercase letter, a number and a symbol).")
		return
	}
	s.logAudit(r, "auth.signup", "user", username, nil)
	httpx.JSON(w, http.StatusCreated, map[string]any{"username": username, "next": "verify_email"})
}

// passwordProblem mirrors Zitadel's password-complexity policy (min 8, upper,
// lower, number, symbol — the instance default) so a weak password is rejected
// with a precise message before we call Zitadel. frontend/app/lib/password.ts
// applies the same rules client-side.
func passwordProblem(pw string) string {
	if len(pw) < 8 || len(pw) > 128 {
		return "Password must be 8–128 characters."
	}
	var upper, lower, digit, symbol bool
	for _, r := range pw {
		switch {
		case unicode.IsUpper(r):
			upper = true
		case unicode.IsLower(r):
			lower = true
		case unicode.IsDigit(r):
			digit = true
		default:
			symbol = true
		}
	}
	if !upper || !lower || !digit || !symbol {
		return "Password needs an uppercase letter, a lowercase letter, a number and a symbol."
	}
	return ""
}

// verifyEmailURL is the Zitadel urlTemplate for the verification email: Quill's
// own /verify-email page on the app origin (first configured CORS origin).
func (s *Server) verifyEmailURL() string {
	if len(s.cfg.CORSAllowedOrigins) == 0 {
		return ""
	}
	return strings.TrimRight(s.cfg.CORSAllowedOrigins[0], "/") + "/verify-email?userID={{.UserID}}&code={{.Code}}"
}

type verifyEmailRequest struct {
	UserID string `json:"userId"`
	Code   string `json:"code"`
}

// handleVerifyEmail completes the emailed link: it confirms the address in
// Zitadel with the code. Public and rate limited; the code is the secret.
func (s *Server) handleVerifyEmail(w http.ResponseWriter, r *http.Request) {
	if s.signup == nil || !s.signup.Enabled() {
		httpx.Error(w, http.StatusNotImplemented, "not_configured", "email verification is not available on this server")
		return
	}
	var req verifyEmailRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.UserID, req.Code = strings.TrimSpace(req.UserID), strings.TrimSpace(req.Code)
	if req.UserID == "" || req.Code == "" || len(req.UserID) > 64 || len(req.Code) > 64 {
		httpx.Error(w, http.StatusBadRequest, "invalid", "This verification link is incomplete.")
		return
	}
	if err := s.signup.VerifyEmail(r.Context(), req.UserID, req.Code); err != nil {
		s.logger.Warn("verify-email: zitadel rejected the code", "error", err)
		httpx.Error(w, http.StatusBadRequest, "rejected", "This verification link is invalid or has expired. Sign in and request a new code.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
