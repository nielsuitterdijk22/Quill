package platform_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// API tokens act as their user on the REST API; git tokens and API tokens
// never stand in for each other; revoking one stops it.
func TestAPITokens(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	uid := makeUser(t, st, "api-tok")
	act := platform.Actor{UserID: uid}

	api, err := svc.MintAPIToken(ctx, act, "agent")
	if err != nil {
		t.Fatal(err)
	}
	id, err := svc.AuthenticateAPIToken(ctx, api.Token)
	if err != nil || id.UserID != uid || id.Username != "api-tok" {
		t.Fatalf("authenticate: %+v %v", id, err)
	}

	git, err := svc.MintGitToken(ctx, act, "push")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := svc.AuthenticateAPIToken(ctx, git.Token); !errors.Is(err, platform.ErrAPITokenInvalid) {
		t.Fatalf("a git token authenticated the API: %v", err)
	}
	if _, err := svc.AuthenticateAPIToken(ctx, platform.APITokenPrefix+"forged"); !errors.Is(err, platform.ErrAPITokenInvalid) {
		t.Fatalf("unknown token: %v", err)
	}
	req, _ := http.NewRequest(http.MethodGet, "http://x/git/a/b/info/refs", nil)
	req.SetBasicAuth("api-tok", api.Token)
	if ga, err := svc.AuthenticateGitRequest(ctx, req); err == nil && ga.Authenticated {
		t.Fatal("an API token authenticated git")
	}

	if err := svc.RevokeGitToken(ctx, act, api.ID); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if _, err := svc.AuthenticateAPIToken(ctx, api.Token); !errors.Is(err, platform.ErrAPITokenInvalid) {
		t.Fatalf("revoked token still works: %v", err)
	}
}
