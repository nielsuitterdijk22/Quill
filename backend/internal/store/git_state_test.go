package store_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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
