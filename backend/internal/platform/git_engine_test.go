package platform_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/gitengine"
	"github.com/nielsuitterdijk22/quill/internal/objectstore"
	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// TestCreateRepoOwnEngine verifies own-mode provisioning end to end (serverless
// epic, PR B1): no Forgejo is involved (the client is disabled in the test
// harness), the bucket receives an empty versioned manifest stamped with the
// tenant, and the Postgres ref-CAS row exists with the tenant recorded so
// metering never joins through projects.
func TestCreateRepoOwnEngine(t *testing.T) {
	svc, st := newService(t)
	mem := objectstore.NewMemStore()
	svc = svc.WithGitEngine(gitengine.New(mem))
	ctx := context.Background()
	creator := makeUser(t, st, "alice")
	project, err := svc.CreateProject(ctx, platform.Actor{UserID: creator}, platform.CreateProjectInput{Slug: "acme"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}

	repo, err := svc.CreateRepo(ctx, actor(creator), "acme", platform.CreateRepoInput{Slug: "widget"})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}

	// The bucket holds an empty manifest of the pinned layout.
	key := gitengine.ManifestKey(project.TenantID.String(), "acme", "widget")
	body, _, err := mem.Get(ctx, key)
	if err != nil {
		t.Fatalf("manifest in bucket: %v", err)
	}
	m, err := gitengine.DecodeManifest(strings.NewReader(string(body)))
	if err != nil {
		t.Fatalf("decode manifest: %v", err)
	}
	if m.Version != gitengine.ManifestVersion || m.DefaultBranch != "main" || m.TenantID != project.TenantID.String() {
		t.Fatalf("manifest = %+v", m)
	}
	if len(m.Refs) != 0 || len(m.Packs) != 0 || m.StorageBytes != 0 {
		t.Fatalf("fresh manifest must be empty: %+v", m)
	}

	// The referee row exists and is tenant-stamped.
	state, err := st.GetGitRepoState(ctx, repo.ID)
	if err != nil {
		t.Fatalf("git repo state: %v", err)
	}
	if state.Revision != 1 || len(state.Refs) != 0 {
		t.Fatalf("state = %+v", state)
	}
	if state.TenantID == nil || state.TenantID.String() != project.TenantID.String() {
		t.Fatalf("state tenant = %+v, want %v", state.TenantID, project.TenantID)
	}

	// No Forgejo linkage on an own-engine repo (clean rows until Phase D).
	if repo.ForgejoRepoID.Valid || repo.ForgejoOwner.Valid || repo.ForgejoName.Valid {
		t.Fatalf("own-engine repo must not carry forgejo linkage: %+v", repo)
	}
}

// TestCreateRepoOwnEngineDoubleProvision: the slug uniqueness check still
// guards provisioning — a conflict must fail before the bucket or the
// referee are touched (exactly one manifest in the bucket afterwards).
func TestCreateRepoOwnEngineDoubleProvision(t *testing.T) {
	svc, st := newService(t)
	mem := objectstore.NewMemStore()
	svc = svc.WithGitEngine(gitengine.New(mem))
	ctx := context.Background()
	creator := makeUser(t, st, "alice")
	if _, err := svc.CreateProject(ctx, platform.Actor{UserID: creator}, platform.CreateProjectInput{Slug: "acme"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, actor(creator), "acme", platform.CreateRepoInput{Slug: "widget"}); err != nil {
		t.Fatalf("first repo: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, actor(creator), "acme", platform.CreateRepoInput{Slug: "Widget"}); !errors.Is(err, platform.ErrConflict) {
		t.Fatalf("duplicate slug = %v, want ErrConflict", err)
	}
	if got := len(mem.Keys()); got != 1 {
		t.Fatalf("bucket must hold exactly the first repo's manifest, got %d objects", got)
	}
}

// TestCreateRepoOwnEngineRejectsImport: the GitHub-import (CloneFrom) surface
// is not available for own-engine repos yet (repo migration tool is PR D);
// requesting it must fail loudly instead of provisioning half a repo.
func TestCreateRepoOwnEngineRejectsImport(t *testing.T) {
	svc, st := newService(t)
	mem := objectstore.NewMemStore()
	svc = svc.WithGitEngine(gitengine.New(mem))
	ctx := context.Background()
	creator := makeUser(t, st, "alice")
	project, err := svc.CreateProject(ctx, platform.Actor{UserID: creator}, platform.CreateProjectInput{Slug: "acme"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, actor(creator), "acme", platform.CreateRepoInput{
		Slug:      "widget",
		CloneFrom: &platform.CloneSource{URL: "https://github.com/example/example"},
	}); err == nil {
		t.Fatal("clone-from under own engine must fail, got nil")
	}
	key := gitengine.ManifestKey(project.TenantID.String(), "acme", "widget")
	if _, _, err := mem.Get(ctx, key); err == nil {
		t.Fatal("manifest must not exist after rejected import")
	}
	if got := len(mem.Keys()); got != 0 {
		t.Fatalf("nothing may be provisioned on rejected import, got %d objects", got)
	}
}
