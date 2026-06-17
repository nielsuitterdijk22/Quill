package policy

import (
	"encoding/json"
	"path"
)

// BranchRule is the typed rules body for KindBranch: the protection settings
// Quill enforces on branches matching a policy's selector.
type BranchRule struct {
	RequiredApprovals     int  `json:"requiredApprovals"`
	DismissStaleApprovals bool `json:"dismissStaleApprovals"`
	RequireUpToDate       bool `json:"requireUpToDate"`
	BlockForcePush        bool `json:"blockForcePush"`
	RequirePullRequest    bool `json:"requirePullRequest"`
	// AllowedSources restricts which head branches may merge into a branch the
	// policy governs (merge-flow control, e.g. only release/* into main). Each
	// entry is a glob matched against the PR head ref. Empty means any source.
	AllowedSources []string `json:"allowedSources,omitempty"`
}

// MarshalRules encodes a BranchRule for storage in the policies.rules column.
func (r BranchRule) MarshalRules() ([]byte, error) {
	return json.Marshal(r)
}

// DecodeBranchRule reads a BranchRule from a policies.rules JSON document.
func DecodeBranchRule(raw []byte) (BranchRule, error) {
	var r BranchRule
	if len(raw) == 0 {
		return r, nil
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return BranchRule{}, err
	}
	return r, nil
}

// ScopedBranch is a branch policy attached to a scope, with its rule decoded.
// The resolver consumes a slice of these spanning the scopes that govern a repo.
type ScopedBranch struct {
	Scope    ScopeType
	Selector string
	Locked   bool
	Rule     BranchRule
}

// EffectiveBranch resolves the rule governing branch by folding the matching
// policies from the broadest scope (tenant) to the narrowest (repo).
//
// Within a scope the most specific selector wins (an exact branch name beats a
// glob). Across scopes a narrower policy fully overrides a broader one, unless a
// broader scope (or any ancestor up the chain) is Locked, in which case the
// narrower policy may only tighten — stricter settings are kept, weaker ones are
// ignored. It returns the effective rule and the selector that produced it, or
// (nil, "") when no policy matches the branch.
//
// policies need not be pre-sorted; EffectiveBranch orders them by scope itself.
func EffectiveBranch(policies []ScopedBranch, branch string) (*BranchRule, string) {
	matches := matchByScope(policies, branch)
	if len(matches) == 0 {
		return nil, ""
	}

	effective := matches[0].Rule
	pattern := matches[0].Selector
	locked := matches[0].Locked
	for _, m := range matches[1:] {
		if locked {
			effective = tightenBranch(effective, m.Rule)
		} else {
			effective = m.Rule
		}
		pattern = m.Selector
		locked = locked || m.Locked
	}
	return &effective, pattern
}

// matchByScope returns the best-matching policy per scope for branch, ordered
// broad -> narrow. At most one policy per scope is returned (the most specific
// selector for that scope).
func matchByScope(policies []ScopedBranch, branch string) []ScopedBranch {
	bestPerScope := map[ScopeType]ScopedBranch{}
	for _, p := range policies {
		if !branchMatches(p.Selector, branch) {
			continue
		}
		cur, ok := bestPerScope[p.Scope]
		if !ok || moreSpecific(p.Selector, cur.Selector, branch) {
			bestPerScope[p.Scope] = p
		}
	}
	ordered := make([]ScopedBranch, 0, len(bestPerScope))
	for _, scope := range []ScopeType{ScopeTenant, ScopeProject, ScopeRepo} {
		if p, ok := bestPerScope[scope]; ok {
			ordered = append(ordered, p)
		}
	}
	return ordered
}

// branchMatches reports whether selector governs branch (exact name or glob).
func branchMatches(selector, branch string) bool {
	if selector == branch {
		return true
	}
	ok, err := path.Match(selector, branch)
	return err == nil && ok
}

// moreSpecific reports whether selector a should win over the currently chosen
// selector b for branch. An exact match is always the most specific.
func moreSpecific(a, b, branch string) bool {
	if a == branch {
		return true
	}
	if b == branch {
		return false
	}
	// Both are globs: prefer the longer (more constrained) pattern as a simple,
	// stable tie-breaker.
	return len(a) > len(b)
}

// tightenBranch returns the stricter of two branch rules field by field: the
// higher approval count and the logical OR of each boolean gate. Used when a
// locked ancestor forbids a narrower scope from weakening protection.
func tightenBranch(base, override BranchRule) BranchRule {
	out := base
	if override.RequiredApprovals > out.RequiredApprovals {
		out.RequiredApprovals = override.RequiredApprovals
	}
	out.DismissStaleApprovals = out.DismissStaleApprovals || override.DismissStaleApprovals
	out.RequireUpToDate = out.RequireUpToDate || override.RequireUpToDate
	out.BlockForcePush = out.BlockForcePush || override.BlockForcePush
	out.RequirePullRequest = out.RequirePullRequest || override.RequirePullRequest
	return out
}
