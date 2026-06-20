package server

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/nielsuitterdijk22/quill/internal/httpx"
)

type addSSHKeyRequest struct {
	Title string `json:"title"`
	Key   string `json:"key"`
}

type sshKeyResponse struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Key         string `json:"key"`
	Fingerprint string `json:"fingerprint"`
}

// handleListSSHKeys returns the SSH public keys registered for the signed-in user.
func (s *Server) handleListSSHKeys(w http.ResponseWriter, r *http.Request) {
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
	if !user.ForgejoUsername.Valid || user.ForgejoUsername.String == "" {
		httpx.JSON(w, http.StatusOK, map[string]any{"keys": []sshKeyResponse{}})
		return
	}
	keys, err := s.forgejo.ListSSHKeys(r.Context(), user.ForgejoUsername.String)
	if err != nil {
		s.logger.Error("list ssh keys failed", "error", err)
		httpx.Error(w, http.StatusBadGateway, "git_unavailable", "could not fetch SSH keys from git backend")
		return
	}
	out := make([]sshKeyResponse, len(keys))
	for i, k := range keys {
		out[i] = sshKeyResponse{ID: k.ID, Title: k.Title, Key: k.Key, Fingerprint: k.Fingerprint}
	}
	httpx.JSON(w, http.StatusOK, map[string]any{"keys": out})
}

// handleAddSSHKey adds a public SSH key to the signed-in user's Forgejo account.
func (s *Server) handleAddSSHKey(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<16)
	var req addSSHKeyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_body", "request body must be valid JSON")
		return
	}
	if req.Title == "" || req.Key == "" {
		httpx.Error(w, http.StatusBadRequest, "invalid_input", "title and key are required")
		return
	}
	user, err := s.auth.CurrentUser(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not load user")
		return
	}
	if !user.ForgejoUsername.Valid || user.ForgejoUsername.String == "" {
		httpx.Error(w, http.StatusBadGateway, "git_unavailable", "git account not provisioned yet")
		return
	}
	key, err := s.forgejo.AddSSHKey(r.Context(), user.ForgejoUsername.String, req.Title, req.Key)
	if err != nil {
		s.logger.Error("add ssh key failed", "error", err)
		httpx.Error(w, http.StatusBadGateway, "git_unavailable", "could not add SSH key to git backend")
		return
	}
	httpx.JSON(w, http.StatusCreated, sshKeyResponse{
		ID: key.ID, Title: key.Title, Key: key.Key, Fingerprint: key.Fingerprint,
	})
}

// handleDeleteSSHKey removes an SSH key (by Forgejo key ID) from the signed-in
// user's Forgejo account.
func (s *Server) handleDeleteSSHKey(w http.ResponseWriter, r *http.Request) {
	id, ok := identityFrom(r.Context())
	if !ok {
		httpx.Error(w, http.StatusUnauthorized, "unauthorized", "authentication required")
		return
	}
	keyID, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil {
		httpx.Error(w, http.StatusBadRequest, "invalid_id", "key id must be an integer")
		return
	}
	user, err := s.auth.CurrentUser(r.Context(), id)
	if err != nil {
		httpx.Error(w, http.StatusInternalServerError, "internal", "could not load user")
		return
	}
	if !user.ForgejoUsername.Valid || user.ForgejoUsername.String == "" {
		httpx.Error(w, http.StatusBadGateway, "git_unavailable", "git account not provisioned yet")
		return
	}
	// Forgejo scopes this deletion to user.ForgejoUsername.String, so a
	// caller supplying a key ID that belongs to a different user gets a 404.
	if err := s.forgejo.DeleteSSHKey(r.Context(), user.ForgejoUsername.String, keyID); err != nil {
		s.logger.Error("delete ssh key failed", "keyID", keyID, "error", err)
		httpx.Error(w, http.StatusBadGateway, "git_unavailable", "could not delete SSH key from git backend")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
