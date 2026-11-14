package platform

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/gitengine"
	"github.com/nielsuitterdijk22/quill/internal/store"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Engine-backed pull requests (serverless epic, PR B5). The PR/issue model
// lives in Postgres (store/pulls.go); the git facts (mergeability, diffs,
// commits) come from upstream git in the hydrated repo; merges write through
// the SAME B3 primitives a push uses — write lease, referee CAS first,
// manifest second. Responses keep the forgejo DTO shapes, so the frontend is
// unchanged.

// resolvePullRepo is the member-authorized, backend-discriminated resolver
// for the PR surface (public-read bypass does not apply here, matching the
// forgejo path's resolveRepo semantics).
func (s *Service) resolvePullRepo(ctx context.Context, actor Actor, projectSlug, repoSlug string) (db.Repository, gitBackend, error) {
	project, err := s.getProject(ctx, projectSlug)
	if err != nil {
		return db.Repository{}, gitBackend{}, err
	}
	repo, err := s.store.GetRepositoryBySlug(ctx, db.GetRepositoryBySlugParams{
		ProjectID: project.ID, Lower: normalizeSlug(repoSlug),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Repository{}, gitBackend{}, ErrNotFound
	}
	if err != nil {
		return db.Repository{}, gitBackend{}, fmt.Errorf("lookup repo: %w", err)
	}
	if err := s.authorizeProjectMember(ctx, actor, project.ID); err != nil {
		return db.Repository{}, gitBackend{}, err
	}
	_, stErr := s.store.GetGitRepoState(ctx, repo.ID)
	switch {
	case stErr == nil:
		if s.gitCache == nil || s.gitEngine == nil {
			// referee row but engine unwired: fail closed (config regression)
			return db.Repository{}, gitBackend{}, ErrUnavailable
		}
		return repo, gitBackend{engine: true, repoID: repo.ID, tenantID: project.TenantID,
			projectSlug: project.Slug, repoSlug: repo.Slug}, nil
	case errors.Is(stErr, pgx.ErrNoRows):
		if !s.forgejoEnabled() {
			return db.Repository{}, gitBackend{}, ErrUnavailable
		}
		owner, name, ok := forgejoTarget(repo, project)
		if !ok {
			return db.Repository{}, gitBackend{}, ErrUnavailable
		}
		return repo, gitBackend{owner: owner, name: name}, nil
	default:
		return db.Repository{}, gitBackend{}, fmt.Errorf("lookup git state: %w", stErr)
	}
}

// ---- small git helpers unique to the PR surface -----------------------------

// mergeTree runs git's own merge computation without touching any ref:
// it returns the merged tree when head merges cleanly into base and ok=false
// on conflicts (exit 1, conflicts reported in-band).
func mergeTree(dir, base, head string) (tree string, clean bool, err error) {
	cmd := exec.Command("git", "merge-tree", "--write-tree", head, base)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err = cmd.Run()
	if err == nil {
		return strings.Fields(out.String())[0], true, nil
	}
	if ee, ok := err.(*exec.ExitError); ok && ee.ExitCode() == 1 {
		return "", false, nil
	}
	return "", false, fmt.Errorf("git merge-tree: %w: %s", err, errOut.String())
}

// revParseRef resolves refs/heads/<name> to a sha; ok is false when the ref
// does not exist (a deleted head branch must not 404 the whole PR).
func revParseRef(dir, branch string) (sha string, ok bool, err error) {
	cmd := exec.Command("git", "rev-parse", "--verify", "refs/heads/"+branch)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		if notFoundGitMsg(errOut.String()) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("git rev-parse %s: %w: %s", branch, err, errOut.String())
	}
	return strings.TrimSpace(out.String()), true, nil
}

// gitCmdEnv returns the git author/committer identity env for a Quill user.
func gitCmdEnv(name, email string) []string {
	return append(os.Environ(),
		"GIT_AUTHOR_NAME="+name, "GIT_AUTHOR_EMAIL="+email,
		"GIT_COMMITTER_NAME="+name, "GIT_COMMITTER_EMAIL="+email)
}

// actorProfile returns the acting user's login + email for git identity and
// display snapshots.
func (s *Service) actorProfile(ctx context.Context, actor Actor) (login, email string, err error) {
	u, err := s.store.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return "", "", fmt.Errorf("load acting user: %w", err)
	}
	name := u.DisplayName
	if name == "" {
		name = u.Username
	}
	return name, u.Email, nil
}

// pullStats computes (additions, deletions, changedFiles) via numstat/name-only
// between the PR's merge base and head.
func pullStats(dir, base, head string) (adds, dels, files int, err error) {
	numstat, err := gitBrowse(dir, "diff", "--numstat", base+"..."+head)
	if err != nil {
		return 0, 0, 0, err
	}
	for _, line := range strings.Split(strings.TrimRight(numstat, "\n"), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		a, _ := strconv.Atoi(fields[0])
		d, _ := strconv.Atoi(fields[1])
		if fields[0] == "-" { // binary
			continue
		}
		adds += a
		dels += d
		files++
	}
	return adds, dels, files, nil
}

// enginePullDTO maps one stored pull to the forgejo DTO the frontend renders,
// refreshing the git facts (shas, mergeability, stats) from the hydrated dir.
func (s *Service) enginePullDTO(ctx context.Context, repo db.Repository, be gitBackend, dir string, pr store.PullRequest, live bool) (forgejo.PullRequest, error) {
	headSHA, headOK, err := revParseRef(dir, pr.HeadRef)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	baseSHA, baseOK, err := revParseRef(dir, pr.BaseRef)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	dto := forgejo.PullRequest{
		Number:    pr.Number,
		Title:     pr.Title,
		Body:      pr.Body,
		State:     pr.State,
		Merged:    pr.Merged,
		User:      &forgejo.User{Login: pr.CreatedByName, FullName: pr.CreatedByName, Type: "User"},
		Head:      forgejo.PRRef{Label: pr.HeadRef, Ref: pr.HeadRef, SHA: pr.HeadSHA},
		Base:      forgejo.PRRef{Label: pr.BaseRef, Ref: pr.BaseRef, SHA: pr.BaseSHA},
		CreatedAt: pr.CreatedAt,
		UpdatedAt: pr.UpdatedAt,
	}
	if pr.Merged || pr.MergedByName != "" {
		dto.MergedBy = &forgejo.User{Login: pr.MergedByName, FullName: pr.MergedByName, Type: "User"}
	}
	if pr.MergedAt != nil {
		dto.MergedAt = pr.MergedAt
	}
	if pr.MergeCommit != "" {
		dto.MergeCommit = pr.MergeCommit
	}
	n, err := s.store.PullCommentCount(ctx, pr.ID)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	dto.Comments = n
	if headOK {
		dto.Head.SHA = headSHA
	}
	if baseOK {
		dto.Base.SHA = baseSHA
	}
	dto.Mergeable = pr.State == "open" && headOK && baseOK
	if live && dto.Mergeable {
		_, clean, err := mergeTree(dir, baseSHA, headSHA)
		if err != nil {
			return forgejo.PullRequest{}, err
		}
		dto.Mergeable = clean
		adds, dels, files, err := pullStats(dir, baseSHA, headSHA)
		if err != nil {
			return forgejo.PullRequest{}, err
		}
		dto.Additions, dto.Deletions, dto.ChangedFiles = adds, dels, files
	}
	_ = repo
	_ = be
	return dto, nil
}

// reviewDTO maps a stored review row to the forgejo DTO the gate consumes.
func reviewDTO(r store.ReviewRow) forgejo.Review {
	return forgejo.Review{
		ID:          r.ID,
		User:        &forgejo.User{Login: r.ReviewerName, FullName: r.ReviewerName, Type: "User"},
		State:       r.Event,
		Body:        r.Body,
		SubmittedAt: r.CreatedAt,
		Official:    r.Event != forgejo.ReviewComment,
	}
}

// ---- engine implementations ------------------------------------------------

// engineListPulls lists a repo's engine pulls.
func (s *Service) engineListPulls(ctx context.Context, repo db.Repository, be gitBackend, state string) ([]forgejo.PullRequest, error) {
	rows, err := s.store.ListPullsByRepo(ctx, be.repoID, state)
	if err != nil {
		return nil, err
	}
	dir, err := s.engineDir(ctx, be)
	if err != nil {
		return nil, err
	}
	out := make([]forgejo.PullRequest, 0, len(rows))
	for _, pr := range rows {
		dto, err := s.enginePullDTO(ctx, repo, be, dir, pr, true)
		if err != nil {
			return nil, err
		}
		out = append(out, dto)
	}
	return out, nil
}

// engineGetPull loads one pull with live git facts.
func (s *Service) engineGetPull(ctx context.Context, repo db.Repository, be gitBackend, number int) (store.PullRequest, forgejo.PullRequest, error) {
	dir, err := s.engineDir(ctx, be)
	if err != nil {
		return store.PullRequest{}, forgejo.PullRequest{}, err
	}
	pr, err := s.store.GetPullByNumber(ctx, be.repoID, number)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return store.PullRequest{}, forgejo.PullRequest{}, ErrNotFound
		}
		return store.PullRequest{}, forgejo.PullRequest{}, err
	}
	dto, err := s.enginePullDTO(ctx, repo, be, dir, pr, true)
	return pr, dto, nil
}

// engineCreatePull opens a PR after validating the branches exist in the repo.
func (s *Service) engineCreatePull(ctx context.Context, repo db.Repository, be gitBackend, actor Actor, in CreatePullInput) (forgejo.PullRequest, error) {
	login, _, err := s.actorProfile(ctx, actor)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	dir, err := s.engineDir(ctx, be)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	headSHA, ok, err := revParseRef(dir, in.Head)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	if !ok {
		return forgejo.PullRequest{}, fmt.Errorf("%w: source branch %q not found", ErrInvalidInput, in.Head)
	}
	baseSHA, ok, err := revParseRef(dir, in.Base)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	if !ok {
		return forgejo.PullRequest{}, fmt.Errorf("%w: target branch %q not found", ErrInvalidInput, in.Base)
	}
	// Like GitHub: branches with entirely different histories have nothing
	// to compare (no merge base) — a 4xx, not a broken PR row.
	if hasBase, err := haveMergeBase(dir, baseSHA, headSHA); err != nil {
		return forgejo.PullRequest{}, err
	} else if !hasBase {
		return forgejo.PullRequest{}, fmt.Errorf("%w: %s and %s have entirely different commit histories", ErrInvalidInput, in.Base, in.Head)
	}
	// Number allocation races only on concurrent creation in one repo:
	// retry the insert once, then surface a conflict.
	pr, err := s.store.CreatePull(ctx, be.repoID, be.tenantID, actor.UserID, login,
		in.Title, in.Body, in.Head, headSHA, in.Base, baseSHA)
	if errors.Is(err, store.ErrPullNumberTaken) {
		pr, err = s.store.CreatePull(ctx, be.repoID, be.tenantID, actor.UserID, login,
			in.Title, in.Body, in.Head, headSHA, in.Base, baseSHA)
	}
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	dto, err := s.enginePullDTO(ctx, repo, be, dir, pr, true)
	return dto, err
}

// enginePullDiff returns the PR's parsed diff — merge-base...head, exactly
// the three-dot semantics forgejo serves.
func (s *Service) enginePullDiff(ctx context.Context, be gitBackend, pr store.PullRequest) ([]forgejo.DiffFile, error) {
	dir, err := s.engineDir(ctx, be)
	if err != nil {
		return nil, err
	}
	mb, err := gitBrowse(dir, "merge-base", pr.BaseSHA, pr.HeadSHA)
	if err != nil {
		return nil, err
	}
	cmd := exec.Command("git", "diff-tree", "-p", "--no-commit-id", strings.TrimSpace(mb), pr.HeadSHA)
	cmd.Dir = dir
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git diff-tree: %w: %s", err, errOut.String())
	}
	b := out.Bytes()
	if int64(len(b)) > browseDiffBytes {
		b = b[:browseDiffBytes]
	}
	return forgejo.ParseUnifiedDiff(string(b)), nil
}

// enginePullCommits lists the commits in the PR (head, minus base).
func (s *Service) enginePullCommits(ctx context.Context, be gitBackend, pr store.PullRequest) ([]forgejo.Commit, error) {
	dir, err := s.engineDir(ctx, be)
	if err != nil {
		return nil, err
	}
	out, err := gitBrowse(dir, "log", "--format="+logFormat, "^"+pr.BaseSHA, pr.HeadSHA)
	if err != nil {
		return nil, err
	}
	var commits []forgejo.Commit
	for _, rec := range parseLogRecords(out) {
		c, err := commitFromRecord(rec)
		if err != nil {
			return nil, err
		}
		commits = append(commits, c)
	}
	return commits, nil
}

// engineMergePull performs the merge through the B3 primitives.
func (s *Service) engineMergePull(ctx context.Context, repo db.Repository, be gitBackend, actor Actor, number int, method string) (forgejo.PullRequest, error) {
	login, email, err := s.actorProfile(ctx, actor)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	pr, dto, err := s.engineGetPull(ctx, repo, be, number)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	if pr.State != "open" {
		return forgejo.PullRequest{}, fmt.Errorf("%w: pull request is not open", ErrInvalidInput)
	}

	// Serialize with pushes and other merges on this repo.
	release, err := s.GitPushLeases().Acquire(ctx, be.repoID.String(), gitengine.PushLeaseTTL)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	defer func() { _ = release() }()

	rcx := gitengine.ReceiveContext{
		ID: be.repoID.String(), TenantID: be.tenantID.String(),
		ProjectSlug: be.projectSlug, RepoSlug: be.repoSlug,
	}
	dir, err := s.engineDir(ctx, be)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	rcx.Dir = dir

	// Truth freshness: merge against what the referee actually holds.
	m, etag, err := s.gitEngine.LoadManifest(ctx, be.tenantID.String(), be.projectSlug, be.repoSlug)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	st, err := s.GitReferee().Load(ctx, rcx.ID)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	if !pullRefsEqual(st.Refs, m) {
		return forgejo.PullRequest{}, fmt.Errorf("%w: repository state is ahead; retry", ErrConflict)
	}

	// Refresh branch tips; missing branches abort cleanly.
	headSHA, headOK, err := revParseRef(dir, pr.HeadRef)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	baseSHA, baseOK, err := revParseRef(dir, pr.BaseRef)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	if !headOK || !baseOK {
		return forgejo.PullRequest{}, fmt.Errorf("%w: head or base branch is gone", ErrInvalidInput)
	}

	// Authoritative merge gate (branch policies + reviews + status checks).
	reviews, err := s.store.ListPullReviews(ctx, pr.ID)
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	dto.Head.SHA, dto.Base.SHA = headSHA, baseSHA
	state, err := s.branchGate(ctx, repo, dto, reviewsToDTO(reviews))
	if err != nil {
		return forgejo.PullRequest{}, err
	}
	if state.Blocked {
		return forgejo.PullRequest{}, fmt.Errorf("%w: %s", ErrPolicyViolation, joinDenials(state.Denials))
	}

	// Compute the merge with git itself.
	mergedSHA, err := computeMerge(dir, method, pr, headSHA, baseSHA, login, email)
	if err != nil {
		return forgejo.PullRequest{}, err
	}

	// Fast-forward base to the merge result, then commit truth exactly like a
	// push does: referee CAS first, manifest second.
	if out, err := gitBrowse(dir, "update-ref", "refs/heads/"+pr.BaseRef, mergedSHA); err != nil {
		_ = out
		return forgejo.PullRequest{}, err
	}
	if err := gitengine.SyncRepoToBucket(ctx, s.gitEngine, s.GitReferee(), rcx, m, etag, st, mergedSHA, login); err != nil {
		return forgejo.PullRequest{}, err
	}
	if err := s.store.MarkPullMerged(ctx, be.repoID, number, actor.UserID, login, method, mergedSHA); err != nil {
		return forgejo.PullRequest{}, err
	}
	_, merged, err := s.engineGetPull(ctx, repo, be, number)
	return merged, err
}

// computeMerge produces the commit sha method requires, leaving refs untouched.
func computeMerge(dir, method string, pr store.PullRequest, headSHA, baseSHA, name, email string) (string, error) {
	env := gitCmdEnv(name, email)
	run := func(args ...string) (string, error) {
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = env
		var out, errOut bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errOut
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("git %v: %w: %s", args, err, errOut.String())
		}
		return strings.TrimSpace(out.String()), nil
	}
	switch method {
	case "merge":
		tree, clean, err := mergeTree(dir, baseSHA, headSHA)
		if err != nil {
			return "", err
		}
		if !clean {
			return "", fmt.Errorf("%w: conflicts with the base branch", ErrConflict)
		}
		msg := fmt.Sprintf("Merge pull request #%d: %s\n", pr.Number, pr.Title)
		if pr.Body != "" {
			msg += "\n" + pr.Body + "\n"
		}
		return run("commit-tree", tree, "-p", baseSHA, "-p", headSHA, "-m", msg)
	case "squash":
		tree, clean, err := mergeTree(dir, baseSHA, headSHA)
		if err != nil {
			return "", err
		}
		if !clean {
			return "", fmt.Errorf("%w: conflicts with the base branch", ErrConflict)
		}
		msg := fmt.Sprintf("%s (#%d)\n", pr.Title, pr.Number)
		if pr.Body != "" {
			msg += "\n" + pr.Body + "\n"
		}
		return run("commit-tree", tree, "-p", baseSHA, "-m", msg)
	case "rebase":
		// Replay the PR commits onto base in a throwaway worktree. A
		// cherry-pick conflict rebase exits non-zero: surface a conflict.
		tmp, err := os.MkdirTemp("", "quill-rebase")
		if err != nil {
			return "", err
		}
		defer os.RemoveAll(tmp)
		wt := filepath.Join(tmp, "w")
		if _, err := run("worktree", "add", "--detach", wt, headSHA); err != nil {
			return "", err
		}
		defer func() { _ = exec.Command("git", "worktree", "remove", "--force", wt).Run() }()
		cmd := exec.Command("git", "rebase", "--onto", baseSHA, baseSHA+"^", "HEAD")
		cmd.Dir = wt
		cmd.Env = env
		var out, errOut bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &errOut
		if err := cmd.Run(); err != nil {
			return "", fmt.Errorf("%w: rebase conflicts\n%s", ErrConflict, errOut.String())
		}
		cmd2 := exec.Command("git", "rev-parse", "HEAD")
		cmd2.Dir = wt
		cmd2.Env = env
		var out2 bytes.Buffer
		cmd2.Stdout = &out2
		if err := cmd2.Run(); err != nil {
			return "", fmt.Errorf("rebase read head: %w", err)
		}
		return strings.TrimSpace(out2.String()), nil
	default:
		return "", fmt.Errorf("%w: merge method must be merge, squash or rebase", ErrInvalidInput)
	}
}

// reviewDTOs maps stored review rows to the DTOs the gate consumes.
func reviewsToDTO(rows []store.ReviewRow) []forgejo.Review {
	out := make([]forgejo.Review, 0, len(rows))
	for _, r := range rows {
		out = append(out, reviewDTO(r))
	}
	return out
}

// pullRefsEqual checks the referee's refs against the manifest's view.
func pullRefsEqual(refs map[string]string, m *gitengine.Manifest) bool {
	if len(refs) != len(m.Refs) {
		return false
	}
	for _, r := range m.Refs {
		if refs[r.Name] != r.Target {
			return false
		}
	}
	return true
}

// haveMergeBase reports whether two commits share history.
func haveMergeBase(dir, a, b string) (bool, error) {
	cmd := exec.Command("git", "merge-base", a, b)
	cmd.Dir = dir
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git merge-base: %w", err)
}
