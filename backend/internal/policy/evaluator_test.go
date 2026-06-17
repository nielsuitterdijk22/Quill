package policy

import (
	"context"
	"testing"
)

// These tests exercise the evaluation contract: the unanimous-allow composition,
// and parity between the typed evaluator and the embedded-OPA evaluator on the
// branch kind. The typed evaluator is the oracle; OPA must agree with it.

func branchPolicy(scope ScopeType, selector string, rule BranchRule) ScopedPolicy {
	raw, err := rule.MarshalRules()
	if err != nil {
		panic(err)
	}
	return ScopedPolicy{Scope: scope, Selector: selector, Rules: raw}
}

func mergeFacts(base, head string, approvals, changesRequested int, upToDate bool) Context {
	return Context{Branch: &BranchFacts{
		BaseRef:          base,
		HeadRef:          head,
		Approvals:        approvals,
		ChangesRequested: changesRequested,
		UpToDate:         upToDate,
	}}
}

// evaluators returns the implementations under test, keyed by name.
func evaluators(t *testing.T) map[string]Evaluator {
	t.Helper()
	opa, err := NewOPAEvaluator(context.Background())
	if err != nil {
		t.Fatalf("compile opa evaluator: %v", err)
	}
	return map[string]Evaluator{
		"typed": NewTypedEvaluator(),
		"opa":   opa,
	}
}

func TestEvaluators_BranchParity(t *testing.T) {
	cases := []struct {
		name        string
		policies    []ScopedPolicy
		facts       Context
		wantAllow   bool
		wantDenials int
	}{
		{
			name:      "no policies allows",
			policies:  nil,
			facts:     mergeFacts("main", "feature/x", 0, 0, true),
			wantAllow: true,
		},
		{
			name:        "insufficient approvals blocks",
			policies:    []ScopedPolicy{branchPolicy(ScopeRepo, "main", BranchRule{RequiredApprovals: 2})},
			facts:       mergeFacts("main", "feature/x", 1, 0, true),
			wantAllow:   false,
			wantDenials: 1,
		},
		{
			name:        "changes requested blocks",
			policies:    []ScopedPolicy{branchPolicy(ScopeRepo, "main", BranchRule{RequiredApprovals: 1})},
			facts:       mergeFacts("main", "feature/x", 1, 1, true),
			wantAllow:   false,
			wantDenials: 1,
		},
		{
			name:      "satisfied policy allows",
			policies:  []ScopedPolicy{branchPolicy(ScopeRepo, "main", BranchRule{RequiredApprovals: 1})},
			facts:     mergeFacts("main", "feature/x", 1, 0, true),
			wantAllow: true,
		},
		{
			name:        "not up to date blocks when required",
			policies:    []ScopedPolicy{branchPolicy(ScopeRepo, "main", BranchRule{RequireUpToDate: true})},
			facts:       mergeFacts("main", "feature/x", 0, 0, false),
			wantAllow:   false,
			wantDenials: 1,
		},
		{
			name:      "policy for other branch does not apply",
			policies:  []ScopedPolicy{branchPolicy(ScopeRepo, "release/*", BranchRule{RequiredApprovals: 5})},
			facts:     mergeFacts("main", "feature/x", 0, 0, true),
			wantAllow: true,
		},
		{
			name:        "disallowed source blocks",
			policies:    []ScopedPolicy{branchPolicy(ScopeRepo, "main", BranchRule{AllowedSources: []string{"release/*"}})},
			facts:       mergeFacts("main", "feature/x", 0, 0, true),
			wantAllow:   false,
			wantDenials: 1,
		},
		{
			name:      "allowed source passes",
			policies:  []ScopedPolicy{branchPolicy(ScopeRepo, "main", BranchRule{AllowedSources: []string{"release/*"}})},
			facts:     mergeFacts("main", "release/1.2", 0, 0, true),
			wantAllow: true,
		},
		{
			name: "two denials accumulate",
			policies: []ScopedPolicy{branchPolicy(ScopeRepo, "main", BranchRule{
				RequiredApprovals: 2,
				AllowedSources:    []string{"release/*"},
			})},
			facts:       mergeFacts("main", "feature/x", 0, 0, true),
			wantAllow:   false,
			wantDenials: 2,
		},
	}

	for name, ev := range evaluators(t) {
		for _, tc := range cases {
			t.Run(name+"/"+tc.name, func(t *testing.T) {
				got, err := ev.Evaluate(context.Background(), KindBranch, tc.policies, tc.facts)
				if err != nil {
					t.Fatalf("evaluate: %v", err)
				}
				if got.Allow != tc.wantAllow {
					t.Fatalf("allow=%v want %v (denials=%+v)", got.Allow, tc.wantAllow, got.Denials)
				}
				if len(got.Denials) != tc.wantDenials {
					t.Fatalf("denials=%d want %d (%+v)", len(got.Denials), tc.wantDenials, got.Denials)
				}
			})
		}
	}
}

func TestEvaluators_UnanimousAllowComposition(t *testing.T) {
	// Tenant requires 1 approval, project requires 3, repo silent. Under
	// unanimous-allow the strictest wins: 1 approval still trips the project's
	// rule, so 2 approvals must block and 3 must allow. The repo cannot loosen.
	policies := []ScopedPolicy{
		branchPolicy(ScopeTenant, "main", BranchRule{RequiredApprovals: 1}),
		branchPolicy(ScopeProject, "main", BranchRule{RequiredApprovals: 3}),
	}

	for name, ev := range evaluators(t) {
		t.Run(name+"/two approvals blocked by project", func(t *testing.T) {
			got, err := ev.Evaluate(context.Background(), KindBranch, policies, mergeFacts("main", "x", 2, 0, true))
			if err != nil {
				t.Fatal(err)
			}
			if got.Allow {
				t.Fatalf("expected block, got allow (denials=%+v)", got.Denials)
			}
			// Only the project (needs 3) denies; tenant (needs 1) is satisfied.
			if len(got.Denials) != 1 || got.Denials[0].Scope != ScopeProject {
				t.Fatalf("expected single project denial, got %+v", got.Denials)
			}
		})

		t.Run(name+"/three approvals allowed", func(t *testing.T) {
			got, err := ev.Evaluate(context.Background(), KindBranch, policies, mergeFacts("main", "x", 3, 0, true))
			if err != nil {
				t.Fatal(err)
			}
			if !got.Allow {
				t.Fatalf("expected allow, got denials %+v", got.Denials)
			}
		})
	}
}

func TestEvaluators_AbstainIsAllow(t *testing.T) {
	// A tenant policy that targets release/* abstains for a PR into main, so the
	// action is allowed even though a policy exists at a higher scope.
	policies := []ScopedPolicy{
		branchPolicy(ScopeTenant, "release/*", BranchRule{RequiredApprovals: 5}),
	}
	for name, ev := range evaluators(t) {
		t.Run(name, func(t *testing.T) {
			got, err := ev.Evaluate(context.Background(), KindBranch, policies, mergeFacts("main", "x", 0, 0, true))
			if err != nil {
				t.Fatal(err)
			}
			if !got.Allow || len(got.Denials) != 0 {
				t.Fatalf("expected allow via abstain, got %+v", got)
			}
		})
	}
}

func TestEvaluators_DenyTaggedWithScope(t *testing.T) {
	policies := []ScopedPolicy{
		branchPolicy(ScopeRepo, "main", BranchRule{RequiredApprovals: 2}),
	}
	for name, ev := range evaluators(t) {
		t.Run(name, func(t *testing.T) {
			got, err := ev.Evaluate(context.Background(), KindBranch, policies, mergeFacts("main", "x", 0, 0, true))
			if err != nil {
				t.Fatal(err)
			}
			if len(got.Denials) != 1 {
				t.Fatalf("want 1 denial, got %+v", got.Denials)
			}
			d := got.Denials[0]
			if d.Scope != ScopeRepo || d.Selector != "main" {
				t.Fatalf("denial not tagged with scope/selector: %+v", d)
			}
		})
	}
}
