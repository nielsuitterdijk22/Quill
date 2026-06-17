package policy

import (
	"context"
	"time"
)

// This file defines the engine's evaluation contract: the typed fact set a gate
// assembles (Context), the verdict it gets back (Decision), and the Evaluator
// interface that turns scope-attached policies plus facts into that verdict.
//
// Composition is unanimous-allow / deny-overrides: a Decision is Allow only when
// every governing scope (tenant, project, repo) allows. A scope with no
// applicable policy abstains, which counts as allow. Because adding a scope can
// only add denials, a narrower scope can never weaken a broader one — the
// "always locked" guarantee holds by construction, with no rule merging.

// ActorFacts describes the principal performing the gated action.
type ActorFacts struct {
	Login   string   `json:"login"`
	Groups  []string `json:"groups"`
	IsAdmin bool     `json:"isAdmin"`
}

// BranchFacts are the facts a branch/merge gate evaluates: the pull request's
// refs, its review tally, and the changed paths. Pointer fields on Context keep
// kinds that don't apply (e.g. a deploy gate) out of the branch input.
type BranchFacts struct {
	BaseRef          string   `json:"baseRef"`
	HeadRef          string   `json:"headRef"`
	Approvals        int      `json:"approvals"`
	ChangesRequested int      `json:"changesRequested"`
	UpToDate         bool     `json:"upToDate"`
	ChangedPaths     []string `json:"changedPaths"`
}

// Context is the typed fact set passed to the evaluator. It is engine-agnostic:
// the same facts feed the typed evaluator and the embedded-OPA evaluator. New
// kinds (environment, artefact promotion) add their own facts here.
type Context struct {
	Actor  ActorFacts   `json:"actor"`
	Now    time.Time    `json:"now"`
	Branch *BranchFacts `json:"branch,omitempty"`
}

// Denial is a single reason a scope blocked the action, tagged with the scope
// and selector that produced it so the UI can explain the verdict.
type Denial struct {
	Scope    ScopeType `json:"scope"`
	Selector string    `json:"selector"`
	Message  string    `json:"message"`
}

// Decision is the composed verdict across all governing scopes.
type Decision struct {
	Allow   bool     `json:"allow"`
	Denials []Denial `json:"denials"`
}

// ScopedPolicy is one stored policy in the form the evaluator consumes: the
// scope it attaches to, its selector, and its raw (kind-specific) rule body.
type ScopedPolicy struct {
	Scope    ScopeType
	Selector string
	Rules    []byte
}

// Evaluator turns the policies governing a target plus the facts about an action
// into a Decision. Implementations differ only in how a single applicable policy
// is judged; the unanimous-allow composition is shared (see Compose).
type Evaluator interface {
	Evaluate(ctx context.Context, kind Kind, policies []ScopedPolicy, facts Context) (Decision, error)
}

// ruleJudge judges one applicable policy against the facts, returning the deny
// messages it raises (empty means that policy allows). Each evaluator supplies
// one; Compose handles applicability and composition around it.
type ruleJudge func(ctx context.Context, p ScopedPolicy, facts Context) ([]string, error)

// applies reports whether a policy governs the action in facts for the kind —
// e.g. a branch policy applies when its selector matches the PR's base ref.
type appliesFunc func(p ScopedPolicy, facts Context) bool

// Compose runs the shared unanimous-allow composition: for every policy that
// applies, collect its denials; the action is allowed only when none deny. A
// policy that doesn't apply abstains. This is monotonic across scopes, so the
// governance lock holds without merging rules.
func Compose(ctx context.Context, policies []ScopedPolicy, facts Context, applies appliesFunc, judge ruleJudge) (Decision, error) {
	decision := Decision{Allow: true}
	for _, p := range policies {
		if !applies(p, facts) {
			continue
		}
		msgs, err := judge(ctx, p, facts)
		if err != nil {
			return Decision{}, err
		}
		for _, m := range msgs {
			decision.Allow = false
			decision.Denials = append(decision.Denials, Denial{
				Scope:    p.Scope,
				Selector: p.Selector,
				Message:  m,
			})
		}
	}
	return decision, nil
}
