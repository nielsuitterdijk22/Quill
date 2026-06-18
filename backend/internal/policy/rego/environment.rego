package quill.environment

# Environment-policy decision logic, mirroring policy.environmentDenials in Go.
# The gate passes input = {rule: <EnvironmentRule>, facts: <EnvironmentFacts>};
# the policy yields a set of human-readable deny reasons. The deploy is allowed
# when deny is empty.
#
# This module is the evaluation logic for the environment kind. As with the
# branch module, the same module judges every scope's typed rule today, which is
# enough to prove parity with the Go evaluator in-process.

# Not enough deploy approvals to meet the rule's threshold.
deny contains msg if {
	input.facts.approvals < input.rule.requiredApprovals
	msg := sprintf("%d of %d required approvals", [input.facts.approvals, input.rule.requiredApprovals])
}

# Source-branch control: when allowedSourceBranches is set, the deploy ref must
# match one of the globs, else it may not reach this environment.
deny contains msg if {
	count(object.get(input.rule, "allowedSourceBranches", [])) > 0
	not source_allowed
	msg := sprintf("ref %q may not deploy to %q", [input.facts.ref, input.facts.environment])
}

source_allowed if {
	some pattern in input.rule.allowedSourceBranches
	glob.match(pattern, ["/"], input.facts.ref)
}

# Promotion order: a named prior environment must have deployed this commit first.
deny contains msg if {
	req := object.get(input.rule, "requirePreviousEnvironment", "")
	req != ""
	not previous_ok(req)
	msg := sprintf("a successful deploy to %q is required first", [req])
}

previous_ok(req) if {
	some e in input.facts.previousEnvironments
	e == req
}

# Required central status check: the commit must have a green pipeline run.
deny contains "the commit has no successful pipeline run" if {
	input.rule.requireSuccessfulRun == true
	input.facts.runSucceeded == false
}

# Soak/freeze window: the candidate must have been eligible long enough.
deny contains msg if {
	wait := object.get(input.rule, "minWaitMinutes", 0)
	wait > 0
	input.facts.ageMinutes < wait
	msg := sprintf("must wait %d minutes before deploying (%d elapsed)", [wait, input.facts.ageMinutes])
}
