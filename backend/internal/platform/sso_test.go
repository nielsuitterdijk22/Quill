package platform

import (
	"context"
	"errors"
	"testing"
)

func TestOrgSSOConfig(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()
	founder := Actor{UserID: scopeMakeUser(t, st, "founder")}
	if _, _, err := svc.CreateOrganization(ctx, founder, "acme", "Acme Inc"); err != nil {
		t.Fatalf("create org: %v", err)
	}

	// No config yet.
	view, err := svc.GetOrgSSO(ctx, founder, "acme")
	if err != nil {
		t.Fatalf("get sso: %v", err)
	}
	if view.Configured {
		t.Fatalf("expected unconfigured, got %+v", view)
	}

	// Save an enabled OIDC config with a secret.
	view, err = svc.SetOrgSSO(ctx, founder, "acme", SSOConfigInput{
		Protocol:     "oidc",
		Issuer:       "https://idp.acme.com",
		ClientID:     "quill",
		ClientSecret: "s3cr3t",
		EmailDomain:  "Acme.com",
		Enabled:      true,
	})
	if err != nil {
		t.Fatalf("set sso: %v", err)
	}
	if !view.Configured || !view.Enabled || !view.HasSecret || view.EmailDomain != "acme.com" {
		t.Fatalf("sso view after set: %+v", view)
	}

	// Reads never leak the secret, only that one is set. Update leaving the secret
	// blank preserves it.
	view, err = svc.SetOrgSSO(ctx, founder, "acme", SSOConfigInput{
		Protocol: "oidc", Issuer: "https://idp.acme.com", ClientID: "quill2", Enabled: true,
	})
	if err != nil {
		t.Fatalf("update sso: %v", err)
	}
	if !view.HasSecret || view.ClientID != "quill2" {
		t.Fatalf("secret should persist on blank update: %+v", view)
	}

	// Enabling without an issuer is rejected.
	if _, err := svc.SetOrgSSO(ctx, founder, "acme", SSOConfigInput{Protocol: "oidc", Enabled: true}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("enable without issuer: want ErrInvalidInput, got %v", err)
	}

	// A non-admin cannot read or write.
	stranger := Actor{UserID: scopeMakeUser(t, st, "stranger")}
	if _, err := svc.GetOrgSSO(ctx, stranger, "acme"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger get: want ErrForbidden, got %v", err)
	}

	// Delete removes it.
	if err := svc.DeleteOrgSSO(ctx, founder, "acme"); err != nil {
		t.Fatalf("delete sso: %v", err)
	}
	if v, _ := svc.GetOrgSSO(ctx, founder, "acme"); v.Configured {
		t.Fatalf("expected removed, got %+v", v)
	}
}
