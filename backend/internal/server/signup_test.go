package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/logging"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/zitadel"
)

// TestSignup drives POST /api/v1/auth/signup against a fake Zitadel: the form
// takes only handle + email + password, validates them server-side, creates
// the Zitadel account with the handle as username (names mirror the handle),
// and answers a taken username or email identically.
func TestSignup(t *testing.T) {
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUILL_TEST_DATABASE_URL not set; skipping signup integration test")
	}
	if err := store.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)

	var got map[string]any
	status := http.StatusOK
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/users/human" || r.Header.Get("Authorization") != "Bearer tok" {
			t.Errorf("unexpected zitadel call %s %q", r.URL.Path, r.Header.Get("Authorization"))
		}
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{}`))
	}))
	t.Cleanup(fake.Close)

	cfg, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	srv := New(cfg, logging.New("error", "text"), st)
	calls := 0
	post := func(body string) int {
		calls++
		req := httptest.NewRequest(http.MethodPost, "/api/v1/auth/signup", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.RemoteAddr = fmt.Sprintf("203.0.113.%d:1234", calls) // one IP per call: the limiter is per IP
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec.Code
	}

	srv.signup = nil
	if c := post(`{"username":"sam","email":"sam@x.test","password":"correct-horse"}`); c != http.StatusNotImplemented {
		t.Fatalf("unconfigured: want 501, got %d", c)
	}
	srv.signup = zitadel.NewClient(fake.URL, "tok")

	for name, body := range map[string]string{
		"reserved":     `{"username":"orgs","email":"a@x.test","password":"correct-horse"}`,
		"bad chars":    `{"username":"sa m","email":"a@x.test","password":"correct-horse"}`,
		"trailing dot": `{"username":"sam.","email":"a@x.test","password":"correct-horse"}`,
		"bad email":    `{"username":"sam","email":"nope","password":"correct-horse"}`,
		"short pw":     `{"username":"sam","email":"a@x.test","password":"short"}`,
	} {
		if c := post(body); c != http.StatusBadRequest {
			t.Errorf("%s: want 400, got %d", name, c)
		}
	}
	if got != nil {
		t.Fatalf("invalid input reached zitadel: %v", got)
	}

	if c := post(`{"username":"Sam-Test","email":"sam@x.test","password":"correct-horse"}`); c != http.StatusCreated {
		t.Fatalf("valid sign-up: want 201, got %d", c)
	}
	profile, _ := got["profile"].(map[string]any)
	if got["username"] != "sam-test" || profile["givenName"] != "sam-test" || profile["familyName"] != "sam-test" {
		t.Fatalf("zitadel payload = %v", got)
	}
	if email, _ := got["email"].(map[string]any); email["email"] != "sam@x.test" || email["sendCode"] == nil {
		t.Fatalf("email payload = %v", got["email"])
	}

	status = http.StatusConflict
	if c := post(`{"username":"sam-test","email":"sam@x.test","password":"correct-horse"}`); c != http.StatusConflict {
		t.Fatalf("taken: want 409, got %d", c)
	}
}
