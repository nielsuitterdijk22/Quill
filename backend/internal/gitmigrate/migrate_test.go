package gitmigrate_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/config"
	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/gitengine"
	"github.com/nielsuitterdijk22/quill/internal/gitmigrate"
	"github.com/nielsuitterdijk22/quill/internal/objectstore"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// D1 tests: the migrator moves a REAL git repository (built with the git
// binary into a local bare mirror — the same shape the live Forgejo serves)
// into the engine's bucket truth through the same primitives a push uses,
// then proves equivalence (fsck, object counts, ref targets). The Forgejo
// REST surface is faked over httptest, mirroring the forgejo package's own
// test pattern. DB-backed tests gate on QUILL_TEST_DATABASE_URL.

func gitRun(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.com")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// buildSourceRepo creates a bare mirror (the Forgejo-shaped source) with two
// branches and an annotated tag, returning its path.
func buildSourceRepo(t *testing.T, root, name string) string {
	t.Helper()
	work := filepath.Join(root, "work", name)
	if err := os.MkdirAll(work, 0o755); err != nil {
		t.Fatal(err)
	}
	gitRun(t, work, "init", "-q", "-b", "main", ".")
	gitRun(t, work, "config", "user.email", "test@example.com")
	gitRun(t, work, "config", "user.name", "test")
	os.WriteFile(filepath.Join(work, "hello.txt"), []byte("hello forgejo\n"), 0o644)
	gitRun(t, work, "add", ".")
	gitRun(t, work, "commit", "-q", "-m", "first")
	gitRun(t, work, "checkout", "-q", "-b", "feature")
	os.WriteFile(filepath.Join(work, "feature.txt"), []byte("feature body\n"), 0o644)
	gitRun(t, work, "add", ".")
	gitRun(t, work, "commit", "-q", "-m", "second")
	gitRun(t, work, "tag", "-a", "v1", "-m", "tag one")
	gitRun(t, work, "checkout", "-q", "main")
	os.WriteFile(filepath.Join(work, "main2.txt"), []byte("second on main\n"), 0o644)
	gitRun(t, work, "add", ".")
	gitRun(t, work, "commit", "-q", "-m", "third")
	src := filepath.Join(root, "src", name+".git")
	if out, err := gitRunOut(work, "clone", "--mirror", "--quiet", ".", src); err != nil {
		t.Fatalf("mirror clone: %v %s", err, out)
	}
	return src
}

func gitRunOut(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

// testDB opens the gated test store (skip when unset) and migrates the schema.
func testDB(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("QUILL_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("QUILL_TEST_DATABASE_URL not set; skipping")
	}
	if err := store.Migrate(dsn); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st, err := store.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(st.Close)
	return st
}

// dbReferee is the test twin of the production pgReferee (unexported in
// platform): the Postgres row IS the commit point.
type dbReferee struct{ st *store.Store }

func (r dbReferee) Load(ctx context.Context, repoID string) (gitengine.RefereeState, error) {
	id, err := uuid.Parse(repoID)
	if err != nil {
		return gitengine.RefereeState{}, err
	}
	row, err := r.st.GetGitRepoState(ctx, id)
	if err != nil {
		return gitengine.RefereeState{}, err
	}
	return gitengine.RefereeState{Revision: row.Revision, Refs: row.Refs, StorageBytes: row.StorageBytes}, nil
}

func (r dbReferee) Commit(ctx context.Context, repoID string, from gitengine.RefereeState, refs map[string]string, storage int64) (gitengine.RefereeState, error) {
	id, err := uuid.Parse(repoID)
	if err != nil {
		return gitengine.RefereeState{}, err
	}
	rev, err := r.st.CASGitRepoRefs(ctx, id, from.Revision, refs, storage)
	if err != nil {
		return gitengine.RefereeState{}, err
	}
	return gitengine.RefereeState{Revision: rev, Refs: refs, StorageBytes: storage}, nil
}

// dbLeases is the test twin of platform's pgLeases.
type dbLeases struct{ st *store.Store }

func (l dbLeases) Acquire(ctx context.Context, key string, ttl time.Duration) (func() error, error) {
	id, err := uuid.Parse(key)
	if err != nil {
		return nil, err
	}
	won, err := l.st.AcquirePushLease(ctx, id, "migrate-test", ttl)
	if err != nil {
		return nil, err
	}
	if !won {
		return nil, gitengine.ErrLeaseHeld
	}
	return func() error { return l.st.ReleasePushLease(ctx, id, "migrate-test") }, nil
}

// fakeForgejo serves the pull-history endpoints the import consumes.
func fakeForgejo(t *testing.T, pulls []forgejo.PullRequest, comments map[int][]forgejo.IssueComment,
	reviews map[int][]forgejo.Review, lineComments map[int][]forgejo.ReviewCodeComment) *forgejo.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.HasSuffix(r.URL.Path, "/pulls"):
			_ = json.NewEncoder(w).Encode(pulls)
		case strings.Contains(r.URL.Path, "/branch_protections"):
			_ = json.NewEncoder(w).Encode(forgejo.BranchProtection{BranchName: "*", EnablePush: false})
		case strings.Contains(r.URL.Path, "/comments") && strings.Contains(r.URL.Path, "/issues"):
			n := numberFromPath(r.URL.Path)
			_ = json.NewEncoder(w).Encode(comments[n])
		case strings.Contains(r.URL.Path, "/reviews") && strings.HasSuffix(r.URL.Path, "/comments"):
			n := numberFromPath(r.URL.Path)
			_ = json.NewEncoder(w).Encode(lineComments[n])
		case strings.Contains(r.URL.Path, "/reviews"):
			n := numberFromPath(r.URL.Path)
			_ = json.NewEncoder(w).Encode(reviews[n])
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	return forgejo.New(config.ForgejoConfig{BaseURL: srv.URL, AdminToken: "t"})
}

func numberFromPath(path string) int {
	parts := strings.Split(strings.Trim(path, "/"), "/")
	for _, p := range parts {
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err == nil && n > 0 {
			return n
		}
	}
	return 0
}

// seedRepo chains tenant → project (forgejo org) → repo (forgejo link) and
// returns the store link the migrator walks.
func seedRepo(t *testing.T, st *store.Store, projectSlug, repoSlug, owner, name string) store.ForgejoRepoLink {
	t.Helper()
	ctx := context.Background()
	tenant, err := st.GetOrCreateTenantByExternalOrg(ctx, uuid.NewString(), "mig-"+uuid.NewString()[:8])
	if err != nil {
		t.Fatalf("tenant: %v", err)
	}
	project, err := st.CreateProject(ctx, db.CreateProjectParams{
		TenantID: tenant.ID, Slug: projectSlug, Name: projectSlug,
	})
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	repo, err := st.CreateRepository(ctx, db.CreateRepositoryParams{
		ProjectID: project.ID, Slug: repoSlug, Name: repoSlug,
		Visibility: "private", DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("repo: %v", err)
	}
	if _, err := st.Pool().Exec(ctx,
		`UPDATE repositories SET forgejo_repo_id = $2, forgejo_owner = $3, forgejo_name = $4 WHERE id = $1`,
		repo.ID, int64(77), owner, name); err != nil {
		t.Fatalf("link: %v", err)
	}
	if _, err := st.EnsureGitRepoState(ctx, repo.ID, tenant.ID); err != nil {
		t.Fatalf("referee row: %v", err)
	}
	// The engine state starts EMPTY (nothing migrated yet).
	t.Cleanup(func() {
		_, _ = st.Pool().Exec(ctx, "DELETE FROM repositories WHERE id = $1", repo.ID)
		_, _ = st.Pool().Exec(ctx, "DELETE FROM projects WHERE id = $1", project.ID)
		_, _ = st.Pool().Exec(ctx, "DELETE FROM tenants WHERE id = $1", tenant.ID)
	})
	return store.ForgejoRepoLink{
		RepoID: repo.ID, RepoSlug: repoSlug, ProjectID: project.ID,
		ProjectSlug: projectSlug, TenantID: &tenant.ID,
		ForgejoOwner: owner, ForgejoName: name,
		Visibility: "private", DefaultBranch: "main",
	}
}

func TestMigrateRepoEndToEnd(t *testing.T) {
	st := testDB(t)
	ctx := context.Background()
	root := t.TempDir()

	src := buildSourceRepo(t, root, "kitchen")
	link := seedRepo(t, st, "kproj", "kr", "kproj", "kr")

	mem := objectstore.NewMemStore()
	engine := gitengine.New(mem)
	cache := &gitengine.RepoCache{
		Root:   filepath.Join(root, "cache"),
		Engine: engine,
		Packs:  gitengine.NewBucketPackSource(mem),
	}
	mg := &gitmigrate.Migrator{
		Engine: engine, Cache: cache, Leases: dbLeases{st}, Store: st,
		Referee:  dbReferee{st},
		CloneURL: func(owner, name string) string { return "file://" + src },
		WorkDir:  filepath.Join(root, "scratch"),
	}

	res, err := mg.MigrateRepo(ctx, link, gitmigrate.Options{SkipPRs: true})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res.Status != "migrated" {
		t.Fatalf("status = %q (%s)", res.Status, res.SkippedReason)
	}
	// main + feature + tag (annotated tags materialize one ref entry).
	if res.Refs != 3 {
		t.Fatalf("refs = %d, want 3 (main, feature, v1)", res.Refs)
	}
	if res.Packs != 1 {
		t.Fatalf("packs = %d, want 1 (repack -a -d)", res.Packs)
	}
	if res.StorageBytes <= 0 {
		t.Fatalf("storage bytes not recorded: %d", res.StorageBytes)
	}
	if !res.LiveRefsVerified {
		t.Fatalf("live refs not verified")
	}
	if res.VerifyObjects == 0 {
		t.Fatalf("verify did not count objects")
	}

	// The engine now OWNS the repo: hydration works and serves every ref.
	hyd, err := cache.Hydrate(ctx, link.TenantID.String(), link.ProjectSlug, link.RepoSlug)
	if err != nil {
		t.Fatalf("hydrate: %v", err)
	}
	mainSHA := gitRun(t, hyd, "rev-parse", "refs/heads/main")
	if mainSHA == "" {
		t.Fatal("main missing after hydration")
	}
	featSHA := gitRun(t, hyd, "rev-parse", "refs/heads/feature")
	if featSHA == "" {
		t.Fatal("feature missing")
	}
	if peeled := gitRun(t, hyd, "rev-parse", "refs/tags/v1"); peeled == "" {
		t.Fatal("tag missing")
	}
	// The tag's peeled commit is recorded in the manifest.
	m, _, err := engine.LoadManifest(ctx, link.TenantID.String(), link.ProjectSlug, link.RepoSlug)
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	var sawPeeled bool
	for _, r := range m.Refs {
		if r.Name == "refs/tags/v1" && r.Peeled != "" {
			sawPeeled = true
		}
	}
	if !sawPeeled {
		t.Fatalf("annotated tag peeled commit not recorded: %+v", m.Refs)
	}
	if m.DefaultBranch != "main" {
		t.Fatalf("default branch = %q", m.DefaultBranch)
	}

	// Idempotence: a second run skips (engine-owned).
	res2, err := mg.MigrateRepo(ctx, link, gitmigrate.Options{SkipPRs: true})
	if err != nil {
		t.Fatalf("re-migrate: %v", err)
	}
	if res2.Status != "skipped" {
		t.Fatalf("re-run status = %q, want skipped", res2.Status)
	}

	// Push history recorded the migration actor.
	if len(m.PushHistory) != 1 || m.PushHistory[0].PushedBy != "forgejo-import" {
		t.Fatalf("push history: %+v", m.PushHistory)
	}
}

func TestMigrateRepoImportsPullHistory(t *testing.T) {
	st := testDB(t)
	ctx := context.Background()
	root := t.TempDir()

	src := buildSourceRepo(t, root, "pulls")
	link := seedRepo(t, st, "pproj", "pr", "pproj", "pr")

	// A Quill user the import can resolve + one unknown author.
	login := "miguser-" + uuid.NewString()[:8]
	if _, err := st.Pool().Exec(ctx,
		`INSERT INTO users (id, username, email, forgejo_username) VALUES ($1, $2, $3, $2)`,
		uuid.New(), login, login+"@ex.com"); err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() { _, _ = st.Pool().Exec(ctx, `DELETE FROM users WHERE username = $1`, login) })

	mergedAt := time.Date(2025, 6, 1, 9, 0, 0, 0, time.UTC)
	pulls := []forgejo.PullRequest{{
		Number: 3, Title: "feat: go", Body: "the body", State: "closed", Merged: true,
		User:     &forgejo.User{Login: login},
		Head:     forgejo.PRRef{Label: "p:feature", Ref: "feature", SHA: "111"},
		Base:     forgejo.PRRef{Label: "p:main", Ref: "main", SHA: "222"},
		MergedBy: &forgejo.User{Login: "ghost-author"},
		MergedAt: &mergedAt, MergeCommit: "ccc",
		CreatedAt: time.Date(2025, 5, 30, 8, 0, 0, 0, time.UTC),
	}}
	fc := fakeForgejo(t, pulls,
		map[int][]forgejo.IssueComment{3: {{ID: 9, Body: "looks good", User: &forgejo.User{Login: "unknown-one"},
			CreatedAt: time.Date(2025, 5, 31, 8, 0, 0, 0, time.UTC)}}},
		map[int][]forgejo.Review{3: {{ID: 4, State: forgejo.ReviewApproved, User: &forgejo.User{Login: login},
			SubmittedAt: mergedAt}, {ID: 5, State: forgejo.ReviewPending, User: &forgejo.User{Login: login}}}},
		map[int][]forgejo.ReviewCodeComment{3: {{ID: 6, Path: "main.go", Position: 7, Body: "nit",
			User: &forgejo.User{Login: login}, CreatedAt: mergedAt}}})

	mem := objectstore.NewMemStore()
	engine := gitengine.New(mem)
	cache := &gitengine.RepoCache{
		Root:   filepath.Join(root, "cache"),
		Engine: engine,
		Packs:  gitengine.NewBucketPackSource(mem),
	}
	mg := &gitmigrate.Migrator{
		Engine: engine, Cache: cache, Leases: dbLeases{st}, Store: st,
		Referee: dbReferee{st}, Forgejo: fc,
		CloneURL: func(owner, name string) string { return "file://" + src },
		WorkDir:  filepath.Join(root, "scratch"),
	}

	res, err := mg.MigrateRepo(ctx, link, gitmigrate.Options{})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res.PullsImported != 1 || res.CommentsAdded != 1 || res.ReviewsAdded != 1 {
		t.Fatalf("history counts wrong: %+v", res)
	}

	// The row keeps Forgejo's number, timestamps and author snapshot.
	imported, err := st.GetPullByNumber(ctx, link.RepoID, 3)
	if err != nil {
		t.Fatalf("get imported pull: %v", err)
	}
	if imported.Title != "feat: go" || imported.State != "closed" || !imported.Merged {
		t.Fatalf("imported fields: %+v", imported)
	}
	if !imported.CreatedAt.Equal(time.Date(2025, 5, 30, 8, 0, 0, 0, time.UTC)) {
		t.Fatalf("created_at not preserved: %v", imported.CreatedAt)
	}
	if imported.CreatedBy == nil {
		t.Fatalf("resolvable author not linked")
	}
	if imported.CreatedByName != login {
		t.Fatalf("author snapshot: %q", imported.CreatedByName)
	}
	if imported.MergedBy != nil || imported.MergedByName != "ghost-author" {
		t.Fatalf("unknown merger must keep snapshot with nil id: %+v", imported)
	}
	comments, err := st.ListPullComments(ctx, imported.ID)
	if err != nil || len(comments) != 1 || comments[0].AuthorName != "unknown-one" {
		t.Fatalf("comments: %v %+v", err, comments)
	}
	reviews, err := st.ListPullReviews(ctx, imported.ID)
	if err != nil || len(reviews) != 1 {
		t.Fatalf("pending review must be skipped: %v %+v", err, reviews)
	}
	lines, err := st.ListPullLineComments(ctx, imported.ID)
	if err != nil || len(lines) != 1 || lines[0].Path != "main.go" || lines[0].Line != 7 {
		t.Fatalf("line comments: %v %+v", err, lines)
	}
}

func TestMigrateRepoEmptySource(t *testing.T) {
	st := testDB(t)
	ctx := context.Background()
	root := t.TempDir()

	// An EMPTY bare repo (Forgejo "empty" repos have no refs at all).
	src := filepath.Join(root, "src", "empty.git")
	if out, err := gitRunOut(root, "init", "-q", "--bare", src); err != nil {
		t.Fatalf("init bare: %v %s", err, out)
	}
	link := seedRepo(t, st, "eproj", "er", "eproj", "er")

	mem := objectstore.NewMemStore()
	engine := gitengine.New(mem)
	cache := &gitengine.RepoCache{Root: filepath.Join(root, "cache"), Engine: engine, Packs: gitengine.NewBucketPackSource(mem)}
	mg := &gitmigrate.Migrator{
		Engine: engine, Cache: cache, Leases: dbLeases{st}, Store: st,
		Referee:  dbReferee{st},
		CloneURL: func(owner, name string) string { return "file://" + src },
		WorkDir:  filepath.Join(root, "scratch"),
	}
	res, err := mg.MigrateRepo(ctx, link, gitmigrate.Options{SkipPRs: true})
	if err != nil {
		t.Fatalf("migrate empty: %v", err)
	}
	if res.Status != "empty" {
		t.Fatalf("status = %q, want empty", res.Status)
	}
	// The empty manifest exists (hydration of an empty repo must not 404).
	if _, _, err := engine.LoadManifest(ctx, link.TenantID.String(), link.ProjectSlug, link.RepoSlug); err != nil {
		t.Fatalf("empty manifest missing: %v", err)
	}
}

func TestMigrateRepoLeaseHeld(t *testing.T) {
	st := testDB(t)
	ctx := context.Background()
	root := t.TempDir()

	src := buildSourceRepo(t, root, "leased")
	link := seedRepo(t, st, "lproj", "lr", "lproj", "lr")

	// Someone else holds the push lease: migration must fail cleanly.
	if won, err := st.AcquirePushLease(ctx, link.RepoID, "someone-else", time.Hour); err != nil || !won {
		t.Fatalf("seed lease: %v %v", won, err)
	}

	mem := objectstore.NewMemStore()
	engine := gitengine.New(mem)
	cache := &gitengine.RepoCache{Root: filepath.Join(root, "cache"), Engine: engine, Packs: gitengine.NewBucketPackSource(mem)}
	mg := &gitmigrate.Migrator{
		Engine: engine, Cache: cache, Leases: dbLeases{st}, Store: st,
		Referee:  dbReferee{st},
		CloneURL: func(owner, name string) string { return "file://" + src },
		WorkDir:  filepath.Join(root, "scratch"),
	}
	_, err := mg.MigrateRepo(ctx, link, gitmigrate.Options{SkipPRs: true})
	if err == nil {
		t.Fatal("expected lease-held failure")
	}
	if !strings.Contains(err.Error(), "push lease") {
		t.Fatalf("wrong failure: %v", err)
	}
}

// Production Forgejo-era repos were never provisioned through the engine, so
// they have NO git_repo_state referee row — the migrator must create it
// (tenant-stamped) instead of failing "referee load: no rows" (found live on
// the first production run, 2026-11-13).
func TestMigrateRepoCreatesMissingRefereeRow(t *testing.T) {
	st := testDB(t)
	ctx := context.Background()
	root := t.TempDir()

	src := buildSourceRepo(t, root, "legacy")
	link := seedRepo(t, st, "lproj", "lr", "lproj", "lr")
	if _, err := st.Pool().Exec(ctx, "DELETE FROM git_repo_state WHERE repo_id = $1", link.RepoID); err != nil {
		t.Fatalf("drop referee row: %v", err)
	}

	mem := objectstore.NewMemStore()
	engine := gitengine.New(mem)
	mg := &gitmigrate.Migrator{
		Engine: engine,
		Cache: &gitengine.RepoCache{
			Root: filepath.Join(root, "cache"), Engine: engine, Packs: gitengine.NewBucketPackSource(mem),
		},
		Leases: dbLeases{st}, Store: st, Referee: dbReferee{st},
		CloneURL: func(owner, name string) string { return "file://" + src },
		WorkDir:  filepath.Join(root, "scratch"),
	}
	res, err := mg.MigrateRepo(ctx, link, gitmigrate.Options{SkipPRs: true, NoFreeze: true})
	if err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if res.Status != "migrated" {
		t.Fatalf("status = %q (%s)", res.Status, res.SkippedReason)
	}
	row, err := st.GetGitRepoState(ctx, link.RepoID)
	if err != nil {
		t.Fatalf("referee row after migrate: %v", err)
	}
	if row.TenantID == nil || *row.TenantID != *link.TenantID {
		t.Fatalf("referee row tenant = %v, want %v", row.TenantID, *link.TenantID)
	}
	if len(row.Refs) == 0 || row.Revision == 0 {
		t.Fatalf("referee row not committed: %+v", row)
	}
}
