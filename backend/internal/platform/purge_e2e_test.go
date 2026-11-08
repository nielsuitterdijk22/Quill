package platform

import (
	"context"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// PurgeE2eDebris removes live-e2e leftovers (tenants/projects/repos with the
// e2e- slug prefix) without touching users, the default tenant, or anything
// a real account owns. It is the ops path behind `quill admin purge-e2e`.
func TestPurgeE2eDebris(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()

	// Debris: an org tenant + project + repo, and a fixture user's personal
	// project with test repos.
	fixture := scopeMakeUser(t, st, "e2e-tester")
	if _, _, err := svc.CreateOrganization(ctx, Actor{UserID: fixture}, "e2e-org-123", "E2E Org"); err != nil {
		t.Fatalf("create debris org: %v", err)
	}
	if err := svc.CreatePersonalProject(ctx, fixture, "e2e-tester"); err != nil {
		t.Fatalf("fixture personal project: %v", err)
	}
	project, err := st.GetProjectBySlug(ctx, "e2e-org-123")
	if err != nil {
		t.Fatalf("get debris project: %v", err)
	}
	if _, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
		ProjectID: project.ID, Slug: "e2e-repo-9", Name: "e2e-repo-9",
	}); err != nil {
		t.Fatalf("seed debris repo: %v", err)
	}

	// Real content that must survive: an org + project + repo without the prefix.
	realID := scopeMakeUser(t, st, "realuser")
	if _, _, err := svc.CreateOrganization(ctx, Actor{UserID: realID}, "blacktulip", "Black Tulip"); err != nil {
		t.Fatalf("create real org: %v", err)
	}

	report, err := svc.PurgeE2eDebris(ctx)
	if err != nil {
		t.Fatalf("PurgeE2eDebris: %v", err)
	}
	if report.Tenants != 1 || report.Projects != 1 || report.Repos < 1 {
		t.Fatalf("report too small: %+v", report)
	}

	// The debris org tenant and its project are gone.
	if _, err := st.GetTenantBySlug(ctx, "e2e-org-123"); err == nil {
		t.Fatal("debris tenant still exists")
	}
	if _, err := st.GetProjectBySlug(ctx, "e2e-org-123"); err == nil {
		t.Fatal("debris project still exists")
	}
	// The fixture user and their personal project survive (the personal project
	// row is kept — its repos, if any, are only purged when prefixed).
	if _, err := st.GetUserByID(ctx, fixture); err != nil {
		t.Fatalf("fixture user was deleted: %v", err)
	}
	if _, err := st.GetProjectBySlug(ctx, "e2e-tester"); err != nil {
		t.Fatalf("fixture personal project was deleted: %v", err)
	}
	// Real content is untouched.
	if _, err := st.GetTenantBySlug(ctx, "blacktulip"); err != nil {
		t.Fatalf("real org tenant was deleted: %v", err)
	}
	if _, err := st.GetProjectBySlug(ctx, "blacktulip"); err != nil {
		t.Fatalf("real org project was deleted: %v", err)
	}

	// Idempotent: a second run reports nothing to do.
	report, err = svc.PurgeE2eDebris(ctx)
	if err != nil {
		t.Fatalf("second PurgeE2eDebris: %v", err)
	}
	if report.Tenants != 0 || report.Projects != 0 || report.Repos != 0 {
		t.Fatalf("second run should be a no-op, got %+v", report)
	}
}

// A repo with the e2e- prefix inside a real (non-e2e) project is not purged:
// the prefix only marks whole test tenants/projects, not user content.
func TestPurgeE2eDebrisSparesForeignPrefix(t *testing.T) {
	svc, st := scopeTestService(t)
	ctx := context.Background()

	realID := scopeMakeUser(t, st, "realowner")
	if _, _, err := svc.CreateOrganization(ctx, Actor{UserID: realID}, "realorg", "Real Org"); err != nil {
		t.Fatalf("create real org: %v", err)
	}
	project, err := st.GetProjectBySlug(ctx, "realorg")
	if err != nil {
		t.Fatalf("get project: %v", err)
	}
	if _, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
		ProjectID: project.ID, Slug: "e2e-named-repo", Name: "e2e-named-repo",
	}); err != nil {
		t.Fatalf("seed repo: %v", err)
	}

	if _, err := svc.PurgeE2eDebris(ctx); err != nil {
		t.Fatalf("PurgeE2eDebris: %v", err)
	}
	if _, err := st.GetProjectBySlug(ctx, "realorg"); err != nil {
		t.Fatal("real project was purged")
	}
	repo, err := st.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{ProjectID: project.ID, Lower: "e2e-named-repo"})
	if err != nil {
		t.Fatal("user-owned e2e-named repo was purged")
	}
	if !strings.EqualFold(repo.Slug, "e2e-named-repo") {
		t.Fatalf("unexpected repo %+v", repo)
	}
}
