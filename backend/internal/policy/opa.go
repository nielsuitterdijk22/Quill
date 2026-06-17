package policy

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"sort"

	"github.com/open-policy-agent/opa/ast"
	"github.com/open-policy-agent/opa/rego"
)

// This file is the embedded-OPA evaluator: it runs the Rego modules in rego/ in
// process (no sidecar) to judge policies, and composes the per-scope verdicts
// with the same unanimous-allow rule as the typed evaluator. It exists to judge
// OPA on real Quill rules behind the Evaluator interface, with the typed
// implementation as the reference oracle.

//go:embed rego/*.rego
var regoFS embed.FS

// regoQuery is the deny set each kind's module exposes. Modules live under the
// data.quill.<kind> path and expose a `deny` set of reason strings.
const regoQuery = "data.quill.branch.deny"

// OPAEvaluator judges policies by evaluating embedded Rego. The prepared query
// is compiled once at construction and reused for every evaluation.
type OPAEvaluator struct {
	prepared rego.PreparedEvalQuery
}

// NewOPAEvaluator compiles the embedded Rego modules and returns an evaluator.
// It fails fast if the policy bundle does not compile.
func NewOPAEvaluator(ctx context.Context) (*OPAEvaluator, error) {
	module, err := regoFS.ReadFile("rego/branch.rego")
	if err != nil {
		return nil, fmt.Errorf("read embedded rego: %w", err)
	}
	prepared, err := rego.New(
		rego.Query(regoQuery),
		rego.Module("branch.rego", string(module)),
		rego.SetRegoVersion(ast.RegoV1),
	).PrepareForEval(ctx)
	if err != nil {
		return nil, fmt.Errorf("compile rego: %w", err)
	}
	return &OPAEvaluator{prepared: prepared}, nil
}

// Evaluate composes the branch policies governing a pull request into a verdict
// by running the Rego deny set for each applicable policy.
func (e *OPAEvaluator) Evaluate(ctx context.Context, kind Kind, policies []ScopedPolicy, facts Context) (Decision, error) {
	if kind != KindBranch {
		return Decision{}, fmt.Errorf("opa evaluator: unsupported kind %q", kind)
	}
	if facts.Branch == nil {
		return Decision{}, fmt.Errorf("opa evaluator: branch facts are required")
	}
	return Compose(ctx, policies, facts, branchApplies, e.judgeBranchRego)
}

// judgeBranchRego evaluates the branch module for one policy, returning its deny
// messages. Input is {rule, facts}; the rule is the decoded BranchRule so Rego
// and Go see identical fields.
func (e *OPAEvaluator) judgeBranchRego(ctx context.Context, p ScopedPolicy, facts Context) ([]string, error) {
	rule, err := DecodeBranchRule(p.Rules)
	if err != nil {
		return nil, err
	}
	input, err := branchInput(rule, *facts.Branch)
	if err != nil {
		return nil, err
	}
	rs, err := e.prepared.Eval(ctx, rego.EvalInput(input))
	if err != nil {
		return nil, fmt.Errorf("eval rego: %w", err)
	}
	return denyMessages(rs)
}

// branchInput builds the JSON-shaped input map Rego evaluates against. Marshaling
// through the typed structs guarantees Rego sees the same field names and values
// the Go evaluator does.
func branchInput(rule BranchRule, facts BranchFacts) (map[string]any, error) {
	ruleJSON, err := json.Marshal(rule)
	if err != nil {
		return nil, fmt.Errorf("marshal rule: %w", err)
	}
	factsJSON, err := json.Marshal(facts)
	if err != nil {
		return nil, fmt.Errorf("marshal facts: %w", err)
	}
	var ruleMap, factsMap map[string]any
	if err := json.Unmarshal(ruleJSON, &ruleMap); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(factsJSON, &factsMap); err != nil {
		return nil, err
	}
	return map[string]any{"rule": ruleMap, "facts": factsMap}, nil
}

// denyMessages extracts the deny set (a list of strings) from a Rego result set,
// sorted for stable output.
func denyMessages(rs rego.ResultSet) ([]string, error) {
	if len(rs) == 0 || len(rs[0].Expressions) == 0 {
		return nil, nil
	}
	raw, ok := rs[0].Expressions[0].Value.([]any)
	if !ok {
		return nil, fmt.Errorf("unexpected rego deny shape %T", rs[0].Expressions[0].Value)
	}
	msgs := make([]string, 0, len(raw))
	for _, v := range raw {
		s, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("non-string deny message %T", v)
		}
		msgs = append(msgs, s)
	}
	sort.Strings(msgs)
	return msgs, nil
}
