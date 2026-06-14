package platform

import (
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/store/db"
)

// These tests cover the pure decision functions behind branch policies: pattern
// matching and the review tally / merge gate. They run in CI with no external
// services.

func policy(pattern string, approvals int32, dismissStale bool) db.BranchPolicy {
	return db.BranchPolicy{
		Pattern:               pattern,
		RequiredApprovals:     approvals,
		DismissStaleApprovals: dismissStale,
	}
}

func review(login, state string, stale, dismissed bool) forgejo.Review {
	return forgejo.Review{
		User:      &forgejo.User{Login: login},
		State:     state,
		Stale:     stale,
		Dismissed: dismissed,
	}
}

func TestMatchBranchPolicy(t *testing.T) {
	policies := []db.BranchPolicy{
		policy("release/*", 2, false),
		policy("main", 1, false),
	}

	t.Run("exact match wins over glob", func(t *testing.T) {
		got := matchBranchPolicy(policies, "main")
		if got == nil || got.Pattern != "main" {
			t.Fatalf("expected exact main policy, got %+v", got)
		}
	})

	t.Run("glob matches", func(t *testing.T) {
		got := matchBranchPolicy(policies, "release/1.2")
		if got == nil || got.Pattern != "release/*" {
			t.Fatalf("expected release/* policy, got %+v", got)
		}
	})

	t.Run("no match returns nil", func(t *testing.T) {
		if got := matchBranchPolicy(policies, "feature/x"); got != nil {
			t.Fatalf("expected nil, got %+v", got)
		}
	})

	t.Run("exact beats glob regardless of order", func(t *testing.T) {
		// "develop" matches the glob "*", but an exact "develop" must win.
		ps := []db.BranchPolicy{policy("*", 3, false), policy("develop", 1, false)}
		got := matchBranchPolicy(ps, "develop")
		if got == nil || got.Pattern != "develop" {
			t.Fatalf("expected exact develop policy, got %+v", got)
		}
	})
}

func TestSummarizeReviews(t *testing.T) {
	author := &forgejo.User{Login: "author"}

	t.Run("latest review per user wins", func(t *testing.T) {
		reviews := []forgejo.Review{
			review("amir", forgejo.ReviewRequestChanges, false, false),
			review("amir", forgejo.ReviewApproved, false, false), // supersedes
		}
		approvals, changes := summarizeReviews(reviews, author, false)
		if approvals != 1 || changes != 0 {
			t.Fatalf("approvals=%d changes=%d, want 1/0", approvals, changes)
		}
	})

	t.Run("author self-review excluded", func(t *testing.T) {
		reviews := []forgejo.Review{review("author", forgejo.ReviewApproved, false, false)}
		approvals, changes := summarizeReviews(reviews, author, false)
		if approvals != 0 || changes != 0 {
			t.Fatalf("approvals=%d changes=%d, want 0/0", approvals, changes)
		}
	})

	t.Run("dismissed reviews ignored", func(t *testing.T) {
		reviews := []forgejo.Review{review("amir", forgejo.ReviewApproved, false, true)}
		approvals, _ := summarizeReviews(reviews, author, false)
		if approvals != 0 {
			t.Fatalf("approvals=%d, want 0", approvals)
		}
	})

	t.Run("comment-only reviews do not count", func(t *testing.T) {
		reviews := []forgejo.Review{review("amir", forgejo.ReviewComment, false, false)}
		approvals, changes := summarizeReviews(reviews, author, false)
		if approvals != 0 || changes != 0 {
			t.Fatalf("approvals=%d changes=%d, want 0/0", approvals, changes)
		}
	})

	t.Run("stale approval ignored only when policy dismisses stale", func(t *testing.T) {
		reviews := []forgejo.Review{review("amir", forgejo.ReviewApproved, true, false)}
		if approvals, _ := summarizeReviews(reviews, author, true); approvals != 0 {
			t.Fatalf("with dismissStale: approvals=%d, want 0", approvals)
		}
		if approvals, _ := summarizeReviews(reviews, author, false); approvals != 1 {
			t.Fatalf("without dismissStale: approvals=%d, want 1", approvals)
		}
	})

	t.Run("distinct reviewers tally independently", func(t *testing.T) {
		reviews := []forgejo.Review{
			review("amir", forgejo.ReviewApproved, false, false),
			review("bea", forgejo.ReviewApproved, false, false),
			review("cy", forgejo.ReviewRequestChanges, false, false),
		}
		approvals, changes := summarizeReviews(reviews, author, false)
		if approvals != 2 || changes != 1 {
			t.Fatalf("approvals=%d changes=%d, want 2/1", approvals, changes)
		}
	})
}

func TestGateFromReviews(t *testing.T) {
	author := &forgejo.User{Login: "author"}

	t.Run("nil policy never blocks", func(t *testing.T) {
		st := gateFromReviews(nil, author, nil)
		if st.Blocked || st.Policy != nil {
			t.Fatalf("expected open gate, got %+v", st)
		}
	})

	t.Run("insufficient approvals blocks with count", func(t *testing.T) {
		p := policy("main", 1, false)
		st := gateFromReviews(&p, author, nil)
		if !st.Blocked {
			t.Fatalf("expected blocked")
		}
		if st.Reason != "0 of 1 required approvals" {
			t.Fatalf("reason=%q", st.Reason)
		}
	})

	t.Run("requested changes block even with enough approvals", func(t *testing.T) {
		p := policy("main", 1, false)
		reviews := []forgejo.Review{
			review("amir", forgejo.ReviewApproved, false, false),
			review("bea", forgejo.ReviewRequestChanges, false, false),
		}
		st := gateFromReviews(&p, author, reviews)
		if !st.Blocked || st.Reason != "changes have been requested and must be resolved" {
			t.Fatalf("expected change-request block, got %+v", st)
		}
	})

	t.Run("satisfied policy is not blocked", func(t *testing.T) {
		p := policy("main", 1, false)
		reviews := []forgejo.Review{review("amir", forgejo.ReviewApproved, false, false)}
		st := gateFromReviews(&p, author, reviews)
		if st.Blocked {
			t.Fatalf("expected open gate, got %+v", st)
		}
		if st.Approvals != 1 {
			t.Fatalf("approvals=%d, want 1", st.Approvals)
		}
	})
}
