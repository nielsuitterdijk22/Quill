package quill.branch

# Branch-policy decision logic, mirroring policy.branchDenials in Go. The gate
# passes input = {rule: <BranchRule>, facts: <BranchFacts>}; the policy yields a
# set of human-readable deny reasons. The action is allowed when deny is empty.
#
# This module is the evaluation logic for the branch kind. Per-scope,
# user-authored Rego is a later step; today the same module judges every scope's
# typed rule, which is enough to prove parity with the Go evaluator in-process.

# Outstanding change requests always block, regardless of approvals.
deny contains "changes have been requested and must be resolved" if {
	input.facts.changesRequested > 0
}

# Not enough approvals to meet the rule's threshold.
deny contains msg if {
	input.facts.approvals < input.rule.requiredApprovals
	msg := sprintf("%d of %d required approvals", [input.facts.approvals, input.rule.requiredApprovals])
}

# The branch must be up to date with its base when the rule requires it.
deny contains "branch is not up to date with the base" if {
	input.rule.requireUpToDate == true
	input.facts.upToDate == false
}

# Merge-flow control: when allowedSources is set, the head ref must match one of
# the globs, else the source may not merge into this base.
deny contains msg if {
	count(object.get(input.rule, "allowedSources", [])) > 0
	not source_allowed
	msg := sprintf("source branch %q may not merge into %q", [input.facts.headRef, input.facts.baseRef])
}

source_allowed if {
	some pattern in input.rule.allowedSources
	glob.match(pattern, ["/"], input.facts.headRef)
}
