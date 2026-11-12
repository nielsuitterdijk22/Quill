package store_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// newRepoForGitState seeds the minimal parent chain (tenant → project → repo)
// for git-engine referee tests.
func newRepoForGitState(t *testing.T, st *store.Store) (uuid.UUID, uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	tenant, err := st.GetOrCreateTenantByExternalOrg(ctx, uuid.NewString(), "gst-"+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("create tenant: %v", err)
	}
	project, err := st.CreateProject(ctx, db.CreateProjectParams{
		TenantID: tenant.ID, Slug: "gst-" + uuid.NewString()[:8], Name: "gst",
	})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	repo, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
		ProjectID: project.ID, Slug: "r" + uuid.NewString()[:8], Name: "r",
		Visibility: "private", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("create repository: %v", err)
	}
	return repo.ID, tenant.ID
}

// TestGitRepoStateEnsureLoad verifies provisioning stamps the referee row: the
// tenant is recorded so metering never joins through projects, and empty reps
// round-trip.
func TestGitRepoStateEnsureLoad(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	repoID, tenantID := newRepoForGitState(t, st)

	row, err := st.EnsureGitRepoState(ctx, repoID, tenantID)
	if err != nil {
		t.Fatalf("EnsureGitRepoState: %v", err)
	}
	if row.Revision != 1 || len(row.Refs) != 0 || row.StorageBytes != 0 {
		t.Fatalf("fresh state = %+v, want revision 1 / empty refs", row)
	}
	if row.TenantID == nil || *row.TenantID != tenantID {
		t.Fatalf("state tenant = %+v, want %v", row.TenantID, tenantID)
	}

	// Idempotent: provisioning twice (retry after partial failure) must not
	// reset the revision or refs.
	updated, err := st.CASGitRepoRefs(ctx, repoID, 1, map[string]string{"refs/heads/main": "sha1"}, 100)
	if err != nil {
		t.Fatalf("CAS: %v", err)
	}
	if updated != 2 {
		t.Fatalf("revision after CAS = %d, want 2", updated)
	}
	again, err := st.EnsureGitRepoState(ctx, repoID, tenantID)
	if err != nil {
		t.Fatalf("Ensure again: %v", err)
	}
	if again.Revision != 2 || again.Refs["refs/heads/main"] != "sha1" {
		t.Fatalf("ensure must not clobber state: %+v", again)
	}

	got, err := st.GetGitRepoState(ctx, repoID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Revision != 2 || got.StorageBytes != 100 || got.Refs["refs/heads/main"] != "sha1" {
		t.Fatalf("state = %+v", got)
	}
}

// TestGitRepoStateCASLostRace verifies the compare-and-swap semantics: two
// writers racing from the same revision — exactly one wins, the loser gets a
// typed lost-race error, and the revision never regresses or forks.
func TestGitRepoStateCASLostRace(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	repoID, tenantID := newRepoForGitState(t, st)

	if _, err := st.EnsureGitRepoState(ctx, repoID, tenantID); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	const racers = 20
	var winners, losers int64
	start := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < racers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			refs := map[string]string{"refs/heads/main": "sha-winner-" + string(rune('a'+i))}
			if _, err := st.CASGitRepoRefs(ctx, repoID, 1, refs, 1000); err == nil {
				atomic.AddInt64(&winners, 1)
			} else if err == store.ErrRefCASLost {
				atomic.AddInt64(&losers, 1)
			} else {
				t.Errorf("CAS: unexpected error: %v", err)
			}
		}(i)
	}
	close(start)
	wg.Wait()

	if winners != 1 {
		t.Fatalf("winners = %d, want exactly 1", winners)
	}
	if losers != racers-1 {
		t.Fatalf("losers = %d, want %d typed lost-race errors", losers, racers-1)
	}

	// Unknown repo: not a lost race, a real miss (pgx.ErrNoRows).
	if _, err := st.CASGitRepoRefs(ctx, uuid.New(), 1, nil, 0); err != pgx.ErrNoRows {
		t.Fatalf("CAS on unknown repo = %v, want pgx.ErrNoRows", err)
	}
}

// TestTenantRuntimeHint asserts the unused routing column from the roadmap's
// scale-out amendment: default 'serverless', settable to 'warm' for later
// pools. No generated query reads it yet (deliberately — "design now,
// implement later"), so this goes through raw SQL.
func TestTenantRuntimeHint(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	var hint string
	err := st.Pool().QueryRow(ctx,
		"SELECT runtime_hint FROM tenants WHERE slug='default'").Scan(&hint)
	if err != nil {
		t.Fatalf("select runtime_hint: %v", err)
	}
	if hint != "serverless" {
		t.Fatalf("runtime_hint default = %q, want 'serverless'", hint)
	}

	_, err = st.Pool().Exec(ctx,
		"UPDATE tenants SET runtime_hint='warm' WHERE slug='default'")
	if err != nil {
		t.Fatalf("set hint: %v", err)
	}
	err = st.Pool().QueryRow(ctx,
		"SELECT runtime_hint FROM tenants WHERE slug='default'").Scan(&hint)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	if hint != "warm" {
		t.Fatalf("runtime_hint after set = %q, want 'warm'", hint)
	}
	_, _ = st.Pool().Exec(ctx, "UPDATE tenants SET runtime_hint='serverless' WHERE slug='default'")
}

// TestFindRepoForGit verifies the interim B2 resolver: slugs → repo identity
// including the tenant for the git engine's bucket layout. Duplication across
// tenants must be an explicit error (B6 replaces this with token-scoped
// resolution).
func TestFindRepoForGit(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	repoID, tenantID := newRepoForGitState(t, st)

	// Resolve the repo we just created via its slugs.
	var projectSlug, repoSlug string
	if err := st.Pool().QueryRow(ctx,
		"SELECT p.slug, r.slug FROM repositories r JOIN projects p ON p.id=r.project_id WHERE r.id=$1",
		repoID).Scan(&projectSlug, &repoSlug); err != nil {
		t.Fatalf("lookup slugs: %v", err)
	}

	ref, err := st.FindRepoForGit(ctx, projectSlug, repoSlug)
	if err != nil {
		t.Fatalf("FindRepoForGit: %v", err)
	}
	if ref.RepoID != repoID || ref.TenantID != tenantID || ref.ProjectSlug != projectSlug || ref.RepoSlug != repoSlug {
		t.Fatalf("ref = %+v", ref)
	}

	// Unknown slugs: not found.
	if _, err := st.FindRepoForGit(ctx, "ghost", "nope"); err != pgx.ErrNoRows {
		t.Fatalf("unknown slugs = %v, want pgx.ErrNoRows", err)
	}

	// Ambiguity premise: project slugs are globally unique today, so the
	// resolver's ambiguity guard cannot trigger against this schema —
	// prove the premise by failing to create a duplicate project slug.
	_, tenant2 := newRepoForGitState(t, st)
	_, err = st.CreateProject(ctx, db.CreateProjectParams{
		TenantID: tenant2, Slug: projectSlug, Name: "colliding",
	})
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Fatalf("duplicate project slug = %v, want unique violation (the ambiguity guard's premise)", err)
	}
	// Repo slugs within a project are unique too — FindRepoForGit stays
	// deterministic; the ErrRepoSlugAmbiguous branch is defense-in-depth for
	// a future per-tenant slug scheme (B6 replaces this resolver anyway).
}

// TestPushLeaseAcquireRelease verifies the cross-process single-writer lease:
// acquire wins on a free row, blocks a second holder, expires a stale one,
// and release only unlocks the row the holder still owns.
func TestPushLeaseAcquireRelease(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	repoID, _ := newRepoForGitState(t, st)

	won, err := st.AcquirePushLease(ctx, repoID, "holder-1", time.Minute)
	if err != nil {
		t.Fatalf("acquire: %v", err)
	}
	if !won {
		t.Fatal("first acquire must win")
	}

	// A second holder is rejected while the first lease is live.
	won2, err := st.AcquirePushLease(ctx, repoID, "holder-2", time.Minute)
	if err != nil {
		t.Fatalf("competing acquire: %v", err)
	}
	if won2 {
		t.Fatal("second holder must not win a live lease")
	}

	// Re-acquiring as the same holder is idempotent (retry after a network
	// blip must not self-deadlock).
	won1b, err := st.AcquirePushLease(ctx, repoID, "holder-1", time.Minute)
	if err != nil || !won1b {
		t.Fatalf("same-holder re-acquire = %v %v, want true", won1b, err)
	}

	// Release by the wrong holder does nothing.
	if err := st.ReleasePushLease(ctx, repoID, "holder-2"); err != nil {
		t.Fatalf("wrong-holder release: %v", err)
	}
	won2b, err := st.AcquirePushLease(ctx, repoID, "holder-2", time.Minute)
	if err != nil || won2b {
		t.Fatalf("lease must stay held after wrong-holder release: %v %v", won2b, err)
	}

	// The right holder releases; someone else can acquire.
	if err := st.ReleasePushLease(ctx, repoID, "holder-1"); err != nil {
		t.Fatalf("release: %v", err)
	}
	won2c, err := st.AcquirePushLease(ctx, repoID, "holder-2", time.Minute)
	if err != nil || !won2c {
		t.Fatalf("acquire after release = %v %v", won2c, err)
	}

	// An expired lease is stealable: insert a stale one directly.
	if _, err := st.Pool().Exec(ctx,
		"UPDATE push_leases SET expires = now() - interval '1 second' WHERE repo_id = $1", repoID); err != nil {
		t.Fatalf("age lease: %v", err)
	}
	won3, err := st.AcquirePushLease(ctx, repoID, "holder-3", time.Minute)
	if err != nil || !won3 {
		t.Fatalf("acquire after expiry = %v %v", won3, err)
	}
}
