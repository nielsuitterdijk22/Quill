package platform

import (
	"context"
	"testing"

	"github.com/nielsuitterdijk22/quill/internal/forgejo"
	"github.com/nielsuitterdijk22/quill/internal/policy"
)

// These tests cover the pure decision functions behind branch policies that live
// in the platform layer: the review tally and the merge gate. Branch-pattern
// resolution and inheritance are tested in internal/policy. They run in CI with
// no external services.

func branchRule(approvals int, dismissStale bool) policy.BranchRule {
	return policy.BranchRule{RequiredApprovals: approvals, DismissStaleApprovals: dismissStale}
}

func review(login, state string, stale, dismissed bool) forgejo.Review {
	return forgejo.Review{
		User:      &forgejo.User{Login: login},
		State:     state,
		Stale:     stale,
		Dismissed: dismissed,
	}
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

func scopedBranch(scope policy.ScopeType, selector string, rule policy.BranchRule) policy.ScopedBranch {
	return policy.ScopedBranch{Scope: scope, Selector: selector, Rule: rule}
}

func scopedInput(t *testing.T, scope policy.ScopeType, selector string, rule policy.BranchRule) policy.ScopedPolicy {
	t.Helper()
	raw, err := rule.MarshalRules()
	if err != nil {
		t.Fatalf("marshal rule: %v", err)
	}
	return policy.ScopedPolicy{Scope: scope, Selector: selector, Rules: raw}
}

func pullOnto(base, head, author string) forgejo.PullRequest {
	return forgejo.PullRequest{
		Base: forgejo.PRRef{Ref: base},
		Head: forgejo.PRRef{Ref: head},
		User: &forgejo.User{Login: author},
	}
}

// TestBranchState exercises the evaluator-backed merge gate core: facts assembly,
// unanimous-allow composition across scopes, and the display summary. It uses the
// production typed evaluator, no DB or Forgejo.
func TestBranchState(t *testing.T) {
	svc := &Service{evaluator: policy.NewTypedEvaluator()}
	ctx := context.Background()
	pr := pullOnto("main", "feature", "author")

	t.Run("no applicable policy leaves the gate open", func(t *testing.T) {
		scoped := []policy.ScopedBranch{scopedBranch(policy.ScopeRepo, "release/*", branchRule(1, false))}
		inputs := []policy.ScopedPolicy{scopedInput(t, policy.ScopeRepo, "release/*", branchRule(1, false))}
		st, err := svc.branchState(ctx, scoped, inputs, pr, nil)
		if err != nil {
			t.Fatal(err)
		}
		if st.Applies || st.Blocked {
			t.Fatalf("expected open gate, got %+v", st)
		}
	})

	t.Run("insufficient approvals blocks with a scope-tagged denial", func(t *testing.T) {
		rule := branchRule(1, false)
		scoped := []policy.ScopedBranch{scopedBranch(policy.ScopeRepo, "main", rule)}
		inputs := []policy.ScopedPolicy{scopedInput(t, policy.ScopeRepo, "main", rule)}
		st, err := svc.branchState(ctx, scoped, inputs, pr, nil)
		if err != nil {
			t.Fatal(err)
		}
		if !st.Blocked || st.Reason != "0 of 1 required approvals" {
			t.Fatalf("expected approval block, got %+v", st)
		}
		if st.Pattern != "main" || st.RequiredApprovals != 1 {
			t.Fatalf("display summary wrong: %+v", st)
		}
		if len(st.Denials) != 1 || st.Denials[0].Scope != policy.ScopeRepo {
			t.Fatalf("denials=%+v", st.Denials)
		}
	})

	t.Run("requested changes block even with enough approvals", func(t *testing.T) {
		rule := branchRule(1, false)
		scoped := []policy.ScopedBranch{scopedBranch(policy.ScopeRepo, "main", rule)}
		inputs := []policy.ScopedPolicy{scopedInput(t, policy.ScopeRepo, "main", rule)}
		reviews := []forgejo.Review{
			review("amir", forgejo.ReviewApproved, false, false),
			review("bea", forgejo.ReviewRequestChanges, false, false),
		}
		st, err := svc.branchState(ctx, scoped, inputs, pr, reviews)
		if err != nil {
			t.Fatal(err)
		}
		if !st.Blocked || st.Reason != "changes have been requested and must be resolved" {
			t.Fatalf("expected change-request block, got %+v", st)
		}
	})

	t.Run("strictest scope wins under unanimous-allow", func(t *testing.T) {
		// Repo asks for 1 approval, tenant for 2. The composed gate requires 2 even
		// though the repo rule is the closest — a narrower scope cannot weaken.
		scoped := []policy.ScopedBranch{
			scopedBranch(policy.ScopeTenant, "main", branchRule(2, false)),
			scopedBranch(policy.ScopeRepo, "main", branchRule(1, false)),
		}
		inputs := []policy.ScopedPolicy{
			scopedInput(t, policy.ScopeTenant, "main", branchRule(2, false)),
			scopedInput(t, policy.ScopeRepo, "main", branchRule(1, false)),
		}
		reviews := []forgejo.Review{review("amir", forgejo.ReviewApproved, false, false)}
		st, err := svc.branchState(ctx, scoped, inputs, pr, reviews)
		if err != nil {
			t.Fatal(err)
		}
		if !st.Blocked {
			t.Fatalf("expected block from tenant rule, got %+v", st)
		}
		if st.RequiredApprovals != 2 {
			t.Fatalf("RequiredApprovals=%d, want 2 (max across scopes)", st.RequiredApprovals)
		}
		if st.Denials[0].Scope != policy.ScopeTenant {
			t.Fatalf("expected tenant denial, got %+v", st.Denials)
		}
	})

	t.Run("satisfied across all scopes opens the gate", func(t *testing.T) {
		scoped := []policy.ScopedBranch{
			scopedBranch(policy.ScopeTenant, "main", branchRule(2, false)),
			scopedBranch(policy.ScopeRepo, "main", branchRule(1, false)),
		}
		inputs := []policy.ScopedPolicy{
			scopedInput(t, policy.ScopeTenant, "main", branchRule(2, false)),
			scopedInput(t, policy.ScopeRepo, "main", branchRule(1, false)),
		}
		reviews := []forgejo.Review{
			review("amir", forgejo.ReviewApproved, false, false),
			review("bea", forgejo.ReviewApproved, false, false),
		}
		st, err := svc.branchState(ctx, scoped, inputs, pr, reviews)
		if err != nil {
			t.Fatal(err)
		}
		if st.Blocked {
			t.Fatalf("expected open gate, got %+v", st)
		}
		if st.Approvals != 2 {
			t.Fatalf("approvals=%d, want 2", st.Approvals)
		}
	})

	t.Run("allowedSources blocks a disallowed merge flow", func(t *testing.T) {
		rule := policy.BranchRule{AllowedSources: []string{"release/*"}}
		scoped := []policy.ScopedBranch{scopedBranch(policy.ScopeRepo, "main", rule)}
		inputs := []policy.ScopedPolicy{scopedInput(t, policy.ScopeRepo, "main", rule)}
		st, err := svc.branchState(ctx, scoped, inputs, pullOnto("main", "feature", "author"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if !st.Blocked {
			t.Fatalf("expected merge-flow block, got %+v", st)
		}
		// A permitted source merges freely.
		ok, err := svc.branchState(ctx, scoped, inputs, pullOnto("main", "release/1.0", "author"), nil)
		if err != nil {
			t.Fatal(err)
		}
		if ok.Blocked {
			t.Fatalf("expected open gate for allowed source, got %+v", ok)
		}
	})

	t.Run("OR'd dismissStale drops a stale approval", func(t *testing.T) {
		// Repo keeps stale approvals; tenant dismisses them. The OR means the stale
		// approval is dropped, so the single approval no longer counts.
		scoped := []policy.ScopedBranch{
			scopedBranch(policy.ScopeTenant, "main", branchRule(1, true)),
			scopedBranch(policy.ScopeRepo, "main", branchRule(1, false)),
		}
		inputs := []policy.ScopedPolicy{
			scopedInput(t, policy.ScopeTenant, "main", branchRule(1, true)),
			scopedInput(t, policy.ScopeRepo, "main", branchRule(1, false)),
		}
		reviews := []forgejo.Review{review("amir", forgejo.ReviewApproved, true, false)}
		st, err := svc.branchState(ctx, scoped, inputs, pr, reviews)
		if err != nil {
			t.Fatal(err)
		}
		if st.Approvals != 0 || !st.Blocked {
			t.Fatalf("expected stale approval dropped and blocked, got %+v", st)
		}
	})
}
