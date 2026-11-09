package server

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/store"
)

// avatarTestStore follows the integration-test convention: skipped unless
// QUILL_TEST_DATABASE_URL is set so `go test ./...` stays green CI-side.
func avatarTestServer(t *testing.T) *Server {
	t.Helper()
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUILL_TEST_DATABASE_URL not set; skipping avatar test")
	}
	if err := store.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	reset := func() {
		_, _ = st.Pool().Exec(context.Background(), "DELETE FROM users")
		_, _ = st.Pool().Exec(context.Background(), "DELETE FROM tenants WHERE slug <> 'default'")
	}
	reset()
	t.Cleanup(reset)
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return New(cfg, logging.New("error", "text"), st)
}

// registerAccount creates a fresh local-auth account through the public API
// and returns its bearer token + user id.
func registerAccount(t *testing.T, srv *Server) (token, id string) {
	t.Helper()
	body := `{"username":"avatest","email":"avatest@example.test","displayName":"Avatar Test","password":"correct-horse-battery"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/register", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK && rec.Code != http.StatusCreated {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Token string `json:"token"`
		User  struct {
			ID string `json:"id"`
		} `json:"user"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("parse register response: %v", err)
	}
	return out.Token, out.User.ID
}

func doAuthed(t *testing.T, srv *Server, method, path, token string, body []byte, contentType string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		rdr = bytes.NewReader(body)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req := httptest.NewRequest(method, path, rdr)
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	return rec
}

func TestAvatarUploadGetRemove(t *testing.T) {
	srv := avatarTestServer(t)
	token, userID := registerAccount(t, srv)

	// A fake-but-valid PNG payload (header bytes are enough — the API checks
	// content type + size, not image validity).
	png := append([]byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}, bytes.Repeat([]byte("a"), 16)...)

	// Before upload: /me has no hasAvatar, the public GET 404s.
	rec := doAuthed(t, srv, http.MethodGet, "/api/v1/auth/me", token, nil, "")
	var me map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("parse me: %v", err)
	}
	if has, _ := me["hasAvatar"].(bool); has {
		t.Fatalf("hasAvatar before upload = true")
	}
	rec = doAuthed(t, srv, http.MethodGet, "/api/v1/users/"+userID+"/avatar", "", nil, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("avatar before upload: %d, want 404", rec.Code)
	}

	// Upload.
	rec = doAuthed(t, srv, http.MethodPut, "/api/v1/me/avatar", token, png, "image/png")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}

	// Wrong content type rejected.
	rec = doAuthed(t, srv, http.MethodPut, "/api/v1/me/avatar", token, png, "image/svg+xml")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("svg upload: %d, want 400", rec.Code)
	}

	// Oversized payload rejected with 413.
	tooBig := bytes.Repeat([]byte{0}, avatarMaxBytes+1)
	rec = doAuthed(t, srv, http.MethodPut, "/api/v1/me/avatar", token, tooBig, "image/png")
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("oversized upload: %d, want 413", rec.Code)
	}

	// /me now reports the avatar.
	rec = doAuthed(t, srv, http.MethodGet, "/api/v1/auth/me", token, nil, "")
	me = map[string]any{}
	if err := json.Unmarshal(rec.Body.Bytes(), &me); err != nil {
		t.Fatalf("parse me: %v", err)
	}
	if has, _ := me["hasAvatar"].(bool); !has {
		t.Fatalf("hasAvatar after upload = false")
	}

	// Public GET serves the bytes with caching headers.
	rec = doAuthed(t, srv, http.MethodGet, "/api/v1/users/"+userID+"/avatar", "", nil, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("avatar GET: %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("avatar content-type: %q", ct)
	}
	if !bytes.Equal(rec.Body.Bytes(), png) {
		t.Fatalf("avatar bytes roundtrip mismatch")
	}
	etag := rec.Header().Get("ETag")
	if etag == "" {
		t.Fatalf("avatar GET missing ETag")
	}

	// ETag revalidation.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/users/"+userID+"/avatar", nil)
	req.Header.Set("If-None-Match", etag)
	rec = httptest.NewRecorder()
	srv.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotModified {
		t.Fatalf("avatar revalidation: %d, want 304", rec.Code)
	}

	// Unknown user id: 404 (no existence leak — same status either way).
	rec = doAuthed(t, srv, http.MethodGet, "/api/v1/users/00000000-0000-0000-0000-000000000000/avatar", "", nil, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("avatar unknown user: %d, want 404", rec.Code)
	}

	// Remove, then it's gone.
	rec = doAuthed(t, srv, http.MethodDelete, "/api/v1/me/avatar", token, nil, "")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("avatar delete: %d", rec.Code)
	}
	rec = doAuthed(t, srv, http.MethodGet, "/api/v1/users/"+userID+"/avatar", "", nil, "")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("avatar after delete: %d, want 404", rec.Code)
	}
}
