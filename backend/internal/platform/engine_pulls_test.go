package platform_test

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/platform"
)

// B5 (serverless epic): PR/issue model on engine-backed repos. The lifecycle
// test drives a real repo through the mounted smart-HTTP stack, then the full
// PR flow — create, read, diff, commits, comments, reviews, the policy gate,
// and a squash merge that lands on main through the B3 referee/manifest
// primitives — plus the conflict path.

func TestEnginePullLifecycle(t *testing.T) {
	browseGitAvailable(t)
	svc, st, gitURL := browseFixture(t)
	ctx := context.Background()
	owner := makeUser(t, st, "pull-owner")
	act := actor(owner)
	reviewer := makeUser(t, st, "pull-reviewer")
	revAct := platform.Actor{UserID: reviewer, IsAdmin: true}

	if _, err := svc.CreateProject(ctx, act, platform.CreateProjectInput{Slug: "pull-acme", Name: "acme"}); err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := svc.CreateRepo(ctx, act, "pull-acme", platform.CreateRepoInput{Slug: "pr-repo", Name: "pr-repo"}); err != nil {
		t.Fatalf("create repo: %v", err)
	}
	url := gitURL + "/pull-acme/pr-repo"

	work := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", url, work).CombinedOutput(); err != nil {
		t.Fatalf("clone: %v\n%s", err, out)
	}
	writeCommit := func(rel, content, msg string) {
		t.Helper()
		if err := os.MkdirAll(work+"/"+dirOf(rel), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(work+"/"+rel, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		browseGit(t, work, "add", "-A")
		browseGit(t, work, "commit", "-qm", msg)
	}
	push := func(args ...string) {
		t.Helper()
		if out, err := browseGitPush(t, work, args...); err != nil {
			t.Fatalf("push %v: %v\n%s", args, err, out)
		}
	}

	// main base commit; feature branch with two commits.
	writeCommit("README.md", "# pr repo\nline one\n", "base commit")
	push("origin", "main")
	browseGit(t, work, "checkout", "-q", "-b", "feature")
	writeCommit("docs/app.md", "# docs\nfeature work\n", "feature: docs")
	writeCommit("src/main.go", "package main\n", "feature: main.go")
	push("origin", "feature")
	featureTip := browseGit(t, work, "rev-parse", "HEAD")
	browseGit(t, work, "checkout", "-q", "main")

	// ---- create + read -----------------------------------------------------
	if _, _, err := svc.CreatePull(ctx, act, "pull-acme", "pr-repo", platform.CreatePullInput{
		Title: "Add docs", Body: "the docs for the feature", Head: "feature", Base: "main",
	}); err != nil {
		t.Fatalf("CreatePull: %v", err)
	}
	repo, dto, err := svc.GetPull(ctx, act, "pull-acme", "pr-repo", 1)
	if err != nil {
		t.Fatalf("GetPull: %v", err)
	}
	if repo.Slug != "pr-repo" || dto.Number != 1 || dto.Title != "Add docs" {
		t.Fatalf("pull dto = %+v", dto)
	}
	if dto.State != "open" || dto.Merged {
		t.Fatalf("fresh pull state = %+v", dto)
	}
	if dto.Head.SHA != featureTip || dto.Head.Ref != "feature" || dto.Base.Ref != "main" {
		t.Fatalf("pull refs = %+v", dto)
	}
	if !dto.Mergeable {
		t.Fatal("fresh pull must be mergeable")
	}
	if dto.ChangedFiles != 2 || dto.Additions == 0 {
		t.Fatalf("pull stats = adds %d dels %d files %d", dto.Additions, dto.Deletions, dto.ChangedFiles)
	}

	_, files, err := svc.GetPullDiff(ctx, act, "pull-acme", "pr-repo", 1)
	if err != nil {
		t.Fatalf("GetPullDiff: %v", err)
	}
	var sawDocs, sawMain bool
	for _, f := range files {
		switch f.Path {
		case "docs/app.md":
			sawDocs = true
		case "src/main.go":
			sawMain = true
		}
	}
	if !sawDocs || !sawMain {
		t.Fatalf("diff files = %+v", files)
	}

	_, commits, err := svc.ListPullCommits(ctx, act, "pull-acme", "pr-repo", 1)
	if err != nil {
		t.Fatalf("ListPullCommits: %v", err)
	}
	if len(commits) != 2 || commits[0].SHA != featureTip {
		t.Fatalf("pull commits = %+v", commits)
	}

	// Numbering: the next PR in this repo gets number 2.
	if _, _, err := svc.GetPull(ctx, act, "pull-acme", "pr-repo", 2); err != platform.ErrNotFound {
		t.Fatalf("PR #2 before creation = %v, want ErrNotFound", err)
	}

	// ---- comments + line comments ------------------------------------------
	if _, err := svc.CreatePullComment(ctx, act, "pull-acme", "pr-repo", 1, "looks good to me"); err != nil {
		t.Fatalf("CreatePullComment: %v", err)
	}
	comments, err := svc.ListPullComments(ctx, act, "pull-acme", "pr-repo", 1)
	if err != nil || len(comments) != 1 || comments[0].Body != "looks good to me" {
		t.Fatalf("comments = %+v %v", comments, err)
	}
	lc, err := svc.CreateLineComment(ctx, act, "pull-acme", "pr-repo", 1, "docs/app.md", 1, "typo here?")
	if err != nil {
		t.Fatalf("CreateLineComment: %v", err)
	}
	if lc.Path != "docs/app.md" || lc.Line != 1 || lc.Author == nil || lc.Author.Login == "" {
		t.Fatalf("line comment = %+v", lc)
	}
	_, lineComments, err := svc.ListLineComments(ctx, act, "pull-acme", "pr-repo", 1)
	if err != nil || len(lineComments) != 1 || lineComments[0].Body != "typo here?" {
		t.Fatalf("line comments = %+v %v", lineComments, err)
	}

	// ---- reviews + policy gate ---------------------------------------------
	_, state, err := svc.ReviewsAndState(ctx, act, "pull-acme", "pr-repo", 1)
	if err != nil {
		t.Fatalf("ReviewsAndState: %v", err)
	}
	if state.Applies {
		t.Fatalf("no policy yet, gate = %+v", state)
	}
	if _, err := svc.SetProjectBranchPolicy(ctx, act, "pull-acme", platform.BranchPolicyInput{
		Pattern: "main", RequiredApprovals: 1,
	}); err != nil {
		t.Fatalf("set policy: %v", err)
	}
	_, state, err = svc.ReviewsAndState(ctx, act, "pull-acme", "pr-repo", 1)
	if err != nil {
		t.Fatalf("ReviewsAndState: %v", err)
	}
	if !state.Applies || state.Approvals != 0 || !state.Blocked {
		t.Fatalf("gate before approval = %+v", state)
	}
	// Merge is blocked by the approvals policy.
	if _, _, err := svc.MergePull(ctx, act, "pull-acme", "pr-repo", 1, "squash"); err == nil {
		t.Fatal("merge must be blocked without approvals")
	} else if !strings.Contains(err.Error(), "approval") {
		t.Fatalf("blocked merge error = %v", err)
	}

	if _, err := svc.CreatePullReview(ctx, revAct, "pull-acme", "pr-repo", 1, "APPROVED", "ship it"); err != nil {
		t.Fatalf("CreatePullReview: %v", err)
	}
	reviews, state, err := svc.ReviewsAndState(ctx, act, "pull-acme", "pr-repo", 1)
	if err != nil || len(reviews) != 1 {
		t.Fatalf("reviews = %+v %v", reviews, err)
	}
	if reviews[0].State != "APPROVED" || reviews[0].User == nil || reviews[0].User.Login == "" {
		t.Fatalf("review = %+v", reviews[0])
	}
	if state.Approvals != 1 || state.Blocked {
		t.Fatalf("gate after approval = %+v", state)
	}

	// ---- merge (squash) -----------------------------------------------------
	_, merged, err := svc.MergePull(ctx, act, "pull-acme", "pr-repo", 1, "squash")
	if err != nil {
		t.Fatalf("MergePull: %v", err)
	}
	if !merged.Merged || merged.State != "closed" || merged.MergeCommit == "" {
		t.Fatalf("merged pull = %+v", merged)
	}
	if merged.MergedBy == nil || merged.MergedBy.Login == "" || merged.MergedAt == nil {
		t.Fatalf("merge attribution missing: %+v", merged)
	}

	// A fresh clone sees the squash commit on main with the feature content.
	dst := t.TempDir()
	if out, err := exec.Command("git", "clone", "-q", url, dst).CombinedOutput(); err != nil {
		t.Fatalf("clone after merge: %v\n%s", err, out)
	}
	body, err := os.ReadFile(dst + "/docs/app.md")
	if err != nil || !strings.Contains(string(body), "feature work") {
		t.Fatalf("merged docs/app.md = %q %v", body, err)
	}
	log := browseGit(t, dst, "log", "--format=%s", "-n", "3")
	if !strings.Contains(log, "(#1)") {
		t.Fatalf("squash commit message = %q", log)
	}

	// Double merge is rejected.
	if _, _, err := svc.MergePull(ctx, act, "pull-acme", "pr-repo", 1, "squash"); err == nil {
		t.Fatal("merging a closed pull must fail")
	}

	// ---- conflict path --------------------------------------------------------
	// A branch that edits README line one, and a main commit editing the
	// same line: the resulting PR must report unmergeable and refuse to merge.
	browseGit(t, work, "pull", "-q", "origin", "main")
	browseGit(t, work, "checkout", "-q", "-b", "conflicted")
	browseGit(t, work, "reset", "-q", "--hard", "origin/main")
	writeCommit("README.md", "# pr repo\nCONFLICT-BRANCH\n", "branch edit")
	push("origin", "conflicted")
	browseGit(t, work, "checkout", "-q", "main")
	browseGit(t, work, "reset", "-q", "--hard", "origin/main")
	writeCommit("README.md", "# pr repo\nCONFLICT-MAIN\n", "main edit")
	push("origin", "main")

	if _, _, err := svc.CreatePull(ctx, act, "pull-acme", "pr-repo", platform.CreatePullInput{
		Title: "Conflicting change", Head: "conflicted", Base: "main",
	}); err != nil {
		t.Fatalf("CreatePull conflict branch: %v", err)
	}
	_, conflict, err := svc.GetPull(ctx, act, "pull-acme", "pr-repo", 2)
	if err != nil {
		t.Fatalf("GetPull conflict: %v", err)
	}
	if conflict.Number != 2 || conflict.Mergeable {
		t.Fatalf("conflict pull = %+v, want number 2 unmergeable", conflict)
	}
	// Approve the conflicting PR too — the gate is not what must reject it,
	// the conflict is.
	if _, err := svc.CreatePullReview(ctx, revAct, "pull-acme", "pr-repo", 2, "APPROVED", ""); err != nil {
		t.Fatalf("CreatePullReview conflict: %v", err)
	}
	if _, _, err := svc.MergePull(ctx, act, "pull-acme", "pr-repo", 2, "merge"); err == nil {
		t.Fatal("conflicting merge must fail")
	} else if !strings.Contains(err.Error(), "conflict") {
		t.Fatalf("conflict merge error = %v", err)
	}
}

// dirOf returns the parent directory path of rel ("" for a root file).
func dirOf(rel string) string {
	if i := strings.LastIndex(rel, "/"); i >= 0 {
		return rel[:i]
	}
	return ""
}
