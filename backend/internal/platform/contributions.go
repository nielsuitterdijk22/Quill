package platform

import (
	"context"
	"fmt"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store"
)

// Contribution graph (2026-11-14). The profile calendar used to be Forgejo's
// user heatmap; with Forgejo off it came back empty. The engine now indexes
// default-branch commits (author email + date) into commit_activity — the
// last year once per repo, then each push's new range — and the graph counts
// the viewer's commits (account email + their extra commit emails) plus their
// pull requests, reviews and issues, over repositories they can read.

const (
	// contributionWindow covers the calendar's 53 weeks plus slack.
	contributionWindow = 372 * 24 * time.Hour
	// activityMaxCommits bounds one indexing walk (a huge import still
	// indexes its newest year of history).
	activityMaxCommits = 50000
	// maxCommitEmails bounds a user's extra commit emails.
	maxCommitEmails = 10
)

// ContributionDay is one UTC day of the calendar in the shape the frontend
// (and formerly Forgejo's heatmap) uses: unix seconds + count.
type ContributionDay struct {
	Timestamp     int64 `json:"timestamp"`
	Contributions int   `json:"contributions"`
}

// CommitEmails is a user's commit-attribution setting.
type CommitEmails struct {
	// Primary is the account email; it always counts.
	Primary string `json:"primary"`
	// Emails are the extra addresses.
	Emails []string `json:"emails"`
}

// indexCommitActivity records the default-branch commits a push brought in.
// A repo whose history isn't indexed yet gets its whole last year (and is
// marked); afterwards only old..new is walked. A force-push whose old head
// is gone falls back to the full year (inserts skip known shas).
func (s *Service) indexCommitActivity(ctx context.Context, be gitBackend, newSHA, oldSHA string) (int, error) {
	if !be.engine || newSHA == "" || isZeroSHA(newSHA) {
		return 0, nil
	}
	backfilled, err := s.store.CommitActivityBackfilled(ctx, be.repoID)
	if err != nil {
		return 0, err
	}
	dir, err := s.engineDir(ctx, be)
	if err != nil {
		return 0, err
	}
	since := time.Now().Add(-contributionWindow)
	var commits []store.CommitActivity
	if backfilled && oldSHA != "" && !isZeroSHA(oldSHA) {
		commits, err = commitActivityLog(dir, since, newSHA, "^"+oldSHA)
	}
	if !backfilled || oldSHA == "" || isZeroSHA(oldSHA) || err != nil {
		commits, err = commitActivityLog(dir, since, newSHA)
	}
	if err != nil {
		return 0, err
	}
	if err := s.store.InsertCommitActivity(ctx, be.repoID, commits); err != nil {
		return 0, fmt.Errorf("insert commit activity: %w", err)
	}
	if !backfilled {
		if err := s.store.MarkCommitActivityBackfilled(ctx, be.repoID); err != nil {
			return len(commits), err
		}
	}
	return len(commits), nil
}

// commitActivityLog lists sha, author email and author date of the commits
// in revs (git rev-list syntax), authored since `since`.
func commitActivityLog(dir string, since time.Time, revs ...string) ([]store.CommitActivity, error) {
	args := append([]string{
		"log", "--format=%H%x00%ae%x00%at",
		"--since=" + strconv.FormatInt(since.Unix(), 10),
		"-n", strconv.Itoa(activityMaxCommits),
	}, revs...)
	out, err := gitBrowse(dir, args...)
	if err != nil {
		return nil, err
	}
	return parseCommitActivity(out, since), nil
}

func parseCommitActivity(out string, since time.Time) []store.CommitActivity {
	var commits []store.CommitActivity
	for _, line := range strings.Split(out, "\n") {
		f := strings.Split(line, "\x00")
		if len(f) != 3 || f[0] == "" || f[1] == "" {
			continue
		}
		ts, err := strconv.ParseInt(f[2], 10, 64)
		if err != nil {
			continue
		}
		at := time.Unix(ts, 0).UTC()
		// --since filters on committer date; a rebased old commit keeps its
		// author date, which is what the calendar shows.
		if at.Before(since) {
			continue
		}
		commits = append(commits, store.CommitActivity{SHA: f[0], AuthorEmail: strings.ToLower(f[1]), AuthoredAt: at})
	}
	return commits
}

// BackfillCommitActivity indexes the last year of default-branch history of
// every engine repo not indexed yet (git-maintenance runs it nightly; repos
// already indexed are skipped, push indexing keeps them current).
func (s *Service) BackfillCommitActivity(ctx context.Context) (indexed, failed int, err error) {
	repos, err := s.store.ListEngineRepos(ctx)
	if err != nil {
		return 0, 0, err
	}
	for _, r := range repos {
		done, err := s.store.CommitActivityBackfilled(ctx, r.RepoID)
		if err != nil {
			failed++
			continue
		}
		if done {
			continue
		}
		repo, err := s.store.GetRepositoryByID(ctx, r.RepoID)
		if err != nil {
			failed++
			continue
		}
		head, _, err := s.defaultHead(ctx, r.RepoID, repo.DefaultBranch)
		if err != nil {
			failed++
			continue
		}
		if head == "" {
			continue // empty repo: indexed by its first push
		}
		be := gitBackend{engine: true, repoID: r.RepoID, tenantID: r.TenantID, projectSlug: r.ProjectSlug, repoSlug: r.RepoSlug}
		if _, err := s.indexCommitActivity(ctx, be, head, ""); err != nil {
			s.logger.Warn("commit activity backfill failed", "repo", r.ProjectSlug+"/"+r.RepoSlug, "error", err)
			failed++
			continue
		}
		indexed++
	}
	return indexed, failed, nil
}

// MyContributions returns the actor's contribution calendar (oldest first).
func (s *Service) MyContributions(ctx context.Context, actor Actor) ([]ContributionDay, error) {
	user, err := s.store.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("load user: %w", err)
	}
	extra, err := s.store.UserCommitEmails(ctx, actor.UserID)
	if err != nil {
		return nil, fmt.Errorf("load commit emails: %w", err)
	}
	emails := append([]string{strings.ToLower(user.Email)}, extra...)
	projects, err := s.visibleProjects(ctx, actor, "")
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(projects))
	for _, p := range projects {
		ids = append(ids, p.ID)
	}
	days, err := s.store.UserContributions(ctx, actor.UserID, emails, ids, time.Now().Add(-contributionWindow))
	if err != nil {
		return nil, fmt.Errorf("count contributions: %w", err)
	}
	out := make([]ContributionDay, 0, len(days))
	for _, d := range days {
		out = append(out, ContributionDay{Timestamp: d.Day.Unix(), Contributions: d.Count})
	}
	return out, nil
}

// MyCommitEmails returns the actor's commit-attribution emails.
func (s *Service) MyCommitEmails(ctx context.Context, actor Actor) (CommitEmails, error) {
	user, err := s.store.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return CommitEmails{}, fmt.Errorf("load user: %w", err)
	}
	extra, err := s.store.UserCommitEmails(ctx, actor.UserID)
	if err != nil {
		return CommitEmails{}, err
	}
	return CommitEmails{Primary: user.Email, Emails: extra}, nil
}

// SetMyCommitEmails replaces the actor's extra commit emails: trimmed,
// lowercased, deduplicated, the account email dropped (it always counts).
func (s *Service) SetMyCommitEmails(ctx context.Context, actor Actor, emails []string) (CommitEmails, error) {
	user, err := s.store.GetUserByID(ctx, actor.UserID)
	if err != nil {
		return CommitEmails{}, fmt.Errorf("load user: %w", err)
	}
	primary := strings.ToLower(user.Email)
	seen := map[string]bool{primary: true}
	clean := []string{}
	for _, e := range emails {
		e = strings.ToLower(strings.TrimSpace(e))
		if e == "" || seen[e] {
			continue
		}
		if len(e) > 254 {
			return CommitEmails{}, fmt.Errorf("%w: email too long", ErrInvalidInput)
		}
		if a, err := mail.ParseAddress(e); err != nil || a.Address != e {
			return CommitEmails{}, fmt.Errorf("%w: %q is not an email address", ErrInvalidInput, e)
		}
		seen[e] = true
		clean = append(clean, e)
	}
	if len(clean) > maxCommitEmails {
		return CommitEmails{}, fmt.Errorf("%w: at most %d commit emails", ErrInvalidInput, maxCommitEmails)
	}
	if err := s.store.SetUserCommitEmails(ctx, actor.UserID, clean); err != nil {
		return CommitEmails{}, err
	}
	return CommitEmails{Primary: user.Email, Emails: clean}, nil
}
