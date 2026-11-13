package platform_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// D2 cutover: pipelines for own-engine repos read workflows, commit SHAs and
// clone credentials from the engine (never Forgejo), CI jobs authenticate with
// a scoped, expiring run token (never an admin credential), and a push to the
// engine triggers the workflows that listen for it.

const enginePipelineYAML = "name: engine-ci\non: [push]\njobs:\n  build:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo engine\n"

// enginePipelineRepo provisions an engine repo with a pushed workflow and
// returns the service, the pushed head and the repo id.
func enginePipelineRepo(t *testing.T, slug string) (*platform.Service, platform.Actor, string, string, uuid.UUID) {
	t.Helper()
	browseGitAvailable(t)
	svc, st, gitURL := browseFixture(t)
	svc = svc.WithQueueMode().WithGitPublicURL("https://git.example.test").WithGitRunTokenSecret("run-token-secret")
	ctx := context.Background()
	owner := makeUser(t, st, slug+"-owner")
	act := actor(owner)
	// Each fixture is its own tenant so cross-tenant scoping is exercised.
	tenant, err := st.GetOrCreateTenantByExternalOrg(ctx, uuid.NewString(), slug+"-"+uuid.NewString()[:6])
	if err != nil {
		t.Fatalf("tenant: %v", err)
	}
	act.TenantID = tenant.ID
	if _, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: slug, Name: slug}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	repo, err := svc.CreateRepo(ctx, act, slug, platform.CreateRepoInput{Slug: "app", Name: "app"})
	if err != nil {
		t.Fatalf("create repo: %v", err)
	}
	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", gitURL+"/"+slug+"/app", work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	if err := os.MkdirAll(filepath.Join(work, ".github/workflows"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, ".github/workflows/ci.yml"), []byte(enginePipelineYAML), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(work, "README.md"), []byte("hi\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "ci")
	head := browseGit(t, work, "rev-parse", "HEAD")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	return svc, act, slug, head, repo.ID
}

// claimRunJob claims queue rows until it finds the one for runID (other tests
// may leave rows behind in the shared database).
func claimRunJob(t *testing.T, svc *platform.Service, runID uuid.UUID) *platform.QueuedJob {
	t.Helper()
	for i := 0; i < 50; i++ {
		job, err := svc.ClaimQueuedJob(context.Background(), "engine-pipeline-test")
		if err != nil {
			t.Fatalf("claim: %v", err)
		}
		if job == nil {
			break
		}
		if job.RunID == runID {
			return job
		}
	}
	t.Fatalf("no queued job for run %s", runID)
	return nil
}

func TestEnginePipelinesReadFromEngine(t *testing.T) {
	svc, act, slug, head, _ := enginePipelineRepo(t, "engpipe-a")
	ctx := context.Background()

	_, pls, err := svc.ListPipelines(ctx, act, slug, "app")
	if err != nil {
		t.Fatalf("ListPipelines: %v", err)
	}
	if len(pls) != 1 || pls[0].WorkflowPath != ".github/workflows/ci.yml" {
		t.Fatalf("pipelines = %+v", pls)
	}

	_, run, err := svc.TriggerRun(ctx, act, slug, "app", platform.TriggerInput{WorkflowPath: ".github/workflows/ci.yml"})
	if err != nil {
		t.Fatalf("TriggerRun: %v", err)
	}
	if run.CommitSha != head {
		t.Fatalf("run sha = %q, want %q", run.CommitSha, head)
	}
	job := claimRunJob(t, svc, run.ID)
	spec := job.Spec
	if !strings.Contains(spec.WorkflowYAML, "engine-ci") {
		t.Fatalf("workflow yaml not from engine: %q", spec.WorkflowYAML)
	}
	if spec.CloneURL != "https://git.example.test/"+slug+"/app.git" {
		t.Fatalf("clone url = %q", spec.CloneURL)
	}
	if spec.ServerURL != "https://git.example.test" {
		t.Fatalf("server url = %q", spec.ServerURL)
	}
	if spec.RepoFullName != slug+"/app" {
		t.Fatalf("full name = %q", spec.RepoFullName)
	}
	if !strings.HasPrefix(spec.Token, "qgr_") {
		t.Fatalf("token = %q, want a scoped qgr_ run token", spec.Token)
	}
	if !strings.Contains(spec.CloneAuthHeader, "Authorization: Basic ") {
		t.Fatalf("clone auth header = %q", spec.CloneAuthHeader)
	}
}

func TestEngineRunTokenScope(t *testing.T) {
	svc, act, slug, _, _ := enginePipelineRepo(t, "engpipe-b")
	ctx := context.Background()
	_, run, err := svc.TriggerRun(ctx, act, slug, "app", platform.TriggerInput{WorkflowPath: ".github/workflows/ci.yml"})
	if err != nil {
		t.Fatalf("TriggerRun: %v", err)
	}
	token := claimRunJob(t, svc, run.ID).Spec.Token

	req := httptest.NewRequest(http.MethodGet, "/git/x/y/info/refs", nil)
	req.SetBasicAuth("token", token) // act's go-git username
	ga, err := svc.AuthenticateGitRequest(ctx, req)
	if err != nil || !ga.Authenticated {
		t.Fatalf("run token rejected: %+v %v", ga, err)
	}
	if _, err := svc.ResolveRepoForGitAccess(ctx, ga, slug, "app", false); err != nil {
		t.Fatalf("run token read own-tenant repo: %v", err)
	}
	if _, err := svc.ResolveRepoForGitAccess(ctx, ga, slug, "app", true); err == nil {
		t.Fatal("run token must never push")
	}

	// A repo in ANOTHER tenant stays invisible to this run's token.
	other, _, otherSlug, _, _ := enginePipelineRepo(t, "engpipe-c")
	_ = other
	if _, err := svc.ResolveRepoForGitAccess(ctx, ga, otherSlug, "app", false); err == nil {
		t.Fatal("run token crossed a tenant boundary")
	}

	// Tampered and foreign-secret tokens fail.
	req.SetBasicAuth("token", token[:len(token)-2]+"00")
	if _, err := svc.AuthenticateGitRequest(ctx, req); err == nil {
		t.Fatal("tampered run token accepted")
	}
}

func TestEnginePushTriggersPipelines(t *testing.T) {
	svc, _, slug, head, repoID := enginePipelineRepo(t, "engpipe-d")
	ctx := context.Background()

	runs, err := svc.HandleEnginePush(ctx, repoID, []platform.EngineRefUpdate{
		{Ref: "refs/heads/main", New: head},
		{Ref: "refs/heads/gone", New: ""}, // deletion: never triggers
	})
	if err != nil {
		t.Fatalf("HandleEnginePush: %v", err)
	}
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1 (push on main)", len(runs))
	}
	if runs[0].Event != "push" || runs[0].CommitSha != head || !strings.HasSuffix(runs[0].Ref, "main") {
		t.Fatalf("run = %+v", runs[0])
	}
	job := claimRunJob(t, svc, runs[0].ID)
	if job.Spec.Event != "push" || !strings.Contains(job.Spec.CloneURL, slug+"/app.git") {
		t.Fatalf("spec = %+v", job.Spec)
	}
}
