package zitadel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestGenerateDomainValidation(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("x-zitadel-orgid") != "org-7" {
			t.Errorf("org header: got %q", r.Header.Get("x-zitadel-orgid"))
		}
		paths = append(paths, r.URL.Path)
		switch r.URL.Path {
		case "/management/v1/orgs/me/domains":
			// Registering the domain — already present (409, tolerated).
			http.Error(w, "exists", http.StatusConflict)
		case "/management/v1/orgs/me/domains/_search":
			// Attachment confirmation — the domain IS on the org.
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":[{"domain":"acme.com","isVerified":false}]}`))
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"token":"chal-123","url":"https://acme.com/.well-known/zitadel"}`))
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "sa-token")
	dv, err := c.GenerateDomainValidation(context.Background(), "org-7", "acme.com")
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	// It registers the domain, confirms it's attached, then generates the challenge.
	want := []string{
		"/management/v1/orgs/me/domains",
		"/management/v1/orgs/me/domains/_search",
		"/management/v1/orgs/me/domains/acme.com/_generate_validation",
	}
	if len(paths) != 3 || paths[0] != want[0] || paths[1] != want[1] || paths[2] != want[2] {
		t.Fatalf("request paths: got %v, want %v", paths, want)
	}
	if dv.Type != "dns" || dv.Token != "chal-123" || dv.RecordValue != "chal-123" {
		t.Fatalf("validation: %+v", dv)
	}
	if dv.RecordName != "_zitadel-challenge.acme.com" {
		t.Fatalf("record name: got %q", dv.RecordName)
	}
}

func TestGenerateDomainValidationReportsUnattachedDomain(t *testing.T) {
	// The domain registers (200) but the org's domain list doesn't contain it —
	// e.g. it's claimed by another org — so generation must fail with an actionable
	// error naming what the org actually has, not attempt _generate_validation.
	var hitGenerate bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/management/v1/orgs/me/domains":
			w.WriteHeader(http.StatusOK)
		case "/management/v1/orgs/me/domains/_search":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"result":[{"domain":"other.example","isVerified":true}]}`))
		default:
			hitGenerate = true
			http.Error(w, `{"code":5,"message":"Not Found"}`, http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "sa-token")
	_, err := c.GenerateDomainValidation(context.Background(), "org-7", "acme.com")
	if err == nil {
		t.Fatal("expected an error when the domain isn't attached to the org")
	}
	if hitGenerate {
		t.Fatal("should not call _generate_validation once attachment check fails")
	}
	// The message should name the domain and what the org actually has.
	if got := err.Error(); !strings.Contains(got, "acme.com") || !strings.Contains(got, "other.example") {
		t.Fatalf("unhelpful error: %v", got)
	}
}

func TestValidateDomainSurfacesFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/management/v1/orgs/me/domains/acme.com/_validation" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		http.Error(w, "not found", http.StatusPreconditionFailed)
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "sa-token")
	if err := c.ValidateDomain(context.Background(), "org-7", "acme.com"); err == nil {
		t.Fatal("expected error when the challenge isn't satisfied")
	}
}

func TestIsDomainVerifiedMatchesCaseInsensitively(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/management/v1/orgs/me/domains/_search" {
			t.Errorf("path: got %q", r.URL.Path)
		}
		w.WriteHeader(http.StatusOK)
		// Real Zitadel carries the name in `domainName`.
		_, _ = w.Write([]byte(`{"result":[{"domainName":"other.com","isVerified":false},{"domainName":"ACME.com","isVerified":true}]}`))
	}))
	defer srv.Close()

	c := NewClient(srv.URL, "sa-token")
	verified, err := c.IsDomainVerified(context.Background(), "org-7", "acme.com")
	if err != nil {
		t.Fatalf("is verified: %v", err)
	}
	if !verified {
		t.Fatal("expected acme.com to be reported verified (case-insensitive match)")
	}

	// A domain not present in the result set is simply unverified, not an error.
	missing, err := c.IsDomainVerified(context.Background(), "org-7", "absent.com")
	if err != nil {
		t.Fatalf("is verified (absent): %v", err)
	}
	if missing {
		t.Fatal("expected an absent domain to report unverified")
	}
}
