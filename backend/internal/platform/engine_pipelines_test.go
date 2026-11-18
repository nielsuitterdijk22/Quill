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
	"github.com/nielsuitterdijk22/quill/internal/store/db"
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
	svc, act, s, head, id, _ := enginePipelineRepoURL(t, slug)
	return svc, act, s, head, id
}

// enginePipelineRepoURL is enginePipelineRepo plus the git base URL, for
// tests that push more branches.
func enginePipelineRepoURL(t *testing.T, slug string) (*platform.Service, platform.Actor, string, string, uuid.UUID, string) {
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
	if err := st.SetTenantPlan(ctx, db.SetTenantPlanParams{ID: tenant.ID, Plan: platform.PlanTeam}); err != nil {
		t.Fatalf("activate tenant: %v", err)
	}
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
	return svc, act, slug, head, repo.ID, gitURL
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

// Merging a pull request moves the base branch exactly like a push does, so
// the workflows listening for a push on the base branch must run for the merge
// commit (GitHub semantics). Without this a repo deploying on push to main
// never deploys a merged PR.
func TestEngineMergeTriggersPushPipelines(t *testing.T) {
	svc, act, slug, _, _, gitURL := enginePipelineRepoURL(t, "engpipe-m")
	ctx := context.Background()

	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", gitURL+"/"+slug+"/app", work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	browseGit(t, work, "checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(work, "feature.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "feature")
	if out, err := browseGitPush(t, work, "origin", "feature"); err != nil {
		t.Fatalf("push feature: %v\n%s", err, out)
	}
	if _, _, err := svc.CreatePull(ctx, act, slug, "app", platform.CreatePullInput{
		Title: "Feature", Head: "feature", Base: "main",
	}); err != nil {
		t.Fatalf("CreatePull: %v", err)
	}
	_, merged, err := svc.MergePull(ctx, act, slug, "app", 1, "merge")
	if err != nil {
		t.Fatalf("MergePull: %v", err)
	}
	if merged.MergeCommit == "" {
		t.Fatalf("merged pull has no merge commit: %+v", merged)
	}

	_, runs, _, err := svc.ListRuns(ctx, act, slug, "app")
	if err != nil {
		t.Fatalf("ListRuns: %v", err)
	}
	for _, r := range runs {
		if r.Event == "push" && r.CommitSha == merged.MergeCommit && strings.HasSuffix(r.Ref, "main") {
			return
		}
	}
	t.Fatalf("no push run on main for merge commit %s; runs = %+v", merged.MergeCommit, runs)
}

// Push filters are honoured (2026-11-15): a workflow limited to
// `branches: [main]` — a deploy — does not run for a push to another branch,
// while an unfiltered workflow does.
func TestEnginePushHonoursBranchFilters(t *testing.T) {
	svc, _, slug, _, repoID, gitURL := enginePipelineRepoURL(t, "engpipe-f")
	ctx := context.Background()
	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", gitURL+"/"+slug+"/app", work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	deploy := "name: deploy\non:\n  push:\n    branches: [main]\njobs:\n  d:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo d\n"
	if err := os.WriteFile(filepath.Join(work, ".github/workflows/deploy.yml"), []byte(deploy), 0o644); err != nil {
		t.Fatal(err)
	}
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "deploy workflow")
	mainHead := browseGit(t, work, "rev-parse", "HEAD")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push: %v\n%s", err, out)
	}
	if out, err := browseGitPush(t, work, "origin", "main:feature"); err != nil {
		t.Fatalf("push feature: %v\n%s", err, out)
	}
	count := func(ref string) int {
		t.Helper()
		runs, err := svc.HandleEnginePush(ctx, repoID, []platform.EngineRefUpdate{{Ref: ref, New: mainHead}})
		if err != nil {
			t.Fatalf("HandleEnginePush %s: %v", ref, err)
		}
		return len(runs)
	}
	if n := count("refs/heads/feature"); n != 1 {
		t.Fatalf("feature push started %d runs, want 1 (ci only, not the main-only deploy)", n)
	}
	if n := count("refs/heads/main"); n != 2 {
		t.Fatalf("main push started %d runs, want 2", n)
	}
}

// pull_request workflows run on Quill's own git engine (they never did before
// 2026-11-16: only pushes triggered anything, so a repo with `on:
// pull_request` CI got no checks on its pull requests): when the PR is opened,
// and again when its source branch is pushed to. `types` and the base-branch
// filters apply, and the run builds the head commit with the PR in its event.
func TestEnginePullRequestTriggersPipelines(t *testing.T) {
	svc, act, slug, _, repoID, gitURL := enginePipelineRepoURL(t, "engpipe-pr")
	ctx := context.Background()

	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", gitURL+"/"+slug+"/app", work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	wf := func(name, on string) {
		body := "name: " + name + "\non:\n" + on + "\njobs:\n  j:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo " + name + "\n"
		if err := os.WriteFile(filepath.Join(work, ".github/workflows/"+name+".yml"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wf("pr-any", "  pull_request:")
	wf("pr-main", "  pull_request:\n    branches: [main]")
	wf("pr-develop", "  pull_request:\n    branches: [develop]")
	wf("pr-opened-only", "  pull_request:\n    types: [opened]")
	browseGit(t, work, "add", "-A")
	browseGit(t, work, "commit", "-qm", "pr workflows")
	if out, err := browseGitPush(t, work, "origin", "main"); err != nil {
		t.Fatalf("push main: %v\n%s", err, out)
	}

	commit := func(file string) string {
		t.Helper()
		if err := os.WriteFile(filepath.Join(work, file), []byte(file+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		browseGit(t, work, "add", "-A")
		browseGit(t, work, "commit", "-qm", file)
		return browseGit(t, work, "rev-parse", "HEAD")
	}
	browseGit(t, work, "checkout", "-q", "-b", "feature")
	head1 := commit("one.txt")
	if out, err := browseGitPush(t, work, "origin", "feature"); err != nil {
		t.Fatalf("push feature: %v\n%s", err, out)
	}

	// A push before any PR exists starts no pull_request run.
	if _, err := svc.HandleEnginePush(ctx, repoID, []platform.EngineRefUpdate{{Ref: "refs/heads/feature", New: head1}}); err != nil {
		t.Fatal(err)
	}
	prRuns := func() map[string]int {
		t.Helper()
		_, runs, paths, err := svc.ListRuns(ctx, act, slug, "app")
		if err != nil {
			t.Fatalf("ListRuns: %v", err)
		}
		out := map[string]int{}
		for _, r := range runs {
			if r.Event == "pull_request" {
				out[strings.TrimSuffix(filepath.Base(paths[r.PipelineID]), ".yml")]++
			}
		}
		return out
	}
	if got := prRuns(); len(got) != 0 {
		t.Fatalf("pull_request runs before any PR: %v", got)
	}

	// Opening the PR runs the matching workflows once each (develop-only is
	// filtered out by the base branch).
	if _, _, err := svc.CreatePull(ctx, act, slug, "app", platform.CreatePullInput{Title: "Feature", Head: "feature", Base: "main"}); err != nil {
		t.Fatalf("CreatePull: %v", err)
	}
	want := map[string]int{"pr-any": 1, "pr-main": 1, "pr-opened-only": 1}
	if got := prRuns(); !mapsEqual(got, want) {
		t.Fatalf("runs after opening = %v, want %v", got, want)
	}
	// The run builds the head commit and carries the PR in its spec.
	_, runs, paths, _ := svc.ListRuns(ctx, act, slug, "app")
	var prAny platform.QueuedJob
	for _, r := range runs {
		if r.Event == "pull_request" && strings.HasSuffix(paths[r.PipelineID], "pr-any.yml") {
			if r.CommitSha != head1 || !strings.HasSuffix(r.Ref, "feature") {
				t.Fatalf("pr run = %+v, want head %s on feature", r, head1)
			}
			prAny = *claimRunJob(t, svc, r.ID)
		}
	}
	if pr := prAny.Spec.PullRequest; pr == nil || pr.Number != 1 || pr.BaseRef != "main" || pr.HeadRef != "feature" || pr.Action != "opened" {
		t.Fatalf("spec pull request = %+v", prAny.Spec.PullRequest)
	}

	// Pushing to the source branch re-runs them as "synchronize" — except
	// the opened-only workflow.
	head2 := commit("two.txt")
	if out, err := browseGitPush(t, work, "origin", "feature"); err != nil {
		t.Fatalf("push feature 2: %v\n%s", err, out)
	}
	if _, err := svc.HandleEnginePush(ctx, repoID, []platform.EngineRefUpdate{{Ref: "refs/heads/feature", New: head2}}); err != nil {
		t.Fatal(err)
	}
	want = map[string]int{"pr-any": 2, "pr-main": 2, "pr-opened-only": 1}
	if got := prRuns(); !mapsEqual(got, want) {
		t.Fatalf("runs after push = %v, want %v", got, want)
	}

	// A push to the BASE branch updates no PR.
	if _, err := svc.HandleEnginePush(ctx, repoID, []platform.EngineRefUpdate{{Ref: "refs/heads/main", New: head2}}); err != nil {
		t.Fatal(err)
	}
	if got := prRuns(); !mapsEqual(got, want) {
		t.Fatalf("runs after a base push = %v, want unchanged %v", got, want)
	}
}

func mapsEqual(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}
