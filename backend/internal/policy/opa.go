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

// kindModule describes how a policy kind is judged by Rego: which embedded
// module file to compile, the deny-set query it exposes, the applicability test,
// and how to build the Rego input from a policy and the facts.
type kindModule struct {
	file    string
	query   string
	applies appliesFunc
	input   func(p ScopedPolicy, facts Context) (map[string]any, error)
}

// kindModules registers the Rego module for every supported kind. Each module
// lives under data.quill.<kind> and exposes a `deny` set of reason strings.
var kindModules = map[Kind]kindModule{
	KindBranch: {
		file:    "rego/branch.rego",
		query:   "data.quill.branch.deny",
		applies: branchApplies,
		input:   branchRegoInput,
	},
	KindEnvironment: {
		file:    "rego/environment.rego",
		query:   "data.quill.environment.deny",
		applies: environmentApplies,
		input:   environmentRegoInput,
	},
}

// OPAEvaluator judges policies by evaluating embedded Rego. Each kind's query is
// compiled once at construction and reused for every evaluation.
type OPAEvaluator struct {
	queries map[Kind]rego.PreparedEvalQuery
}

// NewOPAEvaluator compiles the embedded Rego module for every supported kind and
// returns an evaluator. It fails fast if any module does not compile.
func NewOPAEvaluator(ctx context.Context) (*OPAEvaluator, error) {
	queries := make(map[Kind]rego.PreparedEvalQuery, len(kindModules))
	for kind, m := range kindModules {
		module, err := regoFS.ReadFile(m.file)
		if err != nil {
			return nil, fmt.Errorf("read embedded rego %q: %w", m.file, err)
		}
		prepared, err := rego.New(
			rego.Query(m.query),
			rego.Module(m.file, string(module)),
			rego.SetRegoVersion(ast.RegoV1),
		).PrepareForEval(ctx)
		if err != nil {
			return nil, fmt.Errorf("compile rego %q: %w", m.file, err)
		}
		queries[kind] = prepared
	}
	return &OPAEvaluator{queries: queries}, nil
}

// Evaluate composes the policies governing an action into a verdict by running
// the kind's Rego deny set for each applicable policy.
func (e *OPAEvaluator) Evaluate(ctx context.Context, kind Kind, policies []ScopedPolicy, facts Context) (Decision, error) {
	m, ok := kindModules[kind]
	if !ok {
		return Decision{}, fmt.Errorf("opa evaluator: unsupported kind %q", kind)
	}
	switch kind {
	case KindBranch:
		if facts.Branch == nil {
			return Decision{}, fmt.Errorf("opa evaluator: branch facts are required")
		}
	case KindEnvironment:
		if facts.Environment == nil {
			return Decision{}, fmt.Errorf("opa evaluator: environment facts are required")
		}
	}
	return Compose(ctx, policies, facts, m.applies, e.judge(kind, m))
}

// judge returns a ruleJudge that evaluates the kind's module for one policy,
// returning its deny messages. The input is {rule, facts} built by the kind's
// input function so Rego and Go see identical fields.
func (e *OPAEvaluator) judge(kind Kind, m kindModule) ruleJudge {
	return func(ctx context.Context, p ScopedPolicy, facts Context) ([]string, error) {
		input, err := m.input(p, facts)
		if err != nil {
			return nil, err
		}
		rs, err := e.queries[kind].Eval(ctx, rego.EvalInput(input))
		if err != nil {
			return nil, fmt.Errorf("eval rego: %w", err)
		}
		return denyMessages(rs)
	}
}

// branchRegoInput decodes a branch policy and shapes the Rego input for it.
func branchRegoInput(p ScopedPolicy, facts Context) (map[string]any, error) {
	rule, err := DecodeBranchRule(p.Rules)
	if err != nil {
		return nil, err
	}
	return regoInput(rule, *facts.Branch)
}

// environmentRegoInput decodes an environment policy and shapes the Rego input
// for it. A nil PreviousEnvironments slice is normalised to an empty list so the
// Rego `some e in …` iteration never runs over a JSON null.
func environmentRegoInput(p ScopedPolicy, facts Context) (map[string]any, error) {
	rule, err := DecodeEnvironmentRule(p.Rules)
	if err != nil {
		return nil, err
	}
	f := *facts.Environment
	if f.PreviousEnvironments == nil {
		f.PreviousEnvironments = []string{}
	}
	return regoInput(rule, f)
}

// regoInput builds the JSON-shaped {rule, facts} input map Rego evaluates
// against. Marshaling through the typed structs guarantees Rego sees the same
// field names and values the Go evaluator does.
func regoInput(rule, facts any) (map[string]any, error) {
	ruleMap, err := toMap(rule)
	if err != nil {
		return nil, fmt.Errorf("marshal rule: %w", err)
	}
	factsMap, err := toMap(facts)
	if err != nil {
		return nil, fmt.Errorf("marshal facts: %w", err)
	}
	return map[string]any{"rule": ruleMap, "facts": factsMap}, nil
}

// toMap round-trips v through JSON into a generic map for Rego input.
func toMap(v any) (map[string]any, error) {
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	return m, nil
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
