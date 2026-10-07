package platform

import (
	"context"
	"errors"
	"testing"
)

// TestOrgSSOLink covers linking a Quill org to a hand-provisioned Zitadel org:
// platform-admin gating, the get/set/delete lifecycle, domain normalization, and
// the one-Zitadel-org-per-Quill-org guard. Quill performs no IdP provisioning, so
// there is nothing to mock — the link is pure metadata.
func TestOrgSSOLink(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()

	// A platform engineer (IsAdmin) both creates and links the org; a plain org
	// admin cannot touch SSO ("no self-service").
	admin := Actor{UserID: scopeMakeUser(t, st, "platformeng"), IsAdmin: true}
	if _, _, err := svc.CreateOrganization(ctx, admin, "acme", "Acme Inc"); err != nil {
		t.Fatalf("create org: %v", err)
	}

	// No link yet.
	view, err := svc.GetOrgSSO(ctx, admin, "acme")
	if err != nil {
		t.Fatalf("get sso: %v", err)
	}
	if view.Configured {
		t.Fatalf("expected unlinked, got %+v", view)
	}

	// Link to a Zitadel org; the email domain is normalized and stored for
	// reference only.
	view, err = svc.SetOrgSSO(ctx, admin, "acme", SSOConfigInput{
		ExternalOrgID: "289734982374",
		EmailDomain:   "Acme.com",
	})
	if err != nil {
		t.Fatalf("set sso: %v", err)
	}
	if !view.Configured || view.ExternalOrgID != "289734982374" || view.EmailDomain != "acme.com" {
		t.Fatalf("sso view after link: %+v", view)
	}

	// The link is persisted on the tenant so the auth layer can map a login from
	// that Zitadel org back to this Quill org.
	tenant, err := st.GetTenantBySlug(ctx, "acme")
	if err != nil {
		t.Fatalf("get tenant: %v", err)
	}
	if !tenant.ExternalOrgID.Valid || tenant.ExternalOrgID.String != "289734982374" {
		t.Fatalf("external org not persisted: %+v", tenant.ExternalOrgID)
	}

	// Enabling requires a Zitadel org id.
	if _, err := svc.SetOrgSSO(ctx, admin, "acme", SSOConfigInput{EmailDomain: "acme.com"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("link without org id: want ErrInvalidInput, got %v", err)
	}

	// A second Quill org cannot claim the same Zitadel org.
	if _, _, err := svc.CreateOrganization(ctx, admin, "beta", "Beta Inc"); err != nil {
		t.Fatalf("create org beta: %v", err)
	}
	if _, err := svc.SetOrgSSO(ctx, admin, "beta", SSOConfigInput{ExternalOrgID: "289734982374"}); !errors.Is(err, ErrInvalidInput) {
		t.Fatalf("duplicate zitadel org: want ErrInvalidInput, got %v", err)
	}

	// A non-platform-admin (even an org member) cannot read or write SSO.
	stranger := Actor{UserID: scopeMakeUser(t, st, "stranger")}
	if _, err := svc.GetOrgSSO(ctx, stranger, "acme"); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger get: want ErrForbidden, got %v", err)
	}
	if _, err := svc.SetOrgSSO(ctx, stranger, "acme", SSOConfigInput{ExternalOrgID: "x"}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("stranger set: want ErrForbidden, got %v", err)
	}

	// Delete unlinks it.
	if err := svc.DeleteOrgSSO(ctx, admin, "acme"); err != nil {
		t.Fatalf("delete sso: %v", err)
	}
	if v, _ := svc.GetOrgSSO(ctx, admin, "acme"); v.Configured {
		t.Fatalf("expected unlinked after delete, got %+v", v)
	}
	tenant, err = st.GetTenantBySlug(ctx, "acme")
	if err != nil {
		t.Fatalf("get tenant after delete: %v", err)
	}
	if tenant.ExternalOrgID.Valid && tenant.ExternalOrgID.String != "" {
		t.Fatalf("external org not cleared on delete: %+v", tenant.ExternalOrgID)
	}
}
