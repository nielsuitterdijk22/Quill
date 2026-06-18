package policy

import (
	"context"
	"fmt"
)

// TypedEvaluator judges branch policies with hand-written Go. It is the
// reference implementation: the embedded-OPA evaluator must agree with it on the
// branch kind, which is how we judge OPA on real Quill rules without lock-in.
type TypedEvaluator struct{}

// NewTypedEvaluator returns a TypedEvaluator.
func NewTypedEvaluator() TypedEvaluator { return TypedEvaluator{} }

// Evaluate composes the policies governing an action into a verdict, dispatching
// to the per-kind judge. Branch and environment kinds are supported.
func (TypedEvaluator) Evaluate(ctx context.Context, kind Kind, policies []ScopedPolicy, facts Context) (Decision, error) {
	switch kind {
	case KindBranch:
		if facts.Branch == nil {
			return Decision{}, fmt.Errorf("typed evaluator: branch facts are required")
		}
		return Compose(ctx, policies, facts, branchApplies, judgeBranchTyped)
	case KindEnvironment:
		if facts.Environment == nil {
			return Decision{}, fmt.Errorf("typed evaluator: environment facts are required")
		}
		return Compose(ctx, policies, facts, environmentApplies, judgeEnvironmentTyped)
	default:
		return Decision{}, fmt.Errorf("typed evaluator: unsupported kind %q", kind)
	}
}

// branchApplies reports whether a branch policy governs the PR: its selector
// matches the base ref the PR targets.
func branchApplies(p ScopedPolicy, facts Context) bool {
	if facts.Branch == nil {
		return false
	}
	return branchMatches(p.Selector, facts.Branch.BaseRef)
}

// judgeBranchTyped raises the deny messages a single branch policy produces
// against a pull request's facts.
func judgeBranchTyped(_ context.Context, p ScopedPolicy, facts Context) ([]string, error) {
	rule, err := DecodeBranchRule(p.Rules)
	if err != nil {
		return nil, err
	}
	return branchDenials(rule, *facts.Branch), nil
}

// judgeEnvironmentTyped raises the deny messages a single environment policy
// produces against a deploy's facts.
func judgeEnvironmentTyped(_ context.Context, p ScopedPolicy, facts Context) ([]string, error) {
	rule, err := DecodeEnvironmentRule(p.Rules)
	if err != nil {
		return nil, err
	}
	return environmentDenials(rule, *facts.Environment), nil
}

// branchDenials is the shared verdict logic for one branch rule, kept here so the
// typed evaluator and tests have a single source of truth. The embedded-OPA
// evaluator mirrors this in Rego.
func branchDenials(rule BranchRule, f BranchFacts) []string {
	var denials []string
	if f.ChangesRequested > 0 {
		denials = append(denials, "changes have been requested and must be resolved")
	}
	if f.Approvals < rule.RequiredApprovals {
		denials = append(denials, fmt.Sprintf("%d of %d required approvals", f.Approvals, rule.RequiredApprovals))
	}
	if rule.RequireUpToDate && !f.UpToDate {
		denials = append(denials, "branch is not up to date with the base")
	}
	if len(rule.AllowedSources) > 0 && !sourceAllowed(rule.AllowedSources, f.HeadRef) {
		denials = append(denials, fmt.Sprintf("source branch %q may not merge into %q", f.HeadRef, f.BaseRef))
	}
	return denials
}

// sourceAllowed reports whether head matches any of the allowed source globs.
func sourceAllowed(allowed []string, head string) bool {
	for _, pat := range allowed {
		if branchMatches(pat, head) {
			return true
		}
	}
	return false
}
