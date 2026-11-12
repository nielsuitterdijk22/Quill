package platform_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/gitengine"
	"github.com/nielsuitterdijk22/quill/internal/objectstore"
	"github.com/nielsuitterdijk22/quill/internal/platform"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Git endpoint auth/tenancy (serverless epic, PR B6). These integration tests
// drive the production wiring shape — Basic credentials validated by the
// platform (native qgt_ tokens locally, provider PATs by introspection),
// per-actor repository resolution, and tenant-stamped push metering — against
// a real smart-HTTP mount. DB-gated on QUILL_TEST_DATABASE_URL; push/clone
// runs need a git binary.

// gitAuthFixture wires an own-engine service plus a smart-HTTP server that
// mirrors the production /git mount (auth middleware → ResolveRepoForGit
// Access → hydrate; receive seams → referee/guard/leases; pushes audited).
// Returns the service, store, git URL base, and the engine (for manifest
// inspection).
func gitAuthFixture(t *testing.T) (*platform.Service, *store.Store, *gitengine.Engine, string) {
	t.Helper()
	mem := objectstore.NewMemStore()
	engine := gitengine.New(mem)
	cache := &gitengine.RepoCache{Root: t.TempDir(), Engine: engine, Packs: gitengine.NewBucketPackSource(mem)}
	svc, st := newService(t)
	svc = svc.WithGitEngine(engine).WithGitCache(cache)

	deny := func(ga platform.GitActor, err error) error {
		switch {
		case err == nil:
			return nil
		case errors.Is(err, platform.ErrGitUnauthorized):
			return gitengine.ErrGitUnauthorized
		case !ga.Authenticated:
			// Anonymous denial → Basic challenge so git prompts for
			// credentials (mirror of the server's gitAccessErr).
			return gitengine.ErrGitUnauthorized
		case errors.Is(err, platform.ErrForbidden):
			return gitengine.ErrGitForbidden
		case errors.Is(err, platform.ErrNotFound):
			return gitengine.ErrGitNotFound
		default:
			return err
		}
	}

	gitAuth := func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ga, err := svc.AuthenticateGitRequest(r.Context(), r)
			if err != nil {
				w.Header().Set("WWW-Authenticate", `Basic realm="quill-git"`)
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), gitAuthActor{}, ga)))
		})
	}
	actorFrom := func(ctx context.Context) platform.GitActor {
		ga, _ := ctx.Value(gitAuthActor{}).(platform.GitActor)
		return ga
	}
	resolve := func(ctx context.Context, owner, repo string, wantPush bool) (store.GitRepoRef, platform.GitActor, error) {
		ga := actorFrom(ctx)
		ref, err := svc.ResolveRepoForGitAccess(ctx, ga, owner, repo, wantPush)
		return ref, ga, err
	}
	hydrate := func(ctx context.Context, ref store.GitRepoRef) (string, error) {
		return cache.Hydrate(ctx, ref.TenantID.String(), ref.ProjectSlug, ref.RepoSlug)
	}

	h := &gitengine.SmartHTTP{
		Hydrate: func(ctx context.Context, owner, repo string) (string, error) {
			ref, ga, err := resolve(ctx, owner, repo, false)
			if err != nil {
				return "", deny(ga, err)
			}
			return hydrate(ctx, ref)
		},
		Receive: &gitengine.ReceiveConfig{
			Engine: engine,
			Resolve: func(ctx context.Context, owner, repo string) (gitengine.ReceiveContext, error) {
				ref, ga, err := resolve(ctx, owner, repo, true)
				if err != nil {
					return gitengine.ReceiveContext{}, deny(ga, err)
				}
				dir, err := hydrate(ctx, ref)
				if err != nil {
					return gitengine.ReceiveContext{}, err
				}
				return gitengine.ReceiveContext{
					ID: ref.RepoID.String(), Dir: dir,
					TenantID: ref.TenantID.String(), ProjectSlug: ref.ProjectSlug, RepoSlug: ref.RepoSlug,
					ActorID: ga.UserID.String(),
				}, nil
			},
			Actor: func(r *http.Request) string {
				if ga := actorFrom(r.Context()); ga.Authenticated {
					return ga.Username
				}
				return "anonymous"
			},
			Referee: svc.GitReferee(),
			Guard:   svc.GitPushGuard(),
			Leases:  svc.GitPushLeases(),
			OnPushSuccess: func(rcx gitengine.ReceiveContext, act string) {
				audCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				tenantID := uuid.Nil
				if id, err := uuid.Parse(rcx.TenantID); err == nil {
					tenantID = id
				}
				svc.LogGitPush(audCtx, tenantID, uuid.NullUUID{UUID: ga2id(rcx), Valid: true}, act, rcx.ProjectSlug, rcx.RepoSlug, "")
			},
		},
	}
	srv := httptest.NewServer(http.StripPrefix("/git", gitAuth(http.Handler(h))))
	t.Cleanup(srv.Close)
	return svc, st, engine, srv.URL + "/git"
}

type gitAuthActor struct{}

func ga2id(rcx gitengine.ReceiveContext) uuid.UUID {
	id, err := uuid.Parse(rcx.ActorID)
	if err != nil {
		return uuid.Nil
	}
	return id
}

// gitAuthGet performs a GET against the mounted git surface with optional
// Basic credentials (empty user = anonymous).
func gitAuthGet(t *testing.T, url, user, pass string) *http.Response {
	t.Helper()
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatal(err)
	}
	if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

// pushMeter reads today's push count for the tenant (missing row = 0).
func pushMeter(t *testing.T, st *store.Store, tenantID uuid.UUID) int {
	t.Helper()
	var n int
	err := st.Pool().QueryRow(context.Background(),
		"SELECT pushes FROM git_push_meters WHERE tenant_id = $1 AND day = current_date", tenantID).
		Scan(&n)
	if err != nil {
		if strings.Contains(err.Error(), "no rows") {
			return 0
		}
		t.Fatalf("read push meter: %v", err)
	}
	return n
}

func auditRows(t *testing.T, st *store.Store, action string) int {
	t.Helper()
	var n int
	if err := st.Pool().QueryRow(context.Background(),
		"SELECT count(*) FROM audit_log WHERE action = $1", action).Scan(&n); err != nil {
		t.Fatalf("count audit rows: %v", err)
	}
	return n
}

// TestGitAuthAccessMatrix pins the read/push denial semantics over the real
// smart-HTTP mount: anonymous reads public only (private → Basic challenge),
// members read everything, pushes need an authenticated write-level role, and
// invalid credentials never leak anything but a 401.
func TestGitAuthAccessMatrix(t *testing.T) {
	browseGitAvailable(t)
	svc, st, _, gitURL := gitAuthFixture(t)
	ctx := context.Background()

	owner := makeUser(t, st, "auth-owner")
	act := actor(owner)
	project, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: "auth-acme", Name: "auth acme"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, act, "auth-acme", platform.CreateRepoInput{Slug: "publicrepo", Name: "public", Visibility: "public"}); err != nil {
		t.Fatalf("create public repo: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, act, "auth-acme", platform.CreateRepoInput{Slug: "privaterepo", Name: "private"}); err != nil {
		t.Fatalf("create private repo: %v", err)
	}

	cred, err := svc.CreateGitToken(ctx, act, "test token")
	if err != nil {
		t.Fatalf("mint git token: %v", err)
	}

	// A read-only member: may read, may not push.
	reader := makeUser(t, st, "auth-reader")
	if err := st.AddProjectMember(ctx, db.AddProjectMemberParams{ProjectID: project.ID, UserID: reader, Role: "read"}); err != nil {
		t.Fatalf("add read member: %v", err)
	}
	readerCred, err := svc.CreateGitToken(ctx, actor(reader), "reader token")
	if err != nil {
		t.Fatalf("mint reader token: %v", err)
	}

	// A stranger with their own valid token: no membership at all.
	stranger := makeUser(t, st, "auth-stranger")
	strangerCred, err := svc.CreateGitToken(ctx, actor(stranger), "stranger token")
	if err != nil {
		t.Fatalf("mint stranger token: %v", err)
	}

	readURL := func(repo string) string { return gitURL + "/auth-acme/" + repo + "/info/refs?service=git-upload-pack" }
	pushURL := func(repo string) string { return gitURL + "/auth-acme/" + repo + "/info/refs?service=git-receive-pack" }

	cases := []struct {
		name     string
		url      string
		user     string
		pass     string
		wantCode int
		wantAuth bool // expect a Basic WWW-Authenticate challenge
	}{
		{"anonymous public read", readURL("publicrepo"), "", "", 200, false},
		{"anonymous private read challenges", readURL("privaterepo"), "", "", 401, true},
		{"anonymous unknown repo challenges", readURL("missing"), "", "", 401, true},
		{"owner private read", readURL("privaterepo"), cred.Username, cred.Token, 200, false},
		{"reader private read", readURL("privaterepo"), readerCred.Username, readerCred.Token, 200, false},
		{"stranger private read denied", readURL("privaterepo"), strangerCred.Username, strangerCred.Token, 403, false},
		{"stranger unknown repo 404s", readURL("missing"), strangerCred.Username, strangerCred.Token, 404, false},
		{"invalid token 401", readURL("publicrepo"), "anyone", "qgt_not-a-token", 401, true},
		{"anonymous push challenges", pushURL("publicrepo"), "", "", 401, true},
		{"reader push denied", pushURL("publicrepo"), readerCred.Username, readerCred.Token, 403, false},
		{"owner push advertisement", pushURL("publicrepo"), cred.Username, cred.Token, 200, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := gitAuthGet(t, tc.url, tc.user, tc.pass)
			if resp.StatusCode != tc.wantCode {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tc.wantCode)
			}
			if got := resp.Header.Get("WWW-Authenticate"); (got != "") != tc.wantAuth {
				t.Fatalf("WWW-Authenticate = %q, want challenge: %v", got, tc.wantAuth)
			}
		})
	}
}

// TestGitAuthPushIdentityAndMetering pushes with a native token end-to-end
// and verifies the B6 trail: manifest push_history records the authenticated
// username, an audit row lands, and the tenant's daily push meter ticks.
// Revoking the token kills the next push.
func TestGitAuthPushIdentityAndMetering(t *testing.T) {
	browseGitAvailable(t)
	svc, st, engine, gitURL := gitAuthFixture(t)
	ctx := context.Background()

	owner := makeUser(t, st, "push-owner")
	act := actor(owner)
	project, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: "push-acme", Name: "push acme"})
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, act, "push-acme", platform.CreateRepoInput{Slug: "metered", Name: "metered", Visibility: "public"}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	cred, err := svc.CreateGitToken(ctx, act, "push token")
	if err != nil {
		t.Fatalf("mint git token: %v", err)
	}

	authedURL := gitURL + "/push-acme/metered"
	credURL := "http://" + url.QueryEscape(cred.Username) + ":" + cred.Token + "@" +
		strings.TrimPrefix(authedURL, "http://")

	// Build a local repo with one commit and push it with credentials.
	work := t.TempDir()
	browseGit(t, work, "init", "-q", "--initial-branch", "main")
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("metered\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "first")
	beforeMeter := pushMeter(t, st, project.TenantID)
	beforeAudit := auditRows(t, st, "git.push")
	if out, err := browseGitPush(t, work, credURL, "main"); err != nil {
		t.Fatalf("credentialed push: %v\n%s", err, out)
	}

	// Manifest records WHO pushed.
	m, _, err := engine.LoadManifest(ctx, project.TenantID.String(), "push-acme", "metered")
	if err != nil {
		t.Fatalf("load manifest: %v", err)
	}
	if len(m.PushHistory) != 1 || m.PushHistory[0].PushedBy != "push-owner" {
		t.Fatalf("push_history = %+v, want push-owner", m.PushHistory)
	}
	if m.PushHistory[0].TenantID != project.TenantID.String() {
		t.Fatalf("push_history tenant = %q, want %q", m.PushHistory[0].TenantID, project.TenantID)
	}
	if m.PushHistory[0].PushedSha == "" {
		t.Fatal("push_history has no sha")
	}

	// Tenant metering + audit trail ticked.
	if got := pushMeter(t, st, project.TenantID); got != beforeMeter+1 {
		t.Fatalf("push meter = %d, want %d", got, beforeMeter+1)
	}
	if got := auditRows(t, st, "git.push"); got != beforeAudit+1 {
		t.Fatalf("git.push audit rows = %d, want %d", got, beforeAudit+1)
	}

	// Revoking the token kills future pushes.
	if err := svc.RevokeGitToken(ctx, act, cred.ID); err != nil {
		t.Fatalf("revoke git token: %v", err)
	}
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("second\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	browseGit(t, work, "commit", "-aqm", "second")
	if out, err := browseGitPush(t, work, credURL, "main"); err == nil {
		t.Fatalf("push with revoked token succeeded:\n%s", out)
	}

	// Anonymous clone of the public repo still works (read-only world).
	anon := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", authedURL, anon).CombinedOutput(); err != nil {
		t.Fatalf("anonymous clone: %v\n%s", err, out)
	}
	got, err := os.ReadFile(filepath.Join(anon, "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "metered\n" {
		t.Fatalf("cloned content = %q", got)
	}
}
