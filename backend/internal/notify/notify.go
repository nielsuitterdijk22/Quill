// Package notify sends transactional email notifications for Quill events:
// PR reviews, PR comments, CI failures, and @-mentions.
package notify

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// Config holds the SMTP and application settings required for email delivery.
// All fields are optional; a zero Config produces a disabled Service.
type Config struct {
	SMTPHost     string
	SMTPPort     int
	SMTPUser     string
	SMTPPassword string
	FromAddress  string
	AppURL       string // e.g. "https://app.example.com" — used to build links in emails
}

// Enabled reports whether the config has enough settings to send email.
func (c Config) Enabled() bool {
	return c.SMTPHost != "" && c.FromAddress != ""
}

// UserLookup retrieves a user by ID, username, or linked Forgejo username.
type UserLookup interface {
	GetUserByID(ctx context.Context, id uuid.UUID) (db.User, error)
	GetUserByUsername(ctx context.Context, lower string) (db.User, error)
	GetUserByForgejoUsername(ctx context.Context, forgejoUsername string) (db.User, error)
}

// Mailer sends a single email message.
type Mailer interface {
	Send(to, subject, htmlBody string) error
}

// Service dispatches notification emails. All public methods are safe to call
// on a nil *Service (they become no-ops).
type Service struct {
	cfg    Config
	mailer Mailer
	store  UserLookup
	logger *slog.Logger
}

// New returns a ready Service. When cfg.Enabled() is false, all Send* calls
// are no-ops and no connection is attempted.
func New(cfg Config, store UserLookup, logger *slog.Logger) *Service {
	s := &Service{cfg: cfg, store: store, logger: logger}
	if cfg.Enabled() {
		s.mailer = newSMTPMailer(cfg)
	}
	return s
}

// PRReviewEvent carries the data needed to notify a PR author that their PR
// received a review.
type PRReviewEvent struct {
	ProjectSlug         string
	RepoSlug            string
	PRNumber            int
	PRTitle             string
	PRAuthorID          uuid.UUID  // set when the Quill user ID is known
	PRAuthorForgejoUser string     // set when only the Forgejo login is known
	ReviewerName        string     // display name or username of the reviewer
	ReviewState         string     // "APPROVED", "REQUEST_CHANGES", "COMMENT"
}

// PRCommentEvent carries the data needed to notify a PR author of a new comment.
type PRCommentEvent struct {
	ProjectSlug          string
	RepoSlug             string
	PRNumber             int
	PRTitle              string
	PRAuthorID           uuid.UUID
	PRAuthorForgejoUser  string
	CommenterName        string
}

// CIFailureEvent carries the data needed to notify a user that their pipeline run failed.
type CIFailureEvent struct {
	ProjectSlug  string
	RepoSlug     string
	RunNumber    int64
	WorkflowPath string
	Ref          string
	TriggeredBy  uuid.UUID
}

// MentionEvent carries the data needed to notify a @-mentioned user.
type MentionEvent struct {
	ProjectSlug  string
	RepoSlug     string
	Context      string // "pull request", "issue", "comment"
	ContextTitle string
	ContextURL   string // optional deep link
	MentionerName string
	BodyExcerpt  string
}

// mentionRe matches @username tokens in Markdown/plain text.
var mentionRe = regexp.MustCompile(`(?:^|[^a-zA-Z0-9_])@([a-zA-Z0-9_-]{1,40})`)

// ParseMentions returns unique lowercased usernames mentioned in text.
func ParseMentions(text string) []string {
	matches := mentionRe.FindAllStringSubmatch(text, -1)
	seen := make(map[string]struct{}, len(matches))
	var out []string
	for _, m := range matches {
		u := m[1]
		if _, dup := seen[u]; !dup {
			seen[u] = struct{}{}
			out = append(out, u)
		}
	}
	return out
}

// NotifyPRReview sends an email to the PR author when they receive a review.
// The send is fire-and-forget (goroutine); errors are logged.
func (s *Service) NotifyPRReview(ctx context.Context, ev PRReviewEvent) {
	if s == nil || s.mailer == nil {
		return
	}
	go s.sendPRReview(ctx, ev)
}

// NotifyPRComment sends an email to the PR author when a comment is posted.
func (s *Service) NotifyPRComment(ctx context.Context, ev PRCommentEvent) {
	if s == nil || s.mailer == nil {
		return
	}
	go s.sendPRComment(ctx, ev)
}

// NotifyCIFailure sends an email to the person who triggered a failed run.
func (s *Service) NotifyCIFailure(ctx context.Context, ev CIFailureEvent) {
	if s == nil || s.mailer == nil {
		return
	}
	go s.sendCIFailure(ctx, ev)
}

// NotifyMentions looks up each username, then sends mention notification emails.
func (s *Service) NotifyMentions(ctx context.Context, usernames []string, ev MentionEvent) {
	if s == nil || s.mailer == nil || len(usernames) == 0 {
		return
	}
	go s.sendMentions(ctx, usernames, ev)
}

func (s *Service) sendPRReview(ctx context.Context, ev PRReviewEvent) {
	author, err := s.lookupPRAuthor(ctx, ev.PRAuthorID, ev.PRAuthorForgejoUser)
	if err != nil {
		s.logger.Warn("notify: could not load PR author", "error", err)
		return
	}
	if author.Email == "" {
		return
	}

	var action string
	switch ev.ReviewState {
	case "APPROVED":
		action = "approved"
	case "REQUEST_CHANGES":
		action = "requested changes on"
	default:
		action = "commented on"
	}

	subject := fmt.Sprintf("[Quill] %s %s your pull request: %s", ev.ReviewerName, action, ev.PRTitle)
	link := s.prLink(ev.ProjectSlug, ev.RepoSlug, ev.PRNumber)
	body := renderPRReview(ev.ReviewerName, action, ev.PRTitle, ev.PRNumber, ev.RepoSlug, link)
	if err := s.mailer.Send(author.Email, subject, body); err != nil {
		s.logger.Warn("notify: failed to send PR review email", "to", author.Email, "error", err)
	}
}

func (s *Service) sendPRComment(ctx context.Context, ev PRCommentEvent) {
	author, err := s.lookupPRAuthor(ctx, ev.PRAuthorID, ev.PRAuthorForgejoUser)
	if err != nil {
		s.logger.Warn("notify: could not load PR author", "error", err)
		return
	}
	if author.Email == "" {
		return
	}

	subject := fmt.Sprintf("[Quill] %s commented on your pull request: %s", ev.CommenterName, ev.PRTitle)
	link := s.prLink(ev.ProjectSlug, ev.RepoSlug, ev.PRNumber)
	body := renderPRComment(ev.CommenterName, ev.PRTitle, ev.PRNumber, ev.RepoSlug, link)
	if err := s.mailer.Send(author.Email, subject, body); err != nil {
		s.logger.Warn("notify: failed to send PR comment email", "to", author.Email, "error", err)
	}
}

func (s *Service) sendCIFailure(ctx context.Context, ev CIFailureEvent) {
	user, err := s.store.GetUserByID(ctx, ev.TriggeredBy)
	if err != nil {
		s.logger.Warn("notify: could not load run triggerer", "error", err)
		return
	}
	if user.Email == "" {
		return
	}

	subject := fmt.Sprintf("[Quill] Pipeline failed: %s on %s", ev.WorkflowPath, ev.Ref)
	link := s.runLink(ev.ProjectSlug, ev.RepoSlug, ev.RunNumber)
	body := renderCIFailure(ev.WorkflowPath, ev.Ref, ev.RepoSlug, ev.RunNumber, link)
	if err := s.mailer.Send(user.Email, subject, body); err != nil {
		s.logger.Warn("notify: failed to send CI failure email", "to", user.Email, "error", err)
	}
}

func (s *Service) sendMentions(ctx context.Context, usernames []string, ev MentionEvent) {
	for _, username := range usernames {
		user, err := s.store.GetUserByUsername(ctx, username)
		if err != nil {
			continue // username doesn't exist in Quill — skip silently
		}
		if user.Email == "" {
			continue
		}
		subject := fmt.Sprintf("[Quill] %s mentioned you in a %s", ev.MentionerName, ev.Context)
		body := renderMention(ev.MentionerName, ev.Context, ev.ContextTitle, ev.BodyExcerpt, ev.ContextURL)
		if err := s.mailer.Send(user.Email, subject, body); err != nil {
			s.logger.Warn("notify: failed to send mention email", "to", user.Email, "error", err)
		}
	}
}

// lookupPRAuthor resolves the PR author by Quill user ID (preferred) or
// Forgejo username (fallback). Returns an error if neither is set or the user
// can't be found.
func (s *Service) lookupPRAuthor(ctx context.Context, id uuid.UUID, forgejoLogin string) (db.User, error) {
	if id != (uuid.UUID{}) {
		return s.store.GetUserByID(ctx, id)
	}
	if forgejoLogin != "" {
		return s.store.GetUserByForgejoUsername(ctx, forgejoLogin)
	}
	return db.User{}, fmt.Errorf("no author identifier provided")
}

func (s *Service) prLink(project, repo string, number int) string {
	if s.cfg.AppURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/projects/%s/repos/%s/pulls/%d", s.cfg.AppURL, project, repo, number)
}

func (s *Service) runLink(project, repo string, number int64) string {
	if s.cfg.AppURL == "" {
		return ""
	}
	return fmt.Sprintf("%s/projects/%s/repos/%s/pipelines/runs/%d", s.cfg.AppURL, project, repo, number)
}
