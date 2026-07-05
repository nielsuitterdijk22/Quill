package platform_test

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// newService spins up a platform Service against the live test database with
// Forgejo disabled (metadata-only), or skips when QUILL_TEST_DATABASE_URL is
// unset so CI without a DB stays green.
func newService(t *testing.T) (*platform.Service, *store.Store) {
	t.Helper()
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUILL_TEST_DATABASE_URL not set; skipping platform integration test")
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
		// TRUNCATE ... CASCADE clears dependent repos/members too; a plain DELETE
		// on projects would be blocked by repositories (ON DELETE RESTRICT). The
		// seeded default tenant is intentionally left in place. The sync outbox has
		// no FK to projects (delete events must outlive their project), so it is
		// truncated explicitly.
		_, _ = st.Pool().Exec(context.Background(), "TRUNCATE projects, users, project_sync_outbox CASCADE")
	}
	reset()
	t.Cleanup(reset)
	// Forgejo disabled: empty config means Enabled() is false.
	svc := platform.NewService(st, forgejo.New(config.ForgejoConfig{}), nil)
	return svc, st
}

func makeUser(t *testing.T, st *store.Store, username string) uuid.UUID {
	t.Helper()
	u, err := st.CreateUser(context.Background(), db.CreateUserParams{
		Username:    username,
		Email:       username + "@example.test",
		DisplayName: username,
		IsActive:    true,
	})
	if err != nil {
		t.Fatalf("create user: %v", err)
	}
	return u.ID
}

// actor is a non-admin platform.Actor for the given user id.
func actor(id uuid.UUID) platform.Actor { return platform.Actor{UserID: id} }

func TestCreateProjectProvisionsMembership(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	creator := makeUser(t, st, "alice")

	project, err := svc.CreateProject(ctx, platform.Actor{UserID: creator}, platform.CreateProjectInput{Slug: "Acme", Name: "Acme Inc"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if project.Slug != "acme" {
		t.Fatalf("slug should be normalised to lowercase, got %q", project.Slug)
	}

	// Creator is the sole project owner.
	members, err := st.ListProjectMembers(ctx, project.ID)
	if err != nil {
		t.Fatalf("list project members: %v", err)
	}
	if len(members) != 1 || members[0].ID != creator || members[0].MemberRole != "owner" {
		t.Fatalf("creator should be sole project owner, got %+v", members)
	}
}

func TestCreateProjectRejectsDuplicateSlug(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	creator := makeUser(t, st, "alice")

	if _, err := svc.CreateProject(ctx, platform.Actor{UserID: creator}, platform.CreateProjectInput{Slug: "acme"}); err != nil {
		t.Fatalf("first create: %v", err)
	}
	// Case-insensitive duplicate must conflict.
	if _, err := svc.CreateProject(ctx, platform.Actor{UserID: creator}, platform.CreateProjectInput{Slug: "ACME"}); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestCreateProjectValidatesSlug(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	creator := makeUser(t, st, "alice")

	for _, bad := range []string{"", "-bad", "has space", "_underscore"} {
		if _, err := svc.CreateProject(ctx, platform.Actor{UserID: creator}, platform.CreateProjectInput{Slug: bad}); !errors.Is(err, platform.ErrInvalidInput) {
			t.Fatalf("slug %q: expected ErrInvalidInput, got %v", bad, err)
		}
	}
}

func TestCreateRepoUnderProject(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	creator := makeUser(t, st, "alice")
	project, err := svc.CreateProject(ctx, platform.Actor{UserID: creator}, platform.CreateProjectInput{Slug: "acme"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	repo, err := svc.CreateRepo(ctx, actor(creator), "acme", platform.CreateRepoInput{Slug: "Widget", Name: "Widget"})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	if repo.Slug != "widget" {
		t.Fatalf("repo slug should be normalised, got %q", repo.Slug)
	}
	if repo.Visibility != platform.VisibilityPrivate {
		t.Fatalf("default visibility should be private, got %q", repo.Visibility)
	}
	if repo.DefaultBranch != "main" {
		t.Fatalf("default branch should be main, got %q", repo.DefaultBranch)
	}
	if repo.ProjectID != project.ID {
		t.Fatalf("repo project mismatch")
	}

	_, repos, err := svc.ListReposByProject(ctx, actor(creator), "acme", 0, 0)
	if err != nil {
		t.Fatalf("list repos: %v", err)
	}
	if len(repos) != 1 || repos[0].ID != repo.ID {
		t.Fatalf("expected the created repo in the listing, got %+v", repos)
	}
}

func TestCreateRepoRejectsUnknownProject(t *testing.T) {
	svc, st := newService(t)
	creator := makeUser(t, st, "alice")
	if _, err := svc.CreateRepo(context.Background(), actor(creator), "ghost", platform.CreateRepoInput{Slug: "x"}); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestCreateRepoRejectsDuplicate(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	creator := makeUser(t, st, "alice")
	if _, err := svc.CreateProject(ctx, platform.Actor{UserID: creator}, platform.CreateProjectInput{Slug: "acme"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, actor(creator), "acme", platform.CreateRepoInput{Slug: "widget"}); err != nil {
		t.Fatalf("first repo: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, actor(creator), "acme", platform.CreateRepoInput{Slug: "Widget"}); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("expected ErrConflict, got %v", err)
	}
}

func TestProjectAccessRequiresMembership(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	owner := makeUser(t, st, "alice")
	outsider := makeUser(t, st, "mallory")
	if _, err := svc.CreateProject(ctx, platform.Actor{UserID: owner}, platform.CreateProjectInput{Slug: "acme"}); err != nil {
		t.Fatalf("create project: %v", err)
	}

	// A non-member is denied reads and writes.
	if _, err := svc.GetProject(ctx, actor(outsider), "acme"); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("outsider GetProject: expected ErrForbidden, got %v", err)
	}
	if _, _, err := svc.ListReposByProject(ctx, actor(outsider), "acme", 0, 0); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("outsider ListReposByProject: expected ErrForbidden, got %v", err)
	}
	if _, err := svc.CreateRepo(ctx, actor(outsider), "acme", platform.CreateRepoInput{Slug: "secret"}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("outsider CreateRepo: expected ErrForbidden, got %v", err)
	}

	// A platform admin bypasses membership.
	admin := platform.Actor{UserID: outsider, IsAdmin: true}
	if _, err := svc.GetProject(ctx, admin, "acme"); err != nil {
		t.Fatalf("admin GetProject: %v", err)
	}

	// Unknown project resolves before the membership check, surfacing ErrNotFound.
	if _, err := svc.GetProject(ctx, actor(outsider), "ghost"); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown project, got %v", err)
	}
}

func TestBrowseAuthorizationAndAvailability(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	owner := makeUser(t, st, "alice")
	outsider := makeUser(t, st, "mallory")
	if _, err := svc.CreateProject(ctx, platform.Actor{UserID: owner}, platform.CreateProjectInput{Slug: "acme"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, actor(owner), "acme", platform.CreateRepoInput{Slug: "widget"}); err != nil {
		t.Fatalf("create repo: %v", err)
	}

	// A member can read repository metadata (no git backend needed).
	if _, err := svc.GetRepo(ctx, actor(owner), "acme", "widget"); err != nil {
		t.Fatalf("member GetRepo: %v", err)
	}

	// Non-members are denied before any git lookup.
	if _, err := svc.GetRepo(ctx, actor(outsider), "acme", "widget"); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("outsider GetRepo: expected ErrForbidden, got %v", err)
	}
	if _, _, err := svc.ListBranches(ctx, actor(outsider), "acme", "widget"); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("outsider ListBranches: expected ErrForbidden, got %v", err)
	}
	if _, _, err := svc.GetContents(ctx, actor(outsider), "acme", "widget", "", ""); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("outsider GetContents: expected ErrForbidden, got %v", err)
	}

	// With Forgejo disabled, git reads for a member surface ErrUnavailable.
	if _, _, err := svc.ListBranches(ctx, actor(owner), "acme", "widget"); !errors.Is(err, platform.ErrUnavailable) {
		t.Fatalf("member ListBranches without git: expected ErrUnavailable, got %v", err)
	}
	if _, _, err := svc.GetContents(ctx, actor(owner), "acme", "widget", "", ""); !errors.Is(err, platform.ErrUnavailable) {
		t.Fatalf("member GetContents without git: expected ErrUnavailable, got %v", err)
	}

	// Unknown repo resolves to ErrNotFound for an authorized member.
	if _, err := svc.GetRepo(ctx, actor(owner), "acme", "ghost"); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("expected ErrNotFound for unknown repo, got %v", err)
	}
}

func TestCreateProjectRejectsReservedSlug(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	creator := makeUser(t, st, "alice")
	for _, reserved := range []string{"new", "settings", "api"} {
		if _, err := svc.CreateProject(ctx, platform.Actor{UserID: creator}, platform.CreateProjectInput{Slug: reserved}); !errors.Is(err, platform.ErrInvalidInput) {
			t.Fatalf("reserved slug %q: expected ErrInvalidInput, got %v", reserved, err)
		}
	}
}

func ptr[T any](v T) *T { return &v }

// seedRepo creates an project owned by a fresh user plus a repository in it, and
// returns the owner id. Forgejo is disabled in these tests, so updates and
// deletes exercise the metadata path only.
func seedRepo(t *testing.T, svc *platform.Service, st *store.Store, projectSlug, repoSlug string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	owner := makeUser(t, st, "owner-"+projectSlug+"-"+repoSlug)
	if _, err := svc.CreateProject(ctx, platform.Actor{UserID: owner}, platform.CreateProjectInput{Slug: projectSlug}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, actor(owner), projectSlug, platform.CreateRepoInput{Slug: repoSlug, Name: repoSlug}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	return owner
}

func TestUpdateRepoMetadata(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	owner := seedRepo(t, svc, st, "acme", "widget")

	updated, err := svc.UpdateRepo(ctx, actor(owner), "acme", "widget", platform.UpdateRepoInput{
		Name:          ptr("Widget Service"),
		Description:   ptr("does widget things"),
		Visibility:    ptr(platform.VisibilityInternal),
		DefaultBranch: ptr("trunk"),
		Archived:      ptr(true),
	})
	if err != nil {
		t.Fatalf("update repo: %v", err)
	}
	if updated.Name != "Widget Service" || updated.Description != "does widget things" {
		t.Fatalf("name/description not applied: %+v", updated)
	}
	if updated.Visibility != platform.VisibilityInternal || updated.DefaultBranch != "trunk" || !updated.IsArchived {
		t.Fatalf("visibility/default/archived not applied: %+v", updated)
	}

	// Re-reading returns the persisted values.
	got, err := svc.GetRepo(ctx, actor(owner), "acme", "widget")
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	if got.Visibility != platform.VisibilityInternal || got.DefaultBranch != "trunk" || !got.IsArchived {
		t.Fatalf("changes did not persist: %+v", got)
	}
}

func TestUpdateRepoIsPartial(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	owner := seedRepo(t, svc, st, "acme", "widget")

	// Only the description is provided; other fields must be left untouched.
	updated, err := svc.UpdateRepo(ctx, actor(owner), "acme", "widget", platform.UpdateRepoInput{
		Description: ptr("just a description"),
	})
	if err != nil {
		t.Fatalf("update repo: %v", err)
	}
	if updated.Description != "just a description" {
		t.Fatalf("description not applied: %+v", updated)
	}
	if updated.Visibility != platform.VisibilityPrivate || updated.DefaultBranch != "main" || updated.IsArchived {
		t.Fatalf("untouched fields changed: %+v", updated)
	}
}

func TestUpdateRepoRename(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	owner := seedRepo(t, svc, st, "acme", "widget")

	updated, err := svc.UpdateRepo(ctx, actor(owner), "acme", "widget", platform.UpdateRepoInput{
		Slug: ptr("Gadget"),
	})
	if err != nil {
		t.Fatalf("rename repo: %v", err)
	}
	if updated.Slug != "gadget" {
		t.Fatalf("slug should be normalised to gadget, got %q", updated.Slug)
	}
	// The old slug no longer resolves; the new one does.
	if _, err := svc.GetRepo(ctx, actor(owner), "acme", "widget"); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("old slug should be gone, got %v", err)
	}
	if _, err := svc.GetRepo(ctx, actor(owner), "acme", "gadget"); err != nil {
		t.Fatalf("new slug should resolve: %v", err)
	}
}

func TestUpdateRepoRenameConflict(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	owner := seedRepo(t, svc, st, "acme", "widget")
	if _, err := svc.CreateRepo(ctx, actor(owner), "acme", platform.CreateRepoInput{Slug: "gadget", Name: "gadget"}); err != nil {
		t.Fatalf("create second repo: %v", err)
	}
	if _, err := svc.UpdateRepo(ctx, actor(owner), "acme", "widget", platform.UpdateRepoInput{Slug: ptr("gadget")}); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("expected ErrConflict renaming onto an existing slug, got %v", err)
	}
}

func TestUpdateRepoValidation(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	owner := seedRepo(t, svc, st, "acme", "widget")

	cases := map[string]platform.UpdateRepoInput{
		"bad visibility":     {Visibility: ptr("secret")},
		"empty default":      {DefaultBranch: ptr("   ")},
		"empty name":         {Name: ptr("  ")},
		"reserved slug":      {Slug: ptr("settings")},
		"invalid slug chars": {Slug: ptr("Has Spaces")},
	}
	for name, in := range cases {
		if _, err := svc.UpdateRepo(ctx, actor(owner), "acme", "widget", in); !errors.Is(err, platform.ErrInvalidInput) {
			t.Fatalf("%s: expected ErrInvalidInput, got %v", name, err)
		}
	}
}

func TestUpdateRepoRequiresOwner(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	owner := seedRepo(t, svc, st, "acme", "widget")
	project, _ := st.GetProjectBySlug(ctx, "acme")

	// A plain (non-owner) member may not change settings.
	member := makeUser(t, st, "bob")
	if err := st.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: project.ID, UserID: member, Role: "member"}); err != nil {
		t.Fatalf("add member: %v", err)
	}
	if _, err := svc.UpdateRepo(ctx, actor(member), "acme", "widget", platform.UpdateRepoInput{Description: ptr("nope")}); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("member update: expected ErrForbidden, got %v", err)
	}

	// An outsider is likewise denied.
	outsider := makeUser(t, st, "mallory")
	if err := svc.DeleteRepo(ctx, actor(outsider), "acme", "widget"); !errors.Is(err, platform.ErrForbidden) {
		t.Fatalf("outsider delete: expected ErrForbidden, got %v", err)
	}

	// The owner succeeds, confirming the gate is owner-specific.
	if _, err := svc.UpdateRepo(ctx, actor(owner), "acme", "widget", platform.UpdateRepoInput{Description: ptr("ok")}); err != nil {
		t.Fatalf("owner update: %v", err)
	}
}

func TestDeleteRepoCascadesPolicies(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	owner := seedRepo(t, svc, st, "acme", "widget")

	// Attach a branch policy so the delete must cascade it.
	if _, err := svc.SetBranchPolicy(ctx, actor(owner), "acme", "widget", platform.BranchPolicyInput{
		Pattern: "main", RequiredApprovals: 1, RequirePullRequest: true,
	}); err != nil {
		t.Fatalf("set policy: %v", err)
	}

	if err := svc.DeleteRepo(ctx, actor(owner), "acme", "widget"); err != nil {
		t.Fatalf("delete repo: %v", err)
	}
	if _, err := svc.GetRepo(ctx, actor(owner), "acme", "widget"); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("repo should be gone, got %v", err)
	}
	_, repos, err := svc.ListReposByProject(ctx, actor(owner), "acme", 0, 0)
	if err != nil {
		t.Fatalf("list repos: %v", err)
	}
	if len(repos) != 0 {
		t.Fatalf("expected no repos after delete, got %d", len(repos))
	}
}

func TestDeleteRepoUnknown(t *testing.T) {
	svc, st := newService(t)
	ctx := context.Background()
	owner := seedRepo(t, svc, st, "acme", "widget")
	if err := svc.DeleteRepo(ctx, actor(owner), "acme", "ghost"); !errors.Is(err, platform.ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting unknown repo, got %v", err)
	}
}
