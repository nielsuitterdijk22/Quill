package server

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// An API token is a bearer credential for the REST API (automation acting
// as its user); a git token is not, nor is a forged qat_ string.
func TestAPITokenBearer(t *testing.T) {
	srv, st := newDispatchTestServer(t)
	ctx := context.Background()
	name := fmt.Sprintf("apitok%d", time.Now().UnixNano()%1e9)
	user, err := st.CreateUser(ctx, db.CreateUserParams{Username: name, Email: name + "@example.com", DisplayName: name, IsActive: true})
	if err != nil {
		t.Fatal(err)
	}
	api, err := srv.platform.MintAPIToken(ctx, platform.Actor{UserID: user.ID}, "ci")
	if err != nil {
		t.Fatal(err)
	}
	git, err := srv.platform.MintGitToken(ctx, platform.Actor{UserID: user.ID}, "push")
	if err != nil {
		t.Fatal(err)
	}
	me := func(token string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/auth/me", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)
		return rec
	}
	if rec := me(api.Token); rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), name) {
		t.Fatalf("api token: %d %s", rec.Code, rec.Body.String())
	}
	if rec := me(git.Token); rec.Code != http.StatusUnauthorized {
		t.Fatalf("git token as bearer: %d", rec.Code)
	}
	if rec := me(platform.APITokenPrefix + "forged"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("forged token: %d", rec.Code)
	}
}
