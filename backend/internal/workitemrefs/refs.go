// Package workitemrefs is the outbound half of the Quill→Tempo work-item
// cross-linking (design doc PR 8.5). When a Forgejo push or pull_request webhook
// arrives, Quill scans the commit messages, PR title/body, and branch names for
// Tempo work-item keys ([A-Z][A-Z0-9]*-\d+) and pushes the matches to Tempo's
// work-item-refs endpoint, tagged with the Quill project id the repo belongs to
// so Tempo can resolve the keys within that project's org.
//
// The webhook must ack fast and must not depend on Tempo being up, so the push
// is not made inline. Instead — exactly like the project-mirror outbox in
// internal/projectsync — the match is written to a durable outbox row and a
// background dispatcher delivers it with retry + exponential backoff, surviving
// both Tempo downtime and a Quill restart mid-delivery.
package workitemrefs

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// keyPattern matches Tempo work-item keys. It mirrors the pattern pinned in the
// wire contract ([A-Z][A-Z0-9]*-\d+) but scans case-insensitively; matches are
// uppercased before sending. Word boundaries keep it from matching a key spliced
// into the middle of a larger identifier. Deliberately loose: it also matches
// false positives like UTF-8 or SHA-256 — dropping keys that resolve to no work
// item is Tempo's job per the contract, not Quill's.
var keyPattern = regexp.MustCompile(`(?i)\b[A-Z][A-Z0-9]*-\d+\b`)

// Ref is a single PR or commit cross-link, as sent in the wire contract's refs
// array. workItemKeys is always non-empty (refs with no keys are not built).
type Ref struct {
	RefType      string   `json:"refType"` // "pr" | "commit"
	RepoSlug     string   `json:"repoSlug"`
	ExternalRef  string   `json:"externalRef"` // PR number or commit sha
	URL          string   `json:"url"`
	Title        string   `json:"title"`
	State        string   `json:"state"`
	Author       string   `json:"author"`
	WorkItemKeys []string `json:"workItemKeys"`
}

// RefPush is the exact JSON body POSTed to Tempo's work-item-refs endpoint and
// the payload persisted in an outbox row.
type RefPush struct {
	QuillProjectID uuid.UUID `json:"quillProjectId"`
	Refs           []Ref     `json:"refs"`
}

// Commit is a webhook-shape-independent view of one pushed commit, mapped from
// the Forgejo push payload by the webhook handler.
type Commit struct {
	SHA     string
	Message string
	URL     string
	Author  string
}

// PullRequest is a webhook-shape-independent view of a pull_request event.
type PullRequest struct {
	Number int
	Title  string
	Body   string
	State  string
	URL    string
	Branch string
	Author string
}

// ExtractKeys scans the given sources for work-item keys, uppercases every
// match, and de-duplicates while preserving first-seen order. It returns nil
// when nothing matches so callers can cheaply skip empty refs.
func ExtractKeys(sources ...string) []string {
	var out []string
	seen := make(map[string]struct{})
	for _, src := range sources {
		for _, m := range keyPattern.FindAllString(src, -1) {
			key := strings.ToUpper(m)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, key)
		}
	}
	return out
}

// PushRefs builds one commit Ref per commit that mentions at least one work-item
// key. Each commit is scanned together with the branch name, so a branch named
// after a work item links every commit pushed on it. Commits with no keys are
// omitted entirely.
func PushRefs(repoSlug, branch string, commits []Commit) []Ref {
	var refs []Ref
	for _, c := range commits {
		keys := ExtractKeys(c.Message, branch)
		if len(keys) == 0 {
			continue
		}
		refs = append(refs, Ref{
			RefType:      "commit",
			RepoSlug:     repoSlug,
			ExternalRef:  c.SHA,
			URL:          c.URL,
			Title:        firstLine(c.Message),
			State:        "",
			Author:       c.Author,
			WorkItemKeys: keys,
		})
	}
	return refs
}

// PullRequestRef builds the Ref for a pull_request event, scanning the title,
// body, and source branch name. The second return is false when the PR mentions
// no work-item keys, in which case nothing should be pushed.
func PullRequestRef(repoSlug string, pr PullRequest) (Ref, bool) {
	keys := ExtractKeys(pr.Title, pr.Body, pr.Branch)
	if len(keys) == 0 {
		return Ref{}, false
	}
	return Ref{
		RefType:      "pr",
		RepoSlug:     repoSlug,
		ExternalRef:  strconv.Itoa(pr.Number),
		URL:          pr.URL,
		Title:        pr.Title,
		State:        pr.State,
		Author:       pr.Author,
		WorkItemKeys: keys,
	}, true
}

// firstLine returns the first line of a commit message, trimmed, used as the
// ref title.
func firstLine(msg string) string {
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = msg[:i]
	}
	return strings.TrimSpace(msg)
}

// EventWriter is the subset of the store needed to enqueue an outbox row.
// *store.Store satisfies it via its embedded *db.Queries.
type EventWriter interface {
	InsertWorkItemRefEvent(ctx context.Context, arg db.InsertWorkItemRefEventParams) (db.WorkItemRefOutbox, error)
}

// Enqueue persists a RefPush as an outbox row for the dispatcher to deliver. It
// is a no-op returning nil when the push carries no refs, so callers need not
// special-case the common "no keys mentioned" webhook.
func Enqueue(ctx context.Context, w EventWriter, push RefPush) error {
	if len(push.Refs) == 0 {
		return nil
	}
	payload, err := json.Marshal(push)
	if err != nil {
		return fmt.Errorf("marshal work-item ref push: %w", err)
	}
	_, err = w.InsertWorkItemRefEvent(ctx, db.InsertWorkItemRefEventParams{
		ID:         uuid.New(),
		ProjectID:  push.QuillProjectID,
		Payload:    payload,
		OccurredAt: time.Now().UTC(),
	})
	if err != nil {
		return fmt.Errorf("enqueue work-item ref push: %w", err)
	}
	return nil
}
