package store_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/nielsuitterdijk22/quill/internal/store"
)

// D1 import-surface tests: history rows are inserted with EXPLICIT numbers,
// authors and timestamps (Forgejo preserves them), re-running the same import
// is a no-op that returns the stored row, and unknown author logins resolve
// to nil without failing the import.
func TestImportPullHistory(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()
	repoID, tenantID := newRepoForGitState(t, st)

	// A forgejo link row: the enumeration only walks linked repos.
	linkRepo := func(owner, name string) store.ForgejoRepoLink {
		t.Helper()
		_, err := st.Pool().Exec(ctx,
			`UPDATE repositories SET forgejo_repo_id = $2, forgejo_owner = $3, forgejo_name = $4 WHERE id = $1`,
			repoID, int64(42), owner, name)
		if err != nil {
			t.Fatalf("link repo: %v", err)
		}
		links, err := st.ListForgejoRepoLinks(ctx)
		if err != nil {
			t.Fatalf("list links: %v", err)
		}
		for _, l := range links {
			if l.RepoID == repoID {
				if l.ForgejoOwner != owner || l.ForgejoName != name {
					t.Fatalf("link mismatch: %+v", l)
				}
				if l.HasEngineState {
					t.Fatalf("fresh repo must not report engine state")
				}
				return l
			}
		}
		t.Fatalf("repo not enumerated")
		return store.ForgejoRepoLink{}
	}
	linkRepo("mirror-org", "mirror-repo")

	// Engine state flips the flag (a migrated repo re-runs are detectable).
	if _, err := st.EnsureGitRepoState(ctx, repoID, tenantID); err != nil {
		t.Fatalf("ensure state: %v", err)
	}
	links, err := st.ListForgejoRepoLinks(ctx)
	if err != nil {
		t.Fatalf("list links: %v", err)
	}
	for _, l := range links {
		if l.RepoID == repoID && !l.HasEngineState {
			t.Fatalf("HasEngineState = false after EnsureGitRepoState")
		}
	}

	// Import PR #7 (merged) with its original timestamps.
	created := time.Date(2025, 3, 1, 10, 0, 0, 0, time.UTC)
	mergedAt := time.Date(2025, 3, 2, 12, 0, 0, 0, time.UTC)
	merger := uuid.New()
	pr, err := st.ImportPull(ctx, repoID, tenantID, 7,
		"imported PR", "history body", "feature", "abc123", "main", "def456", "closed",
		true, &merger, "alice", &mergedAt, "mergecommitsha",
		nil, "alice", created)
	if err != nil {
		t.Fatalf("import pull: %v", err)
	}
	if pr.Number != 7 || pr.State != "closed" || !pr.Merged || pr.MergeCommit != "mergecommitsha" {
		t.Fatalf("imported PR fields wrong: %+v", pr)
	}
	if !pr.MergedAt.Equal(mergedAt) || !pr.CreatedAt.Equal(created) {
		t.Fatalf("imported PR timestamps not preserved: %+v", pr)
	}
	if pr.MergedBy == nil || *pr.MergedBy != merger {
		t.Fatalf("merged_by not preserved: %+v", pr.MergedBy)
	}
	if pr.CreatedBy != nil {
		t.Fatalf("unknown author must stay nil, got %+v", pr.CreatedBy)
	}
	if pr.CreatedByName != "alice" {
		t.Fatalf("author snapshot lost: %q", pr.CreatedByName)
	}

	// Re-importing the same number is a no-op returning the stored row.
	title2 := "imported PR (re-run)"
	pr2, err := st.ImportPull(ctx, repoID, tenantID, 7,
		title2, "changed", "feature", "abc123", "main", "def456", "closed",
		true, &merger, "alice", &mergedAt, "mergecommitsha", nil, "alice", created)
	if err != nil {
		t.Fatalf("re-import: %v", err)
	}
	if pr2.ID != pr.ID || pr2.Title != "imported PR" {
		t.Fatalf("re-import mutated stored history: %+v", pr2)
	}

	// The next CreatePull continues the number sequence past imported rows.
	next, err := st.CreatePull(ctx, repoID, tenantID, uuid.New(), "elena", "new PR", "", "feat2", "bbb", "main", "ccc")
	if err != nil {
		t.Fatalf("create pull after import: %v", err)
	}
	if next.Number != 8 {
		t.Fatalf("number continuity broken: got %d want 8", next.Number)
	}

	// Comments and reviews keep their original timestamps.
	if err := st.ImportPullComment(ctx, pr.ID, tenantID, nil, "bob", "lgtm", created); err != nil {
		t.Fatalf("import comment: %v", err)
	}
	if err := st.ImportPullReview(ctx, pr.ID, tenantID, nil, "carol", "APPROVED", "", created); err != nil {
		t.Fatalf("import review: %v", err)
	}
	if err := st.ImportPullLineComment(ctx, pr.ID, tenantID, nil, "dave", "main.go", 12, "hmm", created); err != nil {
		t.Fatalf("import line comment: %v", err)
	}
	comments, err := st.ListPullComments(ctx, pr.ID)
	if err != nil || len(comments) != 1 {
		t.Fatalf("comments: %v %+v", err, comments)
	}
	reviews, err := st.ListPullReviews(ctx, pr.ID)
	if err != nil || len(reviews) != 1 || reviews[0].Event != "APPROVED" {
		t.Fatalf("reviews: %v %+v", err, reviews)
	}
	lines, err := st.ListPullLineComments(ctx, pr.ID)
	if err != nil || len(lines) != 1 || lines[0].Line != 12 || lines[0].Path != "main.go" {
		t.Fatalf("line comments: %v %+v", err, lines)
	}
}

func TestFindUserForForgejoLogin(t *testing.T) {
	st := testStore(t)
	ctx := context.Background()

	login := "import-usr-" + uuid.NewString()[:8]
	_, err := st.Pool().Exec(ctx,
		`INSERT INTO users (id, username, email, forgejo_username)
		 VALUES ($1, $2, $3, $2)`,
		uuid.New(), login, login+"@example.com")
	if err != nil {
		t.Fatalf("seed user: %v", err)
	}
	t.Cleanup(func() {
		_, _ = st.Pool().Exec(ctx, `DELETE FROM users WHERE username = $1`, login)
	})

	if _, ok, err := st.FindUserForForgejoLogin(ctx, login); err != nil || !ok {
		t.Fatalf("resolve by forgejo_username: %v ok=%v", err, ok)
	}
	// Case-insensitive.
	if _, ok, _ := st.FindUserForForgejoLogin(ctx, login[:3]+login[3:]); !ok {
		t.Fatalf("case-insensitive resolve failed")
	}
	// Unknown login: no error, not found.
	if _, ok, err := st.FindUserForForgejoLogin(ctx, "no-such-login"); err != nil || ok {
		t.Fatalf("unknown login: err=%v ok=%v", err, ok)
	}
	if _, ok, err := st.FindUserForForgejoLogin(ctx, ""); err != nil || ok {
		t.Fatalf("empty login: err=%v ok=%v", err, ok)
	}
}
