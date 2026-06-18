package policy

import (
	"encoding/json"
	"fmt"
)

// EnvironmentRule is the typed rules body for KindEnvironment: the gate Quill
// enforces before a commit may deploy to an environment matching a policy's
// selector (the environment name or a glob). It expresses the deploy controls
// teams ask for: required approvers, which source branches may reach the
// environment, an ordered promotion path (a prior environment must succeed
// first), a required green pipeline run (central status check), and a soak/
// freeze window before a candidate becomes deployable.
type EnvironmentRule struct {
	// RequiredApprovals is the number of deploy approvals required.
	RequiredApprovals int `json:"requiredApprovals"`
	// AllowedSourceBranches restricts which git refs may deploy here. Each entry
	// is a glob matched against the deploy ref. Empty means any ref.
	AllowedSourceBranches []string `json:"allowedSourceBranches,omitempty"`
	// RequirePreviousEnvironment names an environment that must have a successful
	// deploy of the same commit before this one (e.g. staging before production).
	// Empty means no ordering requirement.
	RequirePreviousEnvironment string `json:"requirePreviousEnvironment,omitempty"`
	// RequireSuccessfulRun blocks the deploy unless the commit has a green
	// pipeline run — a required central status check.
	RequireSuccessfulRun bool `json:"requireSuccessfulRun"`
	// MinWaitMinutes is a soak/freeze window: the candidate must have been
	// eligible for at least this many minutes before it may deploy. Zero disables
	// the window.
	MinWaitMinutes int `json:"minWaitMinutes,omitempty"`
}

// MarshalRules encodes an EnvironmentRule for storage in the policies.rules column.
func (r EnvironmentRule) MarshalRules() ([]byte, error) {
	return json.Marshal(r)
}

// DecodeEnvironmentRule reads an EnvironmentRule from a policies.rules JSON document.
func DecodeEnvironmentRule(raw []byte) (EnvironmentRule, error) {
	var r EnvironmentRule
	if len(raw) == 0 {
		return r, nil
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return EnvironmentRule{}, err
	}
	return r, nil
}

// environmentApplies reports whether an environment policy governs a deploy: its
// selector matches the target environment name (exact or glob).
func environmentApplies(p ScopedPolicy, facts Context) bool {
	if facts.Environment == nil {
		return false
	}
	return branchMatches(p.Selector, facts.Environment.Environment)
}

// environmentDenials is the shared verdict logic for one environment rule, kept
// here as the single source of truth the typed evaluator uses and the Rego
// module mirrors.
func environmentDenials(rule EnvironmentRule, f EnvironmentFacts) []string {
	var denials []string
	if f.Approvals < rule.RequiredApprovals {
		denials = append(denials, fmt.Sprintf("%d of %d required approvals", f.Approvals, rule.RequiredApprovals))
	}
	if len(rule.AllowedSourceBranches) > 0 && !sourceAllowed(rule.AllowedSourceBranches, f.Ref) {
		denials = append(denials, fmt.Sprintf("ref %q may not deploy to %q", f.Ref, f.Environment))
	}
	if rule.RequirePreviousEnvironment != "" && !containsString(f.PreviousEnvironments, rule.RequirePreviousEnvironment) {
		denials = append(denials, fmt.Sprintf("a successful deploy to %q is required first", rule.RequirePreviousEnvironment))
	}
	if rule.RequireSuccessfulRun && !f.RunSucceeded {
		denials = append(denials, "the commit has no successful pipeline run")
	}
	if rule.MinWaitMinutes > 0 && f.AgeMinutes < rule.MinWaitMinutes {
		denials = append(denials, fmt.Sprintf("must wait %d minutes before deploying (%d elapsed)", rule.MinWaitMinutes, f.AgeMinutes))
	}
	return denials
}

// containsString reports whether s appears in list.
func containsString(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
