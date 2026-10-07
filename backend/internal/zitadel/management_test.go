package zitadel

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientDisabledWhenUnconfigured(t *testing.T) {
	if NewClient("", "tok").Enabled() {
		t.Fatal("client with no issuer should be disabled")
	}
	if NewClient("https://auth.example.com", "").Enabled() {
		t.Fatal("client with no token should be disabled")
	}
	if !NewClient("https://auth.example.com/", "tok").Enabled() {
		t.Fatal("fully configured client should be enabled")
	}
}

func TestInviteUserScopesOrgAndProfile(t *testing.T) {
	var gotOrg, gotPath string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotOrg = r.Header.Get("x-zitadel-orgid")
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "sa-token")
	if err := c.InviteUser(context.Background(), "org-9", "jane@acme.com", "Jane Doe"); err != nil {
		t.Fatalf("invite: %v", err)
	}
	if gotOrg != "org-9" {
		t.Fatalf("org header: got %q", gotOrg)
	}
	if gotPath != "/management/v1/users/human" {
		t.Fatalf("path: got %q", gotPath)
	}
	if body["userName"] != "jane@acme.com" {
		t.Fatalf("userName: got %v", body["userName"])
	}
	profile, _ := body["profile"].(map[string]any)
	if profile["firstName"] != "Jane" || profile["lastName"] != "Doe" {
		t.Fatalf("profile split wrong: %v", profile)
	}
}

func TestInviteUserDerivesNameFromEmail(t *testing.T) {
	first, last := splitName("", "sam@acme.com")
	if first != "sam" || last != "(invited)" {
		t.Fatalf("name derivation: got %q / %q", first, last)
	}
}

func TestDoSurfacesHTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()
	c := NewClient(srv.URL, "sa-token")
	if err := c.InviteUser(context.Background(), "org-1", "x@acme.com", "X"); err == nil {
		t.Fatal("expected error on 403")
	}
}
