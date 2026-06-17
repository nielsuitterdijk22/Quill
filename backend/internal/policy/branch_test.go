package policy

import "testing"

func sb(scope ScopeType, selector string, locked bool, approvals int, requirePR bool) ScopedBranch {
	return ScopedBranch{
		Scope:    scope,
		Selector: selector,
		Locked:   locked,
		Rule: BranchRule{
			RequiredApprovals:  approvals,
			RequirePullRequest: requirePR,
		},
	}
}

func TestEffectiveBranch_SingleScope(t *testing.T) {
	policies := []ScopedBranch{
		sb(ScopeRepo, "release/*", false, 2, true),
		sb(ScopeRepo, "main", false, 1, true),
	}

	t.Run("exact selector beats glob", func(t *testing.T) {
		rule, pattern := EffectiveBranch(policies, "main")
		if rule == nil || pattern != "main" || rule.RequiredApprovals != 1 {
			t.Fatalf("got rule=%+v pattern=%q", rule, pattern)
		}
	})

	t.Run("glob matches", func(t *testing.T) {
		rule, pattern := EffectiveBranch(policies, "release/1.2")
		if rule == nil || pattern != "release/*" || rule.RequiredApprovals != 2 {
			t.Fatalf("got rule=%+v pattern=%q", rule, pattern)
		}
	})

	t.Run("no match returns nil", func(t *testing.T) {
		if rule, pattern := EffectiveBranch(policies, "feature/x"); rule != nil || pattern != "" {
			t.Fatalf("expected no match, got rule=%+v pattern=%q", rule, pattern)
		}
	})
}

func TestEffectiveBranch_UnlockedOverride(t *testing.T) {
	// Tenant requires 2 approvals on main but does not lock; the repo may relax
	// to 1 because a narrower unlocked scope fully overrides.
	policies := []ScopedBranch{
		sb(ScopeTenant, "main", false, 2, true),
		sb(ScopeRepo, "main", false, 1, false),
	}
	rule, pattern := EffectiveBranch(policies, "main")
	if rule == nil || pattern != "main" {
		t.Fatalf("got rule=%+v pattern=%q", rule, pattern)
	}
	if rule.RequiredApprovals != 1 || rule.RequirePullRequest {
		t.Fatalf("expected repo override (1 approval, no PR), got %+v", rule)
	}
}

func TestEffectiveBranch_LockedTightensOnly(t *testing.T) {
	// Tenant locks main at 2 approvals + require PR. The repo tries to weaken to
	// 0 approvals and no PR, but the lock forces a tighten: stricter wins.
	policies := []ScopedBranch{
		sb(ScopeTenant, "main", true, 2, true),
		sb(ScopeRepo, "main", false, 0, false),
	}
	rule, _ := EffectiveBranch(policies, "main")
	if rule == nil {
		t.Fatal("expected a rule")
	}
	if rule.RequiredApprovals != 2 {
		t.Fatalf("locked floor not kept: approvals=%d, want 2", rule.RequiredApprovals)
	}
	if !rule.RequirePullRequest {
		t.Fatal("locked require-PR was weakened")
	}
}

func TestEffectiveBranch_LockedAllowsStricterRepo(t *testing.T) {
	// Tenant locks at 1 approval; the repo raises it to 3. Tightening upward is
	// allowed.
	policies := []ScopedBranch{
		sb(ScopeTenant, "main", true, 1, true),
		sb(ScopeRepo, "main", false, 3, true),
	}
	rule, _ := EffectiveBranch(policies, "main")
	if rule == nil || rule.RequiredApprovals != 3 {
		t.Fatalf("expected repo to tighten to 3, got %+v", rule)
	}
}

func TestEffectiveBranch_ProjectLockBindsRepo(t *testing.T) {
	// Tenant unlocked, project locks at 2, repo tries 0. The project lock binds
	// the repo even though the tenant did not lock.
	policies := []ScopedBranch{
		sb(ScopeTenant, "main", false, 1, true),
		sb(ScopeProject, "main", true, 2, true),
		sb(ScopeRepo, "main", false, 0, false),
	}
	rule, _ := EffectiveBranch(policies, "main")
	if rule == nil || rule.RequiredApprovals != 2 || !rule.RequirePullRequest {
		t.Fatalf("project lock did not bind repo, got %+v", rule)
	}
}

func TestEffectiveBranch_ThreeScopeFold(t *testing.T) {
	// No locks anywhere: the narrowest scope (repo) wins outright.
	policies := []ScopedBranch{
		sb(ScopeTenant, "*", false, 1, true),
		sb(ScopeProject, "main", false, 2, true),
		sb(ScopeRepo, "main", false, 3, false),
	}
	rule, pattern := EffectiveBranch(policies, "main")
	if rule == nil || pattern != "main" || rule.RequiredApprovals != 3 || rule.RequirePullRequest {
		t.Fatalf("expected repo to win, got rule=%+v pattern=%q", rule, pattern)
	}
}

func TestEffectiveBranch_TenantGlobAppliesWhenRepoSilent(t *testing.T) {
	// Only a tenant glob policy exists; it governs repos that declare nothing.
	policies := []ScopedBranch{
		sb(ScopeTenant, "*", false, 1, true),
	}
	rule, pattern := EffectiveBranch(policies, "main")
	if rule == nil || pattern != "*" || rule.RequiredApprovals != 1 {
		t.Fatalf("tenant glob should apply, got rule=%+v pattern=%q", rule, pattern)
	}
}

func TestDecodeBranchRule(t *testing.T) {
	t.Run("empty rules decode to zero value", func(t *testing.T) {
		r, err := DecodeBranchRule(nil)
		if err != nil || r != (BranchRule{}) {
			t.Fatalf("got r=%+v err=%v", r, err)
		}
	})

	t.Run("round-trips through marshal", func(t *testing.T) {
		in := BranchRule{RequiredApprovals: 2, RequirePullRequest: true, BlockForcePush: true}
		raw, err := in.MarshalRules()
		if err != nil {
			t.Fatalf("marshal: %v", err)
		}
		out, err := DecodeBranchRule(raw)
		if err != nil {
			t.Fatalf("decode: %v", err)
		}
		if out != in {
			t.Fatalf("round-trip mismatch: %+v != %+v", out, in)
		}
	})
}
